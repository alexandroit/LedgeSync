#!/usr/bin/env python3
"""Package and verify an existing LedgeSync macOS app without modifying it.

Requires macOS native tools. The app is copied byte-for-byte into a compressed,
read-only HFS+ image with an Applications link and distribution notices. This
does not sign, notarize, install, run, or change security attributes on the app.
"""
from __future__ import annotations

import argparse
import hashlib
import os
from pathlib import Path
import plistlib
import re
import shutil
import stat
import subprocess
import sys
import tempfile

from verify_licenses import verify_notices

ROOT = Path(__file__).resolve().parents[1]
ARCHITECTURES = {"arm64": "arm64", "amd64": "x86_64"}


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def run(tool: str, *arguments: str, timeout: int = 600) -> bytes:
    """Only invoke fixed native tool paths and argument arrays, never a shell."""
    result = subprocess.run(
        [f"/usr/bin/{tool}", *arguments], check=False,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout,
    )
    if result.returncode:
        message = result.stderr.decode("utf-8", errors="replace").strip()
        raise ValueError(f"{tool} failed (exit {result.returncode}): {message}")
    return result.stdout


def bundle_snapshot(bundle: Path) -> dict[str, tuple]:
    """Record all bytes, modes, and internal symlink targets without following links."""
    bundle = bundle.resolve(strict=True)
    entries: dict[str, tuple] = {}

    def inspect(path: Path) -> None:
        relative = path.relative_to(bundle).as_posix()
        metadata = path.lstat()
        mode = stat.S_IMODE(metadata.st_mode)
        if stat.S_ISLNK(metadata.st_mode):
            target = os.readlink(path)
            if not path.resolve(strict=True).is_relative_to(bundle):
                raise ValueError(f"Application symlink escapes the bundle: {relative}")
            entries[relative] = ("symlink", target)
        elif stat.S_ISDIR(metadata.st_mode):
            entries[relative] = ("directory", mode)
            for child in sorted(path.iterdir()):
                inspect(child)
        elif stat.S_ISREG(metadata.st_mode):
            entries[relative] = ("file", mode, metadata.st_size, sha256(path))
        else:
            raise ValueError(f"Unsupported node in application: {relative}")

    inspect(bundle)
    return entries


def output_paths(output: Path, version: str, architecture: str) -> tuple[Path, Path]:
    if len(version) > 64 or not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?", version):
        raise ValueError("Version must be a filename-safe semantic version, such as 0.1.0-alpha.1")
    if architecture not in ARCHITECTURES:
        raise ValueError("Architecture must be arm64 or amd64")
    dmg = output / f"LedgeSync-{version}-macos-{architecture}.dmg"
    checksum = dmg.with_name(dmg.name + ".sha256")
    for path in (dmg, checksum):
        if os.path.lexists(path):
            raise ValueError(f"Refusing to replace an existing output: {path}")
    return dmg, checksum


def verify_bundle(bundle: Path, architecture: str, version: str) -> None:
    with (bundle / "Contents/Info.plist").open("rb") as source:
        info = plistlib.load(source)
    if info.get("CFBundleIdentifier") != "com.ledgesync.app" or info.get("CFBundleExecutable") != "LedgeSync":
        raise ValueError("Expected the existing com.ledgesync.app / LedgeSync application")
    if info.get("CFBundleShortVersionString") != version.split("-", 1)[0]:
        raise ValueError("Application bundle version does not match the requested package version")
    if info.get("LSMinimumSystemVersion") not in ("13.0", "13.0.0"):
        raise ValueError("Expected the documented macOS 13 minimum")
    executable = bundle / "Contents/MacOS/LedgeSync"
    actual = run("lipo", "-archs", str(executable)).decode().strip().split()
    if actual != [ARCHITECTURES[architecture]]:
        raise ValueError(f"Application architecture {actual!r} does not match {architecture}")
    run("codesign", "--verify", "--deep", "--strict", str(bundle))


def publish_pair(image: Path, digest: Path, destination: Path, checksum: Path) -> None:
    """Publish with filesystem no-replace semantics, even under concurrent calls.

    Temporary files live beside the destination, so hard links stay on one
    filesystem. A filesystem without hard-link support fails without replacing
    existing artifacts. Never use rename/replace to overwrite release files.
    """
    os.link(digest, checksum)
    try:
        os.link(image, destination)
    except BaseException:
        checksum.unlink()
        raise


