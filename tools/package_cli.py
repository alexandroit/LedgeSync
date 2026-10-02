#!/usr/bin/env python3
"""Build portable offline CLI archives without installing global tools."""
import argparse
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import zipfile
from verify_licenses import verify_notices

ROOT = Path(__file__).resolve().parents[1]
TARGETS = [(system, arch) for system in ("darwin", "linux", "windows") for arch in ("amd64", "arm64")]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", default="0.1.0-alpha.2")
    args = parser.parse_args()
    if not all(c.isalnum() or c in ".-" for c in args.version):
        parser.error("version must contain only letters, digits, dots, and hyphens")
    verify_notices(ROOT)
    output = ROOT / "build" / "releases"
    output.mkdir(parents=True, exist_ok=True)
    archives = []
    for system, arch in TARGETS:
        name = f"ledgesync-{args.version}-{system}-{arch}"
        with tempfile.TemporaryDirectory(prefix="ledgesync-package-") as tmp:
            stage = Path(tmp) / name
            stage.mkdir()
            exe = "ledgesync.exe" if system == "windows" else "ledgesync"
            env = {**os.environ, "GOOS": system, "GOARCH": arch, "CGO_ENABLED": "0", "GOTOOLCHAIN": "go1.27.1"}
            subprocess.run(["go", "build", "-trimpath", "-o", str(stage / exe), "./cmd/ledgesync"], cwd=ROOT, env=env, check=True)
            for filename in ("LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md"):
                shutil.copy2(ROOT / filename, stage / filename)
            shutil.copytree(ROOT / "third_party", stage / "third_party")
            (stage / "README.txt").write_text("LedgeSync offline alpha CLI\n\nThis build inspects local files and creates previews against a fake destination.\nIt does not connect to Google Drive, upload, overwrite, delete, or run as a service.\n\nUsage: ledgesync --help\nDocumentation: https://github.com/alexandroit/LedgeSync\nLicense: Apache-2.0; third-party components retain their own licenses.\n", encoding="utf-8")
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
