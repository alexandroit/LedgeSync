#!/usr/bin/env python3
"""Portable checks for DMG immutability and bundle integrity guards.

The macOS CI packaging step also creates, mounts, and validates the real image.
"""
import os
from pathlib import Path
import tempfile
import unittest

from package_dmg import bundle_snapshot, output_paths, publish_pair


class DMGGuardTests(unittest.TestCase):
    def test_existing_image_or_dangling_checksum_is_never_replaced(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            image, checksum = output_paths(root, "0.1.0-alpha.1", "arm64")
            image.write_bytes(b"published original")
            with self.assertRaises(ValueError):
                output_paths(root, "0.1.0-alpha.1", "arm64")
            self.assertEqual(image.read_bytes(), b"published original")
            image.unlink()
            checksum.symlink_to(root / "missing-checksum")
            with self.assertRaises(ValueError):
                output_paths(root, "0.1.0-alpha.1", "arm64")
            self.assertTrue(checksum.is_symlink())

    def test_concurrent_destination_appearing_is_not_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            image, checksum = output_paths(root, "0.1.0-alpha.1", "amd64")
            temporary_image = root / "working.dmg"
            temporary_checksum = root / "working.sha256"
            temporary_image.write_bytes(b"new image")
            temporary_checksum.write_bytes(b"new digest")
            # Simulate another publisher creating the target after preflight.
            image.write_bytes(b"original image")
            with self.assertRaises(FileExistsError):
                publish_pair(temporary_image, temporary_checksum, image, checksum)
            self.assertEqual(image.read_bytes(), b"original image")
            self.assertFalse(os.path.lexists(checksum))

    def test_bundle_fingerprint_detects_changed_bytes_permissions_and_links(self):
        with tempfile.TemporaryDirectory() as directory:
            bundle = Path(directory) / "LedgeSync.app"
            bundle.mkdir()
            binary = bundle / "binary"
            binary.write_bytes(b"original")
            binary.chmod(0o755)
            link = bundle / "internal-link"
            link.symlink_to("binary")
            original = bundle_snapshot(bundle)
            binary.write_bytes(b"changed!")
            self.assertNotEqual(bundle_snapshot(bundle), original)
            binary.write_bytes(b"original")
            binary.chmod(0o644)
            self.assertNotEqual(bundle_snapshot(bundle), original)
            binary.chmod(0o755)
            self.assertEqual(bundle_snapshot(bundle), original)
            link.unlink()
            link.symlink_to(directory)
            with self.assertRaises(ValueError):
                bundle_snapshot(bundle)

    def test_unsafe_version_cannot_escape_output_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            for value in ("../1.0.0", "/1.0.0", "1.0.0\n", "1.0.0/../../other", ""):
                with self.subTest(version=value), self.assertRaises(ValueError):
                    output_paths(Path(directory), value, "arm64")


if __name__ == "__main__":
    unittest.main()