def package(app: Path, architecture: str, version: str, output: Path, notices_root: Path) -> Path:
    if sys.platform != "darwin":
        raise ValueError("DMG creation and verification require macOS")
    if app.is_symlink() or app.name != "LedgeSync.app" or not app.is_dir():
        raise ValueError("--app must name an existing LedgeSync.app directory, not a symlink")
    app = app.resolve(strict=True)
    output = output.resolve()
    if output.is_relative_to(app):
        raise ValueError("The output directory must be outside the existing app bundle")
    image, checksum = output_paths(output, version, architecture)
    notices_root = notices_root.resolve(strict=True)
    verify_notices(notices_root)
    verify_bundle(app, architecture, version)
    original = bundle_snapshot(app)
    output.mkdir(parents=True, exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix=".ledgesync-dmg-", dir=output))
    # Mount on the local system volume. macOS refuses mounts beneath some
    # external/file-provider directories even when writing the DMG there works.
    # Image/checksum temporaries remain beside output for no-replace hard links.
    mount_work = Path(tempfile.mkdtemp(prefix="ledgesync-dmg-verify-", dir="/tmp")).resolve()
    mount = mount_work / "mounted"
    mount.mkdir()
    stage = work / "staging"
    stage.mkdir()
    temporary_image = work / image.name
    temporary_checksum = work / checksum.name
    attached_disk = None
    detach_failed = False
    try:
        staged_app = stage / "LedgeSync.app"
        run("ditto", "--rsrc", "--extattr", "--qtn", "--acl", str(app), str(staged_app))
        if bundle_snapshot(staged_app) != original:
            raise ValueError("Staged app differs from the source bundle")
        verify_bundle(staged_app, architecture, version)
        (stage / "Applications").symlink_to("/Applications", target_is_directory=True)
        for name in ("LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md"):
            shutil.copy2(notices_root / name, stage / name)
        shutil.copytree(notices_root / "third_party", stage / "third_party", symlinks=True)
        verify_notices(stage)
        (stage / "README.txt").write_text(
            f"LedgeSync {version} — macOS {'Apple Silicon (ARM64)' if architecture == 'arm64' else 'Intel (x64)'}\n\n"
            "Requires macOS 13 or later.\n\n"
            "Installation\n"
            "1. Drag LedgeSync.app to the Applications shortcut.\n"
            "2. Eject this disk image.\n"
            "3. Open LedgeSync from Applications.\n\n"
            "Offline alpha: browse local files, inspect ignore rules, and preview\n"
            "a simulated destination. Google Drive transfers are not enabled.\n"
            "No service, login item, account connection, or schedule is installed.\n\n"
            "Developer distribution: no trusted publisher signature or notarization\n"
            "is provided by this packaging process. The existing application's\n"
            "signature is preserved; current alpha apps use an ad-hoc signature.\n"
            "macOS may prevent opening this build. Do not disable operating-system\n"
            "security protections; a reviewed source build is the development option.\n\n"
            "Project: https://github.com/alexandroit/LedgeSync\n"
            "Website: https://ledgesync.com\n"
            "License and third-party notices accompany the app in this image.\n",
            encoding="utf-8",
        )
        run("hdiutil", "create", "-volname", f"LedgeSync {version}", "-srcfolder", str(stage), "-fs", "HFS+", "-format", "UDZO", "-nospotlight", str(temporary_image))
        run("hdiutil", "verify", str(temporary_image))
        try:
            attached = plistlib.loads(run("hdiutil", "attach", "-readonly", "-nobrowse", "-noautoopen", "-mountpoint", str(mount), "-plist", str(temporary_image)))
            devices = [entity.get("dev-entry", "") for entity in attached.get("system-entities", [])]
            attached_disk = next((device for device in devices if re.fullmatch(r"/dev/disk[0-9]+", device)), None)
            if attached_disk is None:
                attached_disk = next((device for device in devices if re.fullmatch(r"/dev/disk[0-9]+s[0-9]+", device)), None)
            if not any(entity.get("mount-point") == str(mount) for entity in attached.get("system-entities", [])):
                raise ValueError("Disk image did not mount at the private verification path")
            mounted_app = mount / "LedgeSync.app"
            if bundle_snapshot(mounted_app) != original:
                raise ValueError("App content or permissions changed inside the disk image")
            verify_bundle(mounted_app, architecture, version)
            if not (mount / "Applications").is_symlink() or os.readlink(mount / "Applications") != "/Applications":
                raise ValueError("Missing or incorrect Applications shortcut")
            verify_notices(mount)
            if (mount / "README.txt").read_bytes() != (stage / "README.txt").read_bytes():
                raise ValueError("Installation README changed inside the disk image")
        finally:
            detach_target = attached_disk or (str(mount) if os.path.ismount(mount) else None)
            if detach_target:
                try:
                    run("hdiutil", "detach", detach_target, timeout=60)
                except (ValueError, OSError, subprocess.TimeoutExpired):
                    detach_failed = True
                    raise
        if bundle_snapshot(app) != original:
            raise ValueError("Source application changed while packaging; no artifact published")
        digest = sha256(temporary_image)
        temporary_checksum.write_text(f"{digest}  {image.name}\n", encoding="utf-8")
        publish_pair(temporary_image, temporary_checksum, image, checksum)
    finally:
        # Never recurse into a mounted volume if detach failed. Leave the private
        # directory for explicit recovery and report it instead of force-detaching.
        if detach_failed or os.path.ismount(mount):
            print(f"Verification device {attached_disk or mount} could not detach; temporary files retained at {work}.", file=sys.stderr)
        else:
            shutil.rmtree(work)
            shutil.rmtree(mount_work)
    print(f"Verified app architecture, signature, bundle bytes/modes, notices, Applications shortcut, and DMG checksum: {image.name}")
    return image


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--app", type=Path, required=True, help="Existing LedgeSync.app; never modified.")
    parser.add_argument("--arch", choices=ARCHITECTURES, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--output", type=Path, required=True, help="Destination directory; existing DMGs/checksums are never replaced.")
    parser.add_argument("--notices-root", type=Path, default=ROOT, help="Matching release notice directory; defaults to this checkout.")
    args = parser.parse_args()
    try:
        package(args.app, args.arch, args.version, args.output, args.notices_root)
    except (ValueError, OSError, RuntimeError, subprocess.TimeoutExpired, plistlib.InvalidFileException) as exc:
        print(f"DMG packaging failed: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
