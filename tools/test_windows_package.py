#!/usr/bin/env python3
"""Portable release guard tests; native installer behavior is tested by PowerShell."""
from pathlib import Path
import struct
import tempfile
import unittest

from package_windows import MACHINES, output_paths, publish_pair, selected_files, verify_pe


class WindowsPackagingGuards(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="ledgesync-windows-guards-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)

    def executable(self, arch="amd64", subsystem=2):
        data = bytearray(256)
        data[:2] = b"MZ"
        struct.pack_into("<I", data, 60, 64)
        data[64:68] = b"PE\0\0"
        struct.pack_into("<H", data, 68, MACHINES[arch])
        struct.pack_into("<H", data, 84, 240)
        struct.pack_into("<H", data, 88, 0x20B)
        struct.pack_into("<H", data, 156, subsystem)
        executable = self.root / "LedgeSync.exe"
        executable.write_bytes(data)
        return executable

    def test_rejects_wrong_architecture_cli_and_truncated_payload(self):
        for arch in MACHINES:
            executable = self.executable(arch)
            verify_pe(executable, arch)
            with self.assertRaises(ValueError):
                verify_pe(executable, "arm64" if arch == "amd64" else "amd64")
        with self.assertRaises(ValueError):
            verify_pe(self.executable(subsystem=3), "amd64")
        executable.write_bytes(b"MZ")
        with self.assertRaises(ValueError):
            verify_pe(executable, "amd64")

    def test_rejects_unsafe_version_and_existing_artifact_or_checksum(self):
        for bad in ("../release", "1.0", "1.0.0\n", "65536.0.0", '1.0.0"'):
            with self.assertRaises(ValueError):
                output_paths(self.root, bad, "arm64")
        artifact, checksum = output_paths(self.root, "0.1.0-alpha.1", "arm64")
        checksum.write_text("existing")
        with self.assertRaises(ValueError):
            output_paths(self.root, "0.1.0-alpha.1", "arm64")
        checksum.unlink()
        artifact.write_bytes(b"existing")
        with self.assertRaises(ValueError):
            output_paths(self.root, "0.1.0-alpha.1", "arm64")

    def test_publication_cannot_replace_concurrent_artifact(self):
        generated, digest = self.root / "temporary.exe", self.root / "temporary.sha256"
        generated.write_bytes(b"new")
        digest.write_bytes(b"new digest")
        destination, checksum = self.root / "release.exe", self.root / "release.sha256"
        destination.write_bytes(b"published")
        with self.assertRaises(FileExistsError):
            publish_pair(generated, digest, destination, checksum)
        self.assertEqual(destination.read_bytes(), b"published")
        self.assertFalse(checksum.exists())

    def test_snapshot_rejects_notice_symlink(self):
        self.executable()
        for name in ("LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md"):
            (self.root / name).write_text(name)
        (self.root / "third_party").mkdir()
        (self.root / "third_party/license").write_text("notice")
        self.assertEqual(len(selected_files(self.root)), 5)
        try:
            (self.root / "third_party/link").symlink_to(self.root / "NOTICE")
        except OSError:
            self.skipTest("Host does not permit symlinks")
        with self.assertRaises(ValueError):
            selected_files(self.root)


if __name__ == "__main__":
    unittest.main()
