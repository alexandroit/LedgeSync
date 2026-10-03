#!/usr/bin/env python3
"""Wrap the released Windows desktop app in a Microsoft Store MSIX bundle.

Requires Windows and the Windows SDK (makeappx.exe and makepri.exe). The
verified x64 and ARM64 release payloads are packaged unchanged; this never
rebuilds the app. The bundle is unsigned because the Microsoft Store signs it
on publication. Self-signed certificates are never used.
"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import re
import shutil
import struct
import subprocess
import tempfile
from string import Template
from xml.sax.saxutils import escape
import zlib

from package_windows import NOTICE_FILES, sha256, verify_pe

ROOT = Path(__file__).resolve().parents[1]
MANIFEST_TEMPLATE = ROOT / "deploy/msix/AppxManifest.xml.in"
MARK = ROOT / "frontend/native/mark.json"
ARCHITECTURES = {"amd64": "x64", "arm64": "arm64"}
PRERELEASE_OFFSETS = {"alpha": 0, "beta": 300, "rc": 600}
IDENTITY_KEYS = {"identityName", "publisher", "publisherDisplayName"}
# Logo references in deploy/msix/AppxManifest.xml.in, resolved through resources.pri.
MANIFEST_LOGOS = ("StoreLogo.png", "Square44x44Logo.png", "Square150x150Logo.png", "Wide310x150Logo.png")
# Used only to prove the packaging pipeline before the Store identity is known.
# Partner Center rejects it, and its outputs are marked as not for upload.
VALIDATION_IDENTITY = {
    "identityName": "LedgeSync.Validation",
    "publisher": "CN=LedgeSync Validation",
    "publisherDisplayName": "LedgeSync validation build",
}


def msix_version(version: str) -> str:
    """Map a release version to the Store's four-part package version.

    The Store requires the first part to be at least 1 and reserves the fourth
    part, which stays 0. MAJOR.MINOR.PATCH-alpha.N becomes
    (MAJOR+1).MINOR.(PATCH*1000+N).0; beta adds 300, rc adds 600 and a final
    release uses 999, so package versions always increase.
    """
    match = re.fullmatch(r"(\d+)\.(\d+)\.(\d+)(?:-(alpha|beta|rc)\.(\d+))?", version)
    if not match:
        raise ValueError("Version must be MAJOR.MINOR.PATCH, optionally with -alpha.N, -beta.N or -rc.N")
    major, minor, patch = (int(match.group(i)) for i in (1, 2, 3))
    if match.group(4):
        number = int(match.group(5))
        if not 1 <= number <= 299:
            raise ValueError("Pre-release numbers must be between 1 and 299")
        build = patch * 1000 + PRERELEASE_OFFSETS[match.group(4)] + number
    else:
        build = patch * 1000 + 999
    if max(major + 1, minor, build) > 65535:
        raise ValueError("Package version parts must not exceed 65535")
    return f"{major + 1}.{minor}.{build}.0"


def load_identity(path: Path) -> dict[str, str]:
    """Read the Store identity from Partner Center's Product identity page."""
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict) or set(data) != IDENTITY_KEYS or not all(isinstance(v, str) for v in data.values()):
        raise ValueError("The identity file must hold the strings identityName, publisher and publisherDisplayName")
    return check_identity(data)


def check_identity(identity: dict[str, str]) -> dict[str, str]:
    name, publisher, display = identity["identityName"], identity["publisher"], identity["publisherDisplayName"]
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9.-]{1,48}[A-Za-z0-9]", name):
        raise ValueError("identityName must be Package/Identity/Name from Partner Center")
    if not publisher.startswith("CN=") or len(publisher) > 8192 or any(ord(c) < 32 for c in publisher):
        raise ValueError("publisher must be Package/Identity/Publisher from Partner Center (it starts with CN=)")
    if not display.strip() or len(display) > 256 or any(ord(c) < 32 for c in display):
        raise ValueError("publisherDisplayName must be Package/Properties/PublisherDisplayName from Partner Center")
    return dict(identity)


