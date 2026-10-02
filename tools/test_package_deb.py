#!/usr/bin/env python3
"""Portable guard tests; native build/install evidence comes from Ubuntu CI."""
from __future__ import annotations

import io
import json
import copy
from pathlib import Path
import os
import struct
import tarfile
import tempfile
import unittest
from unittest.mock import patch

from package_deb import (DEFAULT_MAINTAINER, ROOT, copy_notices, debian_version, load_release,
                         dependencies, extract_release, needed_libraries,
                         publish_outputs, sha256, verify_archive, verify_elf,
                         write_control)


class DebianPackageGuards(unittest.TestCase):
    def release_fixture(self) -> dict:
        return {"version": "0.1.0-alpha.2", "tag": "v0.1.0-alpha.2",
                "applicationSourceCommit": "a" * 40, "sourceDateEpoch": 1790984728,
                "targets": {"windows-amd64": {"desktop": {
                    "name": "ledgesync-desktop-0.1.0-alpha.2-windows-amd64.zip",
                    "url": "https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.2/ledgesync-desktop-0.1.0-alpha.2-windows-amd64.zip",
                    "sha256": "b" * 64, "size": 123}}}}

    def test_manifest_pins_release_identity_without_old_source_constants(self):
        with tempfile.TemporaryDirectory() as temporary:
            manifest = Path(temporary) / "source.json"
            good = self.release_fixture()
            manifest.write_text(json.dumps(good))
            release = load_release(manifest, "0.1.0-alpha.2")
            self.assertEqual(release["debianVersion"], "0.1.0~alpha.2-1")
            self.assertEqual(release["applicationSourceCommit"], "a" * 40)
            self.assertEqual(release["sourceDateEpoch"], 1790984728)
            self.assertEqual(release["manifestSha256"], sha256(manifest))
            with self.assertRaises(ValueError):
                load_release(manifest, "0.1.0-alpha.1")
            cases = []
            for key, value in (("version", "../../unsafe"), ("tag", "v0.1.0-alpha.1"),
                               ("applicationSourceCommit", "main"), ("sourceDateEpoch", None),
                               ("sourceDateEpoch", True)):
                changed = copy.deepcopy(good)
                changed[key] = value
                cases.append(changed)
            for key, value in (("url", "https://example.test/other.zip"),
                               ("name", "old-release.zip"), ("sha256", "bad"), ("size", True)):
                changed = copy.deepcopy(good)
                changed["targets"]["windows-amd64"]["desktop"][key] = value
                cases.append(changed)
            for changed in cases:
                manifest.write_text(json.dumps(changed))
                with self.assertRaises(ValueError):
                    load_release(manifest)
            self.assertEqual(debian_version("1.2.3"), "1.2.3-1")

    def test_desktop_recommends_native_secret_service(self):
        with tempfile.TemporaryDirectory() as temporary:
            stage = Path(temporary)
            release = self.release_fixture()
            release["debianVersion"] = debian_version(release["version"])
            control = write_control(stage, "ledgesync", "amd64", "ledgesync-cli", DEFAULT_MAINTAINER, release)
            self.assertIn("Recommends: gnome-keyring", control)
            self.assertIn("X-LedgeSync-Source-Revision: " + "a" * 40, control)

    def make_archive(self, path: Path, entries: list[tuple[str, bytes, str]]) -> None:
        with tarfile.open(path, "w:gz") as archive:
            for name, data, kind in entries:
                info = tarfile.TarInfo(name)
                info.mode = 0o755 if name.endswith("/LedgeSync") else 0o644
                if kind == "link":
                    info.type = tarfile.SYMTYPE
                    info.linkname = "/outside"
                elif kind == "special":
                    info.type = tarfile.FIFOTYPE
                else:
                    info.size = len(data)
                archive.addfile(info, io.BytesIO(data) if info.isreg() else None)

    def test_cli_suggests_vault_without_graphical_library_dependencies(self):
        with tempfile.TemporaryDirectory() as temporary:
            release = self.release_fixture()
            release["debianVersion"] = debian_version(release["version"])
            control = write_control(Path(temporary), "ledgesync-cli", "arm64", "", DEFAULT_MAINTAINER, release)
            self.assertIn("Suggests: gnome-keyring", control)
            self.assertNotIn("Depends:", control)
            self.assertNotIn("Recommends:", control)
            self.assertIn("unlocked Secret Service collection", control)

    def test_archive_hash_is_required_and_symlinks_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive = root / "release.tar.gz"
            self.make_archive(archive, [("release/LedgeSync", b"binary", "file")])
            self.assertEqual(verify_archive(archive, sha256(archive)), sha256(archive))
            for digest in ("0" * 64, "not-a-hash"):
                with self.assertRaises(ValueError):
                    verify_archive(archive, digest)
            link = root / "alias"
            link.symlink_to(archive)
            with self.assertRaises(ValueError):
                verify_archive(link, sha256(archive))

    def test_extract_rejects_escape_links_special_nodes_and_duplicates(self):
        cases = [
            [("release/../../escape", b"x", "file")],
            [("/release/absolute", b"x", "file")],
            [("different/file", b"x", "file")],
            [("release/link", b"", "link")],
            [("release/fifo", b"", "special")],
            [("release/a", b"x", "file"), ("release/a", b"y", "file")],
        ]
        for entries in cases:
            with self.subTest(entries=entries), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                archive = root / "input.tar.gz"
                self.make_archive(archive, entries)
                with self.assertRaises(ValueError):
                    extract_release(archive, root / "stage", "release")
                self.assertFalse((root / "escape").exists())

    def test_extract_preserves_binary_bytes(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive = root / "input.tar.gz"
            self.make_archive(archive, [("release/LedgeSync", b"unchanged\x00bytes", "file")])
            stage = extract_release(archive, root / "stage", "release")
            self.assertEqual((stage / "LedgeSync").read_bytes(), b"unchanged\x00bytes")
            self.assertTrue((stage / "LedgeSync").stat().st_mode & 0o111)

    def test_elf_architecture_guard(self):
        with tempfile.TemporaryDirectory() as temporary:
            binary = Path(temporary) / "binary"
            for architecture, machine in (("amd64", 62), ("arm64", 183)):
                header = bytearray(64)
                header[:7] = b"\x7fELF\x02\x01\x01"
                struct.pack_into("<HH", header, 16, 2, machine)
                binary.write_bytes(header)
                binary.chmod(0o755)
                self.assertEqual(verify_elf(binary, architecture), sha256(binary))
                with self.assertRaises(ValueError):
                    verify_elf(binary, "amd64" if architecture == "arm64" else "arm64")
            binary.write_bytes(b"not ELF")
            with self.assertRaises(ValueError):
                verify_elf(binary, "amd64")

    def test_dynamic_dependencies_fail_closed(self):
        with patch("package_deb.run", return_value=" 0 (NEEDED) Shared library: [libc.so.6]\n"):
            self.assertEqual(needed_libraries(Path("binary")), ["libc.so.6"])
        with patch("package_deb.run", return_value=" 0 (RUNPATH) Library runpath: [/untrusted]\n"):
            with self.assertRaises(ValueError):
                needed_libraries(Path("binary"))
        for output in ("", "shlibs:Depends=libc6 (>= 2.34)\n", "shlibs:Depends=${unknown}\n"):
            with tempfile.TemporaryDirectory() as temporary, patch("package_deb.run", return_value=output):
                with self.assertRaises(ValueError):
                    dependencies(Path("binary"), Path(temporary), "amd64", DEFAULT_MAINTAINER)

    def test_publish_cannot_replace_and_rolls_back_only_own_outputs(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "source"
            source.write_bytes(b"new")
            first, existing = root / "first.deb", root / "existing.deb"
            existing.write_bytes(b"published")
            with self.assertRaises(FileExistsError):
                publish_outputs([(source, first), (source, existing)])
            self.assertFalse(first.exists())
            self.assertEqual(existing.read_bytes(), b"published")
            dangling = root / "dangling.sha256"
            dangling.symlink_to(root / "missing")
            with self.assertRaises(FileExistsError):
                publish_outputs([(source, dangling)])
            self.assertTrue(dangling.is_symlink())

    def test_cli_control_is_headless_and_has_no_scripts(self):
        with tempfile.TemporaryDirectory() as temporary:
            stage = Path(temporary)
            binary = stage / "usr/bin/ledgesync"
            binary.parent.mkdir(parents=True)
            binary.write_bytes(b"fixture")
            release = self.release_fixture()
            release["debianVersion"] = debian_version(release["version"])
            control = write_control(stage, "ledgesync-cli", "amd64", "", DEFAULT_MAINTAINER, release)
            self.assertIn("Version: " + release["debianVersion"], control)
            self.assertNotIn("Recommends:", control)
            self.assertNotIn("Depends:", control)
            self.assertEqual({p.name for p in (stage / "DEBIAN").iterdir()}, {"control", "md5sums"})
            self.assertEqual(binary.stat().st_mode & 0o777, 0o755)

    def test_notice_copy_retains_release_inventory(self):
        # Real repository notice manifests exercise their recorded hash checks.
        with tempfile.TemporaryDirectory() as temporary:
            release = Path(temporary) / "release"
            release.mkdir()
            import shutil
            for name in ("LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md"):
                shutil.copyfile(ROOT / name, release / name)
            shutil.copytree(ROOT / "third_party", release / "third_party")
            (release / "README.txt").write_text("synthetic release readme\n", encoding="utf-8")
            doc = Path(temporary) / "doc"
            records = copy_notices(release, doc)
            self.assertGreater(len(records), 10)
            for name, digest in records.items():
                self.assertEqual(sha256(doc / name), digest)
            self.assertEqual((doc / "copyright").read_bytes(), (release / "LICENSE").read_bytes())


if __name__ == "__main__":
    unittest.main()
