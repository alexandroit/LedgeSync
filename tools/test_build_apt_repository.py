#!/usr/bin/env python3
"""Read-only inventory/version guards; native signed APT lifecycle runs in CI."""
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from build_apt_repository import ARCHES, PACKAGES, validate_packages


class RepositoryInputGuards(unittest.TestCase):
    def inventory(self, root, version):
        metadata = {}
        for package in PACKAGES:
            for arch in ARCHES:
                path = root / f'{package}_{version}_{arch}.deb'
                path.write_bytes(b'synthetic package fixture')
                metadata[str(path)] = f'Package: {package}\nVersion: {version}\nArchitecture: {arch}\n'.encode()
        return metadata

    def test_accepts_explicit_new_and_previous_versions(self):
        for version in ('0.1.0~alpha.1-1', '0.1.0~alpha.2-1', '1.2.3-1'):
            with self.subTest(version=version), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                metadata = self.inventory(root, version)
                with patch('build_apt_repository.run', side_effect=lambda args: metadata[args[2]]):
                    self.assertEqual(len(validate_packages(root, version)), 4)

    def test_requested_version_must_match_every_package(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            metadata = self.inventory(root, '0.1.0~alpha.2-1')
            with patch('build_apt_repository.run', side_effect=lambda args: metadata[args[2]]):
                with self.assertRaisesRegex(ValueError, 'Unexpected package metadata'):
                    validate_packages(root, '0.1.0~alpha.1-1')
                path = next(iter(metadata))
                metadata[path] = metadata[path].replace(b'alpha.2', b'alpha.1')
                with self.assertRaisesRegex(ValueError, 'Unexpected package metadata'):
                    validate_packages(root, '0.1.0~alpha.2-1')

    def test_incomplete_inventory_and_identity_alias_are_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            metadata = self.inventory(root, '0.1.0~alpha.2-1')
            with patch('build_apt_repository.run', side_effect=lambda args: metadata[args[2]]):
                first, second = list(metadata)[:2]
                original = metadata[second]
                metadata[second] = metadata[first]
                with self.assertRaisesRegex(ValueError, 'duplicate package filename'):
                    validate_packages(root, '0.1.0~alpha.2-1')
                metadata[second] = original
                Path(first).unlink()
                with self.assertRaisesRegex(ValueError, 'Both desktop and CLI'):
                    validate_packages(root, '0.1.0~alpha.2-1')

    def test_unsafe_version_and_package_symlink_are_rejected_before_metadata(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for version in ('', '../escape', '0.1.0-alpha.2', '0.1.0~alpha.2-1\n', None, '1'*81):
                with patch('build_apt_repository.run') as call, self.assertRaises(ValueError):
                    validate_packages(root, version)
                call.assert_not_called()
            target = root / 'fixture'
            target.write_bytes(b'synthetic')
            (root / 'alias.deb').symlink_to(target)
            with patch('build_apt_repository.run') as call, self.assertRaises(ValueError):
                validate_packages(root, '0.1.0~alpha.2-1')
            call.assert_not_called()


if __name__ == '__main__':
    unittest.main()