def render_manifest(identity: dict[str, str], version: str, architecture: str) -> str:
    """Fill the manifest template; every value is XML-escaped."""
    if architecture not in ARCHITECTURES.values():
        raise ValueError("Architecture must be x64 or arm64")
    values = {
        "identity_name": identity["identityName"],
        "publisher": identity["publisher"],
        "publisher_display_name": identity["publisherDisplayName"],
        "version": version,
        "architecture": architecture,
    }
    return Template(MANIFEST_TEMPLATE.read_text(encoding="utf-8")).substitute(
        {key: escape(value, {'"': "&quot;"}) for key, value in values.items()})


def _segment_distance(x: float, y: float, a: list[float], b: list[float]) -> float:
    dx, dy = b[0] - a[0], b[1] - a[1]
    t = max(0.0, min(1.0, ((x - a[0]) * dx + (y - a[1]) * dy) / (dx * dx + dy * dy)))
    return (x - a[0] - t * dx) ** 2 + (y - a[1] - t * dy) ** 2


def render_icon(icon: int) -> list[bytes]:
    """Rasterize the application mark at icon x icon pixels (RGBA rows).

    Same vector mark and rules as frontend/scripts/prepare-native.mjs, with 4x4
    supersampling so small sizes stay sharp.
    """
    shape = json.loads(MARK.read_text(encoding="utf-8"))
    size, inset, radius = shape["size"], shape["inset"], shape["radius"]
    background, stroke, points = shape["background"], shape["stroke"], shape["points"]
    limit = (shape["strokeWidth"] / 2) ** 2
    scale = size / icon
    offsets = [(i + 0.5) / 4 for i in range(4)]
    rows = []
    for y in range(icon):
        row = bytearray()
        for x in range(icon):
            red = green = blue = covered = 0
            for sy in offsets:
                py = (y + sy) * scale
                for sx in offsets:
                    px = (x + sx) * scale
                    dx = max(inset + radius - px, 0, px - (size - inset - radius))
                    dy = max(inset + radius - py, 0, py - (size - inset - radius))
                    if dx * dx + dy * dy > radius * radius:
                        continue
                    on_stroke = any(_segment_distance(px, py, points[i], points[i + 1]) <= limit for i in range(len(points) - 1))
                    color = stroke if on_stroke else background
                    red, green, blue, covered = red + color[0], green + color[1], blue + color[2], covered + 1
            if covered:
                row += bytes((round(red / covered), round(green / covered), round(blue / covered), round(255 * covered / 16)))
            else:
                row += b"\0\0\0\0"
        rows.append(bytes(row))
    return rows


def png(width: int, height: int, icon_rows: list[bytes]) -> bytes:
    """Center the icon rows on a transparent width x height canvas as a PNG."""
    icon = len(icon_rows)
    if icon > width or icon > height:
        raise ValueError("The icon must fit inside the canvas")
    left, top = (width - icon) // 2, (height - icon) // 2
    raw = bytearray()
    for y in range(height):
        raw.append(0)
        if top <= y < top + icon:
            raw += b"\0" * (4 * left) + icon_rows[y - top] + b"\0" * (4 * (width - left - icon))
        else:
            raw += b"\0" * (4 * width)

    def chunk(kind: bytes, data: bytes) -> bytes:
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))

    header = struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", header) + chunk(b"IDAT", zlib.compress(bytes(raw), 9)) + chunk(b"IEND", b"")


