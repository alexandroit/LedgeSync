#!/usr/bin/env python3
"""Guard native vault build selection and truthful CLI archive instructions."""
import contextlib
import io
from pathlib import Path
import tempfile
import tarfile
import unittest
from unittest.mock import patch

import package_cli as packaging


class NativeCLIPackagingTests(unittest.TestCase):
    def test_native_targets_keep_their_supported_vault_backend(self):
        for system, arch in packaging.TARGETS:
            with self.subTest(system=system, arch=arch), patch.dict("os.environ", {"CGO_ENABLED": "unexpected"}):
                env = packaging.build_environment(system, arch, True, (system, arch))
                self.assertEqual(env["GOOS"], system)
                self.assertEqual(env["GOARCH"], arch)
                self.assertEqual(env["CGO_ENABLED"], "1" if system == "darwin" else "0")

    def test_native_packaging_rejects_cross_compilation(self):
        for host in [("linux", "arm64"), ("darwin", "amd64")]:
            with self.subTest(host=host), self.assertRaises(ValueError):
                packaging.build_environment("darwin", "arm64", True, host)

    def test_portable_build_remains_explicitly_unconfigured(self):
        env = packaging.build_environment("darwin", "arm64", False, ("linux", "amd64"))
        self.assertEqual(env["CGO_ENABLED"], "0")
        self.assertIn("Unconfigured developer build", packaging.readme("test", False, False))
        self.assertNotIn("Connect Google Drive with browser consent", packaging.readme("test", True, False))

    def test_official_native_build_refuses_missing_client_before_building(self):
        with tempfile.TemporaryDirectory() as directory, patch.object(packaging, "ROOT", Path(directory)), \
             patch("sys.argv", ["package_cli.py", "--platform", "darwin/arm64", "--native", "--require-oauth-client"]), \
             patch.object(packaging.subprocess, "check_output") as go, contextlib.redirect_stderr(io.StringIO()), \
             self.assertRaises(SystemExit) as failure:
            packaging.main()
        self.assertEqual(failure.exception.code, 2)
        go.assert_not_called()

    def test_configured_client_cannot_leak_into_portable_archives(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client = root / "internal/connections/oauth_client_generated.go"
            client.parent.mkdir(parents=True)
            client.write_text("synthetic build marker")
            with patch.object(packaging, "ROOT", root), patch("sys.argv", ["package_cli.py"]), \
                 patch.object(packaging.subprocess, "check_output") as go, contextlib.redirect_stderr(io.StringIO()), \
                 self.assertRaises(SystemExit) as failure:
                packaging.main()
            self.assertEqual(failure.exception.code, 2)
            go.assert_not_called()

    def test_official_archive_build_includes_oauth_tag_and_native_keychain(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client = root / "internal/connections/oauth_client_generated.go"
            client.parent.mkdir(parents=True)
            client.write_text("synthetic build marker")
            for name in ("LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md"):
                (root / name).write_text("synthetic notice")
            (root / "third_party").mkdir()
            output = root / "packages"

            def build(command, **kwargs):
                self.assertEqual(command[command.index("-tags") + 1], "oauth")
                self.assertEqual(kwargs["env"]["CGO_ENABLED"], "1")
                Path(command[command.index("-o") + 1]).write_bytes(b"synthetic native binary")

            with patch.object(packaging, "ROOT", root), patch("sys.argv", ["package_cli.py", "--platform", "darwin/arm64", "--native", "--require-oauth-client", "--output", str(output)]), \
                 patch.object(packaging, "verify_notices"), patch.object(packaging.subprocess, "check_output", return_value="darwin\narm64\n"), \
                 patch.object(packaging.subprocess, "run", side_effect=build), contextlib.redirect_stdout(io.StringIO()):
                packaging.main()
            archive = next(output.glob("*.tar.gz"))
            with tarfile.open(archive) as bundle:
                readme = next(member for member in bundle.getmembers() if member.name.endswith("/README.txt"))
                self.assertIn(b"Connect Google Drive with browser consent", bundle.extractfile(readme).read())


if __name__ == "__main__":
    unittest.main()
