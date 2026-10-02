#!/usr/bin/env python3
"""Wrap an existing Windows GUI release in a current-user Inno Setup installer.

Requires Windows and the verified Inno Setup 7.1.0 compiler. Never rebuilds or
changes the released app, fetches dependencies, signs, or replaces artifacts.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import re
import shutil
import stat
import struct
import subprocess
import sys
import tempfile
import zlib

from verify_licenses import verify_notices

ROOT = Path(__file__).resolve().parents[1]
MACHINES = {"amd64": 0x8664, "arm64": 0xAA64}
NOTICE_FILES = ("LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md")


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def verify_pe(path: Path, architecture: str) -> None:
    """Reject a CLI, wrong CPU, truncated header, or non-PE32+ executable."""
    if architecture not in MACHINES:
        raise ValueError("Architecture must be amd64 or arm64")
    if path.is_symlink() or not path.is_file():
        raise ValueError("Expected a regular LedgeSync.exe file")
    with path.open("rb") as source:
        dos = source.read(64)
        if len(dos) != 64 or dos[:2] != b"MZ":
            raise ValueError("Missing Windows DOS header")
        offset = struct.unpack_from("<I", dos, 60)[0]
        if offset < 64 or offset > path.stat().st_size - 94:
            raise ValueError("Invalid Windows PE header offset")
        source.seek(offset)
        header = source.read(94)
    if header[:4] != b"PE\0\0":
        raise ValueError("Missing Windows PE signature")
    machine = struct.unpack_from("<H", header, 4)[0]
    optional_size = struct.unpack_from("<H", header, 20)[0]
    magic = struct.unpack_from("<H", header, 24)[0]
    subsystem = struct.unpack_from("<H", header, 92)[0]
    if machine != MACHINES[architecture] or optional_size < 70 or magic != 0x20B or subsystem != 2:
        raise ValueError(f"Expected a {architecture} PE32+ graphical application, got machine={machine:#x}, subsystem={subsystem}")


def selected_files(root: Path) -> dict[str, str]:
    """Snapshot only the desktop app and its release notices; no CLI or extras."""
    files: dict[str, str] = {}
    casefolded: set[str] = set()
    candidates = [root / "LedgeSync.exe", *(root / name for name in NOTICE_FILES)]
    third_party = root / "third_party"
    if third_party.is_symlink() or not third_party.is_dir():
        raise ValueError("third_party must be an ordinary directory")
    candidates.extend(sorted(third_party.rglob("*")))
    for path in candidates:
        metadata = path.lstat()
        if stat.S_ISLNK(metadata.st_mode) or getattr(metadata, "st_file_attributes", 0) & 0x400:
            raise ValueError(f"Reparse points and symlinks are not permitted: {path}")
        if stat.S_ISDIR(metadata.st_mode):
            continue
        if not stat.S_ISREG(metadata.st_mode):
            raise ValueError(f"Not a regular payload file: {path}")
        relative = path.relative_to(root).as_posix()
        if relative.casefold() in casefolded:
            raise ValueError(f"Case-insensitive payload collision: {relative}")
        casefolded.add(relative.casefold())
        files[relative] = sha256(path)
    return files


def output_paths(output: Path, version: str, architecture: str) -> tuple[Path, Path]:
    if architecture not in MACHINES:
        raise ValueError("Architecture must be amd64 or arm64")
    if len(version) > 64 or not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?", version):
        raise ValueError("Version must be a filename-safe semantic version")
    if any(int(part) > 65535 for part in version.split("-", 1)[0].split(".")):
        raise ValueError("Windows version components must fit in 16 bits")
    installer = output / f"LedgeSync-{version}-windows-{architecture}-setup.exe"
    checksum = installer.with_name(installer.name + ".sha256")
    for path in (installer, checksum):
        if os.path.lexists(path):
            raise ValueError(f"Refusing to replace existing output: {path}")
    return installer, checksum


def create_icon(output: Path) -> None:
    """Render the existing application-owned vector mark using only stdlib."""
    shape = json.loads((ROOT / "frontend/native/mark.json").read_text())
    def png(size: int) -> bytes:
        scale = shape["size"] / size
        rows = bytearray()
        def distance(x, y, a, b):
            dx, dy = b[0] - a[0], b[1] - a[1]
            t = max(0, min(1, ((x - a[0]) * dx + (y - a[1]) * dy) / (dx * dx + dy * dy)))
            return (x - a[0] - t * dx) ** 2 + (y - a[1] - t * dy) ** 2
        for y in range(size):
            rows.append(0)
            for x in range(size):
                channels, count = [0, 0, 0], 0
                for sy in (0.25, 0.75):
                    for sx in (0.25, 0.75):
                        px, py = (x + sx) * scale, (y + sy) * scale
                        inset, radius, full = shape["inset"], shape["radius"], shape["size"]
                        dx = max(inset + radius - px, 0, px - (full - inset - radius))
                        dy = max(inset + radius - py, 0, py - (full - inset - radius))
                        if dx * dx + dy * dy > radius * radius:
                            continue
                        points = shape["points"]
                        stroke = any(distance(px, py, a, b) <= (shape["strokeWidth"] / 2) ** 2 for a, b in zip(points, points[1:]))
                        color = shape["stroke"] if stroke else shape["background"]
                        channels = [channels[c] + color[c] for c in range(3)]
                        count += 1
                rows.extend([*(math.floor(c / count + 0.5) if count else 0 for c in channels), math.floor(count * 255 / 4 + 0.5)])
        def chunk(kind: bytes, data: bytes) -> bytes:
            return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))
        return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(rows, 9)) + chunk(b"IEND", b"")
    sizes = (16, 32, 48, 256)
    images = [png(size) for size in sizes]
    offset = 6 + 16 * len(images)
    directory = bytearray(struct.pack("<HHH", 0, 1, len(images)))
    for size, content in zip(sizes, images):
        directory.extend(struct.pack("<BBBBHHII", size % 256, size % 256, 0, 0, 1, 32, len(content), offset))
        offset += len(content)
    output.write_bytes(directory + b"".join(images))


def publish_pair(installer: Path, digest: Path, destination: Path, checksum: Path) -> None:
    # Staging lives on the same filesystem; hard links provide no-replace semantics.
    os.link(digest, checksum)
    try:
        os.link(installer, destination)
    except BaseException:
        checksum.unlink()
        raise


def package(desktop_root: Path, architecture: str, version: str, output: Path, iscc: Path) -> Path:
    if sys.platform != "win32":
        raise ValueError("Windows installer compilation requires Windows")
    if desktop_root.is_symlink() or not desktop_root.is_dir():
        raise ValueError("--desktop-root must be an ordinary extracted release directory")
    desktop_root = desktop_root.resolve(strict=True)
    output = output.resolve()
    iscc = iscc.resolve(strict=True)
    if iscc.name.lower() != "iscc.exe":
        raise ValueError("Expected the pinned Inno Setup ISCC.exe compiler")
    if output.is_relative_to(desktop_root):
        raise ValueError("Outputs must be outside the immutable desktop release directory")
    installer, checksum = output_paths(output, version, architecture)
    verify_notices(desktop_root)
    verify_pe(desktop_root / "LedgeSync.exe", architecture)
    original = selected_files(desktop_root)
    # Verify the actual invoked compiler, independently of where it was installed.
    probe = subprocess.run([str(iscc), "--version"], capture_output=True, check=False, timeout=30)
    banner = (probe.stdout + probe.stderr).decode(errors="replace")
    if probe.returncode != 0 or not re.fullmatch(r"7\.1\.0\s*", banner):
        raise ValueError("The compiler must be Inno Setup 7.1.0")
    output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".ledgesync-setup-", dir=output) as temporary:
        work = Path(temporary)
        stage = work / "payload"
        stage.mkdir()
        for relative in original:
            target = stage / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(desktop_root / relative, target)
        if selected_files(stage) != original:
            raise ValueError("Staged payload changed the released bytes")
        verify_notices(stage)
        (stage / "INSTALL.txt").write_text(
            f"LedgeSync {version} — Windows {'ARM64' if architecture == 'arm64' else 'Intel / AMD x64'}\n\n"
            "This wizard installs LedgeSync for your current Windows account.\n"
            "Choose an installation folder; a Start menu entry is created.\n"
            "A desktop shortcut and opening the app after setup are optional.\n"
            "Uninstall using Windows Settings > Apps > Installed apps.\n"
            "Uninstall removes installed program files, preserving user-created data.\n\n"
            "Requires Windows 11 and Microsoft Edge WebView2 Runtime.\n"
            "Server 2022+ with a desktop may run setup; Server GUI acceptance is not\n"
            "claimed. Use the separate CLI on Server Core and headless machines.\n"
            "If WebView2 is missing, obtain Microsoft's Evergreen Runtime from\n"
            "https://developer.microsoft.com/microsoft-edge/webview2/\n\n"
            "Connect Google Drive in your browser, choose My Drive or an existing\n"
            "parent folder, preview the included files, then approve Upload folder.\n"
            "Included files and empty folders retain the local root's hierarchy.\n"
            "The app verifies completed copies and reuses unchanged files. Changed\n"
            "files keep both versions; no existing file is overwritten or deleted.\n"
            "Keep the app open during upload. Cancellation leaves completed copies\n"
            "in Drive; after cancellation or restart, preview again to continue.\n"
            "Credentials use your Windows Credential Manager; no plaintext token\n"
            "fallback is provided. Source folders remain read-only.\n"
            "No shared-drive support, startup item, service, scheduled task,\n"
            "preauthorized account, or CLI is installed.\n\n"
            "Developer distribution: this installer and app have no trusted publisher\n"
            "signature. Windows security policies may prevent running them.\n"
            "Do not disable operating-system security protections.\n\n"
            "Website: https://ledgesync.com\n"
            "Source: https://github.com/alexandroit/LedgeSync\n"
            "Application: Apache-2.0; bundled dependency notices accompany this app.\n"
            "Installer built with Inno Setup 7.1.0: https://jrsoftware.org/\n",
            encoding="utf-8",
        )
        shutil.copy2(ROOT / "deploy/windows/INNO_SETUP_LICENSE.txt", stage / "INNO_SETUP_LICENSE.txt")
        icon = work / "LedgeSync.ico"
        create_icon(icon)
        definitions = {
            "PayloadDir": stage, "PackageVersion": version,
            "NumericVersion": version.split("-", 1)[0] + ".0",
            "AllowedArchitecture": "arm64" if architecture == "arm64" else "x64compatible and not arm64",
            "OutputName": installer.stem, "PackageOutput": work,
            "InstallerIcon": icon,
        }
        if any(any(c in str(value) for c in ('"', '\r', '\n')) for value in definitions.values()):
            raise ValueError("Packaging paths must not contain quotes or line breaks")
        subprocess.run([str(iscc), "/Qp", *(f"/D{key}={value}" for key, value in definitions.items()), str(ROOT / "deploy/windows/LedgeSync.iss")], check=True, timeout=600)
        generated = work / installer.name
        if not generated.is_file() or generated.stat().st_size == 0:
            raise ValueError("Compiler did not produce the requested installer")
        verify_pe(generated, "amd64")  # Inno's installer is x64; ARM64 payload runs natively.
        if selected_files(desktop_root) != original or selected_files(stage) != original:
            raise ValueError("Source or staged release changed while compiling")
        digest = work / checksum.name
        digest.write_text(f"{sha256(generated)}  {installer.name}\n", encoding="utf-8")
        publish_pair(generated, digest, installer, checksum)
    print(f"Packaged verified {architecture} GUI and immutable notices: {installer}")
    return installer


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--desktop-root", type=Path, required=True)
    parser.add_argument("--arch", choices=MACHINES, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--iscc", type=Path, required=True)
    args = parser.parse_args()
    try:
        package(args.desktop_root, args.arch, args.version, args.output, args.iscc)
    except (OSError, ValueError, subprocess.SubprocessError) as exc:
        print(f"Windows installer packaging failed: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