def asset_plan() -> dict[str, tuple[int, int, int]]:
    """Logo files as name -> (canvas width, canvas height, icon size).

    Names carry MRT qualifiers (scale, target size); resources.pri lets Windows
    pick the sharpest one for each Assets\\Name.png reference in the manifest.
    """
    plan = {
        "StoreLogo.scale-100.png": (50, 50, 50),
        "StoreLogo.scale-200.png": (100, 100, 100),
        "Square44x44Logo.scale-100.png": (44, 44, 44),
        "Square44x44Logo.scale-200.png": (88, 88, 88),
        "Square150x150Logo.scale-100.png": (150, 150, 100),
        "Square150x150Logo.scale-200.png": (300, 300, 200),
        "Wide310x150Logo.scale-100.png": (310, 150, 100),
        "Wide310x150Logo.scale-200.png": (620, 300, 200),
    }
    for size in (16, 24, 32, 48, 256):
        plan[f"Square44x44Logo.targetsize-{size}.png"] = (size, size, size)
        plan[f"Square44x44Logo.targetsize-{size}_altform-unplated.png"] = (size, size, size)
    return plan


def write_assets(directory: Path) -> list[str]:
    directory.mkdir(parents=True)
    icons: dict[int, list[bytes]] = {}
    for name, (width, height, icon) in sorted(asset_plan().items()):
        if icon not in icons:
            icons[icon] = render_icon(icon)
        (directory / name).write_bytes(png(width, height, icons[icon]))
    return sorted(asset_plan())


def sdk_tool(name: str, override: Path | None) -> Path:
    """Find the newest Windows SDK copy of makeappx.exe or makepri.exe."""
    if override:
        return override.resolve(strict=True)
    kits = Path(os.environ.get("ProgramFiles(x86)", r"C:\Program Files (x86)")) / "Windows Kits" / "10" / "bin"
    candidates = [path for path in kits.glob(f"10.*/x64/{name}") if path.is_file()]
    if not candidates:
        raise ValueError(f"{name} from the Windows SDK was not found")
    return max(candidates, key=lambda path: tuple(int(part) for part in path.parent.parent.name.split(".")))


def run(command: list[str | Path]) -> None:
    result = subprocess.run([str(part) for part in command], capture_output=True, text=True, check=False, timeout=600)
    if result.returncode != 0:
        raise RuntimeError(f"{Path(command[0]).name} failed:\n{result.stdout}\n{result.stderr}")


def pri_config(makepri: Path, path: Path) -> None:
    """Create makepri's default configuration without resource-package splits.

    The default splits scales and languages into resource-package PRI files.
    This bundle ships no resource packages, so every candidate must stay in
    the main resources.pri.
    """
    run([makepri, "createconfig", "/cf", path, "/dq", "en-US", "/pv", "10.0.0", "/o"])
    text = re.sub(r"\s*<packaging>.*?</packaging>", "", path.read_text(encoding="utf-8-sig"), flags=re.S)
    if "<packaging" in text or "autoResourcePackage" in text or text.count('root="\\" startIndexAt="\\"') != 1:
        raise ValueError("Unexpected makepri configuration layout")
    path.write_text(text, encoding="utf-8")


def index_assets(makepri: Path, config: Path, stage: Path, index_root: Path, dump: Path) -> None:
    """Build resources.pri for the logos only, named as the manifest refers to them.

    Indexing a copy of Assets from its parent keeps resource names relative to
    the package root (Files/Assets/StoreLogo.png), which is how Windows and
    Partner Center resolve "Assets\\StoreLogo.png", without indexing the app's
    license files.
    """
    shutil.copytree(stage / "Assets", index_root / "Assets")
    run([makepri, "new", "/pr", index_root, "/cf", config, "/mn", stage / "AppxManifest.xml", "/of", stage / "resources.pri", "/o"])
    split = sorted(path.name for path in stage.glob("resources*.pri") if path.name != "resources.pri")
    if split:
        raise ValueError(f"Unexpected resource-package PRI files: {', '.join(split)}")
    run([makepri, "dump", "/if", stage / "resources.pri", "/of", dump, "/o"])
    text = dump.read_text(encoding="utf-8-sig", errors="replace")
    missing = [f"Files/Assets/{name}" for name in MANIFEST_LOGOS if f"/Files/Assets/{name}" not in text]
    missing += [f"Assets\\{name}" for name in asset_plan() if f"Assets\\{name}" not in text]
    if missing:
        raise ValueError(f"resources.pri lacks {', '.join(missing)}; see {dump.name}")


