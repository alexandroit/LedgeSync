#!/usr/bin/env python3
"""Archive a native Wails build, retaining bundle permissions inside the archive."""
import argparse
import hashlib
from pathlib import Path
import shutil
import tarfile
import tempfile
import zipfile
from verify_licenses import verify_notices

ROOT = Path(__file__).resolve().parents[1]
TARGETS = {
    "macos-15": "darwin-arm64", "macos-15-intel": "darwin-amd64",
    "ubuntu-24.04": "linux-amd64", "ubuntu-24.04-arm": "linux-arm64",
    "windows-2022": "windows-amd64", "windows-11-arm": "windows-arm64",
}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--runner", required=True, choices=TARGETS)
    parser.add_argument("--version", default="0.1.0-alpha.7")
    args = parser.parse_args()
    for value in (args.runner, args.version):
        if not all(c.isalnum() or c in ".-" for c in value):
            parser.error("unsafe package name")
    verify_notices(ROOT)
    source = ROOT / "build" / "bin"
    if args.runner.startswith("macos-"):
        bundle = source / "LedgeSync.app"
        if not (bundle / "Contents/MacOS/LedgeSync").is_file():
            parser.error("LedgeSync macOS application bundle is missing")
    elif args.runner.startswith("windows-"):
        bundle = source / "LedgeSync.exe"
    elif args.runner.startswith("ubuntu-"):
        bundle = source / "LedgeSync"
    else:
        parser.error("runner must identify a supported macOS, Windows or Ubuntu target")
    if not bundle.exists():
        parser.error("expected native desktop build output is missing")
    output = ROOT / "build" / "packages"
    output.mkdir(parents=True, exist_ok=True)
    name = f"ledgesync-desktop-{args.version}-{TARGETS[args.runner]}"
    with tempfile.TemporaryDirectory(prefix="ledgesync-desktop-") as temp:
        stage = Path(temp) / name
        stage.mkdir()
        if bundle.is_dir():
            shutil.copytree(bundle, stage / bundle.name, symlinks=True)
        else:
            shutil.copy2(bundle, stage / bundle.name)
        for filename in ("LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md"):
            shutil.copy2(ROOT / filename, stage / filename)
        shutil.copytree(ROOT / "third_party", stage / "third_party")
        (stage / "README.txt").write_text(
            f"LedgeSync {args.version} developer alpha desktop\n\n"
            "Browse local files and manually copy an approved folder to Google Drive.\n"
            "Open Connections and choose Connect Google Drive to authorize in your browser.\n"
            "Choose My Drive or an existing parent with the browser folder picker.\n"
            "Preview folder upload, review included/excluded items, then choose Upload folder.\n"
            "The local root and included empty folders retain their hierarchy inside the parent.\n"
            "Verified copies are reused; changed files keep both versions. No overwrite or deletion.\n"
            "Keep the app open. After cancellation or restart, preview again to reconcile and continue.\n"
            "No scheduler, watcher, shared-drive upload or background service is enabled.\n"
            "The native CLI uses the same approval/copy engine and OS credential vault for interactive server use.\n"
            "Guide: https://github.com/alexandroit/LedgeSync/blob/main/docs/GOOGLE_DRIVE_AUTH.md\n"
            "Developer builds are unsigned and are not notarized.\n"
            "See https://github.com/alexandroit/LedgeSync/blob/main/docs/PLATFORMS.md\n",
            encoding="utf-8",
        )
        if args.runner.startswith("windows"):
            archive = output / (name + ".zip")
            with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as z:
                for file in sorted(stage.rglob("*")):
                    if file.is_file():
                        z.write(file, file.relative_to(stage.parent))
        else:
            archive = output / (name + ".tar.gz")
            with tarfile.open(archive, "w:gz") as tar:
                tar.add(stage, arcname=name)
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    (output / (archive.name + ".sha256")).write_text(f"{digest}  {archive.name}\n", encoding="utf-8")
    print(archive.name)


if __name__ == "__main__":
    main()
