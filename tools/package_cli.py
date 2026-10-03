#!/usr/bin/env python3
"""Build native or unconfigured portable CLI archives without global tools."""
import argparse
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import zipfile
from verify_licenses import verify_notices

ROOT = Path(__file__).resolve().parents[1]
TARGETS = [(system, arch) for system in ("darwin", "linux", "windows") for arch in ("amd64", "arm64")]


def build_environment(system, arch, native, host):
    if native and (system, arch) != tuple(host):
        raise ValueError("native CLI packaging requires the matching host OS and architecture")
    return {**os.environ, "GOOS": system, "GOARCH": arch,
            "CGO_ENABLED": "1" if native and system == "darwin" else "0",
            "GOTOOLCHAIN": "go1.27.1"}


def readme(version, native, configured):
    text = f"LedgeSync {version} CLI\n\n"
    if native and configured:
        text += (
            "Connect Google Drive with browser consent, then preview and explicitly approve a folder copy.\n"
            "Use ledgesync --help for authentication, destination selection and copy commands.\n"
            "Copies use the same engine as the desktop: source files are read-only, unchanged copies\n"
            "are verified and skipped, and changed files keep both versions. No overwrite or deletion.\n"
            "No unattended scheduler, watcher or system service is installed.\n"
            "Credentials require macOS Keychain, Windows Credential Manager or Linux Secret Service.\n"
            "A locked/unavailable vault blocks authorization; there is no plaintext fallback.\n"
            "A headless server needs an interactive user session, native vault and browser loopback\n"
            "access through a local SSH tunnel. See the CLI guide before connecting.\n"
        )
    else:
        text += (
            "Unconfigured developer build. Local inspection and offline previews are available.\n"
            "Google authentication and copies require a configured native build with an available OS vault.\n"
        )
    return text + (
        "\nUsage: ledgesync --help\n"
        "Documentation: https://github.com/alexandroit/LedgeSync\n"
        "License: Apache-2.0; third-party components retain their own licenses.\n"
    )


def sign_native(system, binary):
    """Sign the CLI with the publisher identity when release signing is configured."""
    if system == "darwin" and os.environ.get("LEDGESYNC_MACOS_IDENTITY"):
        subprocess.run([sys.executable, str(ROOT / "tools/sign_macos.py"), "--cli-only", "--cli", str(binary)], check=True)
    elif system == "windows" and os.environ.get("LEDGESYNC_WINDOWS_SIGNING"):
        subprocess.run(["pwsh", "-NoProfile", "-File", str(ROOT / "tools/sign_windows.ps1"), "-Path", str(binary)], check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", default="0.1.0-alpha.7")
    parser.add_argument("--platform", choices=["/".join(t) for t in TARGETS])
    parser.add_argument("--native", action="store_true", help="build with the native credential vault, on a matching host")
    parser.add_argument("--require-oauth-client", action="store_true", help="fail if publisher client injection is missing")
    parser.add_argument("--output", type=Path, default=ROOT / "build" / "releases")
    args = parser.parse_args()
    if not all(c.isalnum() or c in ".-" for c in args.version):
        parser.error("version must contain only letters, digits, dots, and hyphens")
    if args.native and not args.platform:
        parser.error("--native requires an explicit --platform")
    configured = (ROOT / "internal/connections/oauth_client_generated.go").is_file()
    if args.require_oauth_client and (not args.native or not configured):
        parser.error("official CLI packaging requires a configured native build")
    if configured and not args.native:
        parser.error("remove generated OAuth configuration before portable developer packaging")
    host = subprocess.check_output(["go", "env", "GOHOSTOS", "GOHOSTARCH"], cwd=ROOT, text=True).split()
    verify_notices(ROOT)
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    archives = []
    for system, arch in ([tuple(args.platform.split("/"))] if args.platform else TARGETS):
        name = f"ledgesync-{args.version}-{system}-{arch}"
        with tempfile.TemporaryDirectory(prefix="ledgesync-package-") as tmp:
            stage = Path(tmp) / name
            stage.mkdir()
            exe = "ledgesync.exe" if system == "windows" else "ledgesync"
            try:
                env = build_environment(system, arch, args.native, host)
            except ValueError as error:
                parser.error(str(error))
            command = ["go", "build", "-trimpath"]
            if args.native:
                command += ["-tags", "oauth"]
            subprocess.run(command + ["-o", str(stage / exe), "./cmd/ledgesync"], cwd=ROOT, env=env, check=True)
            sign_native(system, stage / exe)
            for filename in ("LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md"):
                shutil.copy2(ROOT / filename, stage / filename)
            shutil.copytree(ROOT / "third_party", stage / "third_party")
            (stage / "README.txt").write_text(readme(args.version, args.native, configured), encoding="utf-8")
            if system == "windows":
                archive = output / (name + ".zip")
                with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as z:
                    for file in sorted(stage.rglob("*")):
                        if file.is_file():
                            z.write(file, file.relative_to(stage.parent))
            else:
                archive = output / (name + ".tar.gz")
                with tarfile.open(archive, "w:gz") as tar:
                    tar.add(stage, arcname=name)
            archives.append(archive)
            print(f"Built {archive.name}", flush=True)
    lines = [f"{hashlib.sha256(file.read_bytes()).hexdigest()}  {file.name}\n" for file in archives]
    (output / "SHA256SUMS").write_text("".join(lines), encoding="utf-8")


if __name__ == "__main__":
    main()