def package(inputs: Path, identity: dict[str, str], version: str, output: Path, makeappx: Path, makepri: Path, validation: bool) -> dict:
    package_version = msix_version(version)
    output.mkdir(parents=True, exist_ok=True)
    suffix = "-validation-not-for-upload" if validation else ""
    bundle = output / f"LedgeSync-{version}-store{suffix}.msixbundle"
    if bundle.exists():
        raise ValueError(f"Refusing to replace {bundle}")
    with tempfile.TemporaryDirectory(prefix="ledgesync-msix-") as temporary:
        work = Path(temporary)
        config = work / "priconfig.xml"
        pri_config(makepri, config)
        packages = work / "packages"
        packages.mkdir()
        sources = {}
        for arch, msix_arch in ARCHITECTURES.items():
            payload = inputs / f"windows-{arch}" / "desktop"
            verify_pe(payload / "LedgeSync.exe", arch)
            missing = [name for name in NOTICE_FILES if not (payload / name).is_file()]
            if missing:
                raise ValueError(f"The {arch} payload lacks {', '.join(missing)}")
            stage = work / f"stage-{msix_arch}"
            shutil.copytree(payload, stage, symlinks=False)
            write_assets(stage / "Assets")
            (stage / "AppxManifest.xml").write_text(render_manifest(identity, package_version, msix_arch), encoding="utf-8")
            index_assets(makepri, config, stage, work / f"index-{msix_arch}", output / f"resources-{msix_arch}{suffix}.pri.xml")
            run([makeappx, "pack", "/d", stage, "/p", packages / f"LedgeSync_{package_version}_{msix_arch}.msix", "/o"])
            inputs_file = inputs / f"windows-{arch}" / "inputs.json"
            sources[msix_arch] = json.loads(inputs_file.read_text(encoding="utf-8"))["desktop"] if inputs_file.is_file() else None
        run([makeappx, "bundle", "/d", packages, "/p", bundle, "/bv", package_version, "/o"])
    report = {
        "product": "LedgeSync",
        "version": version,
        "packageVersion": package_version,
        "identity": identity,
        "uploadable": not validation,
        "signed": False,
        "signing": "The Microsoft Store signs the package on publication; no certificate is used here.",
        "architectures": sorted(ARCHITECTURES.values()),
        "bundle": {"name": bundle.name, "sha256": sha256(bundle), "size": bundle.stat().st_size},
        "sourceArchives": sources,
    }
    (output / f"STORE-PACKAGE{suffix}.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    return report


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True, help="Released application version, for example 0.1.0-alpha.8")
    parser.add_argument("--inputs", type=Path, default=ROOT / "build/installer-inputs", help="Output of fetch_installer_inputs.py for both Windows architectures")
    parser.add_argument("--identity", type=Path, help="Store identity JSON (deploy/msix/identity.json)")
    parser.add_argument("--validation", action="store_true", help="Prove the pipeline with a placeholder identity; the result is not for upload")
    parser.add_argument("--output", type=Path, default=ROOT / "build/msix")
    parser.add_argument("--makeappx", type=Path)
    parser.add_argument("--makepri", type=Path)
    args = parser.parse_args()
    if bool(args.identity) == args.validation:
        parser.error("Use exactly one of --identity FILE or --validation")
    identity = check_identity(VALIDATION_IDENTITY) if args.validation else load_identity(args.identity)
    report = package(args.inputs, identity, args.version, args.output, sdk_tool("makeappx.exe", args.makeappx), sdk_tool("makepri.exe", args.makepri), args.validation)
    print(json.dumps(report["bundle"]))


if __name__ == "__main__":
    main()
