#!/usr/bin/env python3
"""Repackage verified, immutable LedgeSync alpha archives on native Ubuntu 24.04.

No application rebuild, binary patching, service, or maintainer script is used.
The desktop's shared-library dependencies come from dpkg-shlibdeps on the target
architecture. Missing libraries or dependency metadata are fatal.
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
import struct
import subprocess
import sys
import tarfile
import tempfile

from verify_licenses import verify_notices

ROOT = Path(__file__).resolve().parents[1]
DEFAULT_MANIFEST = ROOT / "deploy/installers/source-release.json"
ELF_MACHINES = {"amd64": 62, "arm64": 183}
MAX_ARCHIVE_BYTES = 512 * 1024 * 1024
MAX_MEMBERS = 10000
REQUIRED_DESKTOP_LIBRARIES = {"libgtk-3.so.0", "libwebkit2gtk-4.1.so.0"}
DEFAULT_MAINTAINER = "LedgeSync maintainers <alexandroit@users.noreply.github.com>"


def debian_version(version: str) -> str:
    if not isinstance(version, str) or len(version) > 64 or not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?", version):
        raise ValueError("Release version must be a filename-safe semantic version")
    return version.replace("-", "~", 1) + "-1"


def load_release(path: Path = DEFAULT_MANIFEST, expected_version: str | None = None) -> dict:
    if path.is_symlink() or not path.is_file() or path.stat().st_size > 1024 * 1024:
        raise ValueError("Release manifest must be a bounded regular file")
    raw = path.read_bytes()
    release = json.loads(raw)
    if not isinstance(release, dict):
        raise ValueError("Release manifest must be a JSON object")
    version = release.get("version")
    release["debianVersion"] = debian_version(version)
    if expected_version is not None and version != expected_version:
        raise ValueError("Requested version differs from the pinned source-release manifest")
    if release.get("tag") != "v" + version:
        raise ValueError("Release tag differs from its version")
    commit = release.get("applicationSourceCommit")
    if not isinstance(commit, str) or not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError("Release must pin its full application source commit")
    epoch = release.get("sourceDateEpoch")
    if type(epoch) is not int or not 0 < epoch < 2**32:
        raise ValueError("Release must pin its application commit sourceDateEpoch")
    targets = release.get("targets")
    if not isinstance(targets, dict) or not targets:
        raise ValueError("Release has no pinned target archives")
    for target, entries in targets.items():
        if target not in {"linux-amd64", "linux-arm64", "windows-amd64", "windows-arm64"}:
            raise ValueError("Unsupported installer target")
        required = {"desktop", "cli"} if target.startswith("linux-") else {"desktop"}
        if not isinstance(entries, dict) or set(entries) != required:
            raise ValueError("Incomplete target archive inventory")
        for kind, entry in entries.items():
            if not isinstance(entry, dict):
                raise ValueError("Release archive metadata must be a JSON object")
            prefix = "ledgesync-desktop" if kind == "desktop" else "ledgesync"
            suffix = ".zip" if target.startswith("windows-") else ".tar.gz"
            name = f"{prefix}-{version}-{target}{suffix}"
            url = f"https://github.com/alexandroit/LedgeSync/releases/download/v{version}/{name}"
            if entry.get("name") != name or entry.get("url") != url:
                raise ValueError("Archive name or URL differs from its pinned release")
            digest = entry.get("sha256")
            if not isinstance(digest, str) or not re.fullmatch(r"[0-9a-f]{64}", digest):
                raise ValueError("Release archive must have an exact SHA-256")
            if type(entry.get("size")) is not int or not 0 < entry["size"] <= MAX_ARCHIVE_BYTES:
                raise ValueError("Release archive must have a bounded pinned size")
    release["manifestSha256"] = hashlib.sha256(raw).hexdigest()
    return release


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def run(arguments: list[str], *, cwd: Path | None = None, timeout: int = 120, source_date_epoch: int | None = None) -> str:
    env = {**os.environ, "LC_ALL": "C"}
    if source_date_epoch is not None:
        env["SOURCE_DATE_EPOCH"] = str(source_date_epoch)
    # Dependency inspection must use the native package database, never a user
    # library overlay or architecture override inherited from a build session.
    for name in ("LD_LIBRARY_PATH", "LD_PRELOAD", "DPKG_ADMINDIR", "DPKG_ROOT",
                 "DEB_HOST_ARCH", "DEB_HOST_ARCH_CPU", "DEB_HOST_GNU_TYPE"):
        env.pop(name, None)
    result = subprocess.run(arguments, cwd=cwd, env=env, text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            check=False, timeout=timeout)
    if result.returncode:
        raise ValueError(f"{arguments[0]} failed ({result.returncode}): {result.stderr.strip()}")
    if result.stderr.strip():
        print(result.stderr.strip(), file=sys.stderr)
    return result.stdout


def require_native_ubuntu(architecture: str) -> None:
    if sys.platform != "linux":
        raise ValueError("Debian packaging requires native Ubuntu 24.04")
    release = {}
    for line in Path("/etc/os-release").read_text(encoding="utf-8").splitlines():
        if "=" in line:
            key, value = line.split("=", 1)
            release[key] = value.strip('"')
    if release.get("ID") != "ubuntu" or release.get("VERSION_ID") != "24.04":
        raise ValueError("Only Ubuntu 24.04 dependency metadata is validated by this packaging profile")
    if run(["dpkg", "--print-architecture"]).strip() != architecture:
        raise ValueError("Build on the matching native architecture; cross-library guesses are forbidden")
    for tool in ("dpkg-deb", "dpkg-shlibdeps", "readelf", "desktop-file-validate"):
        if shutil.which(tool) is None:
            raise ValueError(f"Required packaging tool is missing: {tool}")


def verify_archive(path: Path, expected: str) -> str:
    if not re.fullmatch(r"[0-9a-f]{64}", expected):
        raise ValueError("Expected archive SHA-256 must be 64 lowercase hexadecimal characters")
    if path.is_symlink() or not path.is_file() or path.stat().st_size > MAX_ARCHIVE_BYTES:
        raise ValueError("Archive must be a bounded regular file, not a symlink")
    actual = sha256(path)
    if actual != expected:
        raise ValueError(f"Archive SHA-256 mismatch: {path.name}")
    return actual


def extract_release(archive: Path, destination: Path, expected_root: str) -> Path:
    """Extract only bounded regular files/directories into a fresh private root."""
    destination.mkdir(mode=0o700)
    seen: set[str] = set()
    total = 0
    with tarfile.open(archive, "r:gz") as source:
        for index, member in enumerate(source):
            if index >= MAX_MEMBERS:
                raise ValueError("Release archive has too many members")
            name = member.name.rstrip("/")
            parts = name.split("/")
            if (not name or name.startswith("/") or "\\" in name or ":" in name
                    or any(p in ("", ".", "..") for p in parts)
                    or any(ord(c) < 32 or ord(c) == 127 for c in name)
                    or parts[0] != expected_root or name in seen):
                raise ValueError(f"Unsafe, unexpected, or duplicate archive path: {name!r}")
            if not (member.isdir() or member.isreg()) or member.mode & 0o7000:
                raise ValueError(f"Archive links, special nodes, and privileged modes are forbidden: {name}")
            seen.add(name)
            total += member.size
            if total > MAX_ARCHIVE_BYTES or member.size < 0:
                raise ValueError("Extracted archive exceeds the size limit")
            target = destination.joinpath(*parts)
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
                target.chmod(0o755)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                contents = source.extractfile(member)
                if contents is None:
                    raise ValueError(f"Missing archive data: {name}")
                with contents, target.open("xb") as output:
                    shutil.copyfileobj(contents, output, length=1024 * 1024)
                if target.stat().st_size != member.size:
                    raise ValueError(f"Truncated archive data: {name}")
                target.chmod(member.mode & 0o777)
    root = destination / expected_root
    if not root.is_dir():
        raise ValueError("Expected release archive root is missing")
    return root


def verify_elf(binary: Path, architecture: str) -> str:
    if binary.is_symlink() or not binary.is_file() or not binary.stat().st_mode & 0o111:
        raise ValueError("Release executable is missing or not executable")
    with binary.open("rb") as source:
        header = source.read(64)
    if len(header) != 64 or header[:7] != b"\x7fELF\x02\x01\x01":
        raise ValueError("Expected a 64-bit little-endian ELF executable")
    file_type, machine = struct.unpack_from("<HH", header, 16)
    if file_type not in (2, 3) or machine != ELF_MACHINES[architecture]:
        raise ValueError(f"ELF architecture does not match {architecture}")
    return sha256(binary)


def needed_libraries(binary: Path) -> list[str]:
    dynamic = run(["readelf", "--wide", "--dynamic", str(binary)])
    if re.search(r"\((RPATH|RUNPATH)\)", dynamic):
        raise ValueError("Unexpected runtime library search path in immutable release binary")
    return sorted(set(re.findall(r"\(NEEDED\).*?\[([^\]]+)\]", dynamic)))


def dependencies(binary: Path, work: Path, architecture: str, maintainer: str) -> str:
    debian = work / "debian"
    debian.mkdir()
    (debian / "control").write_text(
        f"Source: ledgesync\nSection: utils\nPriority: optional\nMaintainer: {maintainer}\n\n"
        f"Package: ledgesync\nArchitecture: {architecture}\nDepends: ${{shlibs:Depends}}\n"
        "Description: LedgeSync offline desktop\n Native dependency discovery only.\n",
        encoding="utf-8",
    )
    output = run(["dpkg-shlibdeps", "-O", "-dDepends", "-e" + str(binary)], cwd=work)
    lines = [line.removeprefix("shlibs:Depends=") for line in output.splitlines()
             if line.startswith("shlibs:Depends=")]
    if len(lines) != 1 or not lines[0] or "\n" in lines[0] or "${" in lines[0]:
        raise ValueError("dpkg-shlibdeps did not produce a complete dependency list")
    result = lines[0]
    names = set(re.findall(r"(?:^|[,|])\s*([a-z0-9][a-z0-9+.-]+)", result))
    # These names are checks of native resolver output, not substitutes for it.
    if not {"libgtk-3-0t64", "libwebkit2gtk-4.1-0"}.issubset(names):
        raise ValueError("Native dependency output lacks the Ubuntu 24.04 GTK3/WebKit4.1 packages")
    return result


def copy_notices(release: Path, doc: Path) -> dict[str, str]:
    verify_notices(release)
    doc.mkdir(parents=True)
    for name in ("LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md", "README.txt"):
        shutil.copyfile(release / name, doc / name)
    shutil.copytree(release / "third_party", doc / "third_party")
    shutil.copyfile(release / "LICENSE", doc / "copyright")
    expected = {file.relative_to(release).as_posix(): sha256(file)
                for file in sorted(release.rglob("*")) if file.is_file()
                and (file.relative_to(release).parts[0] == "third_party"
                     or file.name in ("LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md", "README.txt"))}
    for relative, digest in expected.items():
        if sha256(doc / relative) != digest:
            raise ValueError(f"Notice changed while staging: {relative}")
    verify_notices(doc)
    return expected


def write_control(stage: Path, package: str, architecture: str, depends: str, maintainer: str, release: dict) -> str:
    if package not in ("ledgesync", "ledgesync-cli"):
        raise ValueError("Unexpected binary package")
    control = stage / "DEBIAN"
    control.mkdir(mode=0o755)
    size = sum(math.ceil(file.stat().st_size / 1024) for file in stage.rglob("*") if file.is_file())
    description = ("local file policy explorer and desktop previews" if package == "ledgesync"
                   else "headless local file policy inspection and previews")
    text = (f"Package: {package}\nVersion: {release['debianVersion']}\nArchitecture: {architecture}\n"
            f"Section: utils\nPriority: optional\nMaintainer: {maintainer}\n"
            f"Installed-Size: {size}\nHomepage: https://ledgesync.com\n"
            f"X-LedgeSync-Source-Revision: {release['applicationSourceCommit']}\n")
    if depends:
        text += f"Depends: {depends}\n"
    if package == "ledgesync":
        text += "Recommends: gnome-keyring\n"
    text += (f"Description: {description}\n"
             " Browse local files, inspect Gitignore and rclone filter policies, and\n"
             " preview a simulated destination using the shared LedgeSync engine.\n"
             " This alpha does not transfer files to Google Drive, run a\n"
             " background service, schedule jobs, overwrite data, or delete files.\n")
    (control / "control").write_text(text, encoding="utf-8")
    lines = []
    for file in sorted(stage.rglob("*")):
        if file.is_file() and control not in file.parents:
            digest = hashlib.md5(file.read_bytes(), usedforsecurity=False).hexdigest()
            lines.append(f"{digest}  {file.relative_to(stage).as_posix()}\n")
    (control / "md5sums").write_text("".join(lines), encoding="utf-8")
    for directory in (stage, *[p for p in stage.rglob("*") if p.is_dir()]):
        directory.chmod(0o755)
    for file in stage.rglob("*"):
        if file.is_file():
            file.chmod(0o755 if file.parent == stage / "usr/bin" else 0o644)
        os.utime(file, (release["sourceDateEpoch"], release["sourceDateEpoch"]), follow_symlinks=False)
    os.utime(stage, (release["sourceDateEpoch"], release["sourceDateEpoch"]))
    return text


def publish_outputs(files: list[tuple[Path, Path]]) -> None:
    """Publish the batch with no-replace hard links; roll back only our own links."""
    published = []
    try:
        for source, destination in files:
            os.link(source, destination)
            published.append(destination)
    except BaseException:
        for destination in reversed(published):
            destination.unlink()
        raise


def package(architecture: str, desktop_archive: Path, desktop_hash: str,
            cli_archive: Path, cli_hash: str, output: Path, maintainer: str,
            manifest_path: Path = DEFAULT_MANIFEST, version: str | None = None) -> list[Path]:
    release = load_release(manifest_path, version)
    if architecture not in ELF_MACHINES:
        raise ValueError("Architecture must be amd64 or arm64")
    if not re.fullmatch(r"[^\r\n<>]+ <[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]+@[A-Za-z0-9.-]+>", maintainer):
        raise ValueError("Maintainer must have a single-line Name <email> form")
    entries = release["targets"]["linux-" + architecture]
    for kind, archive, digest in (("desktop", desktop_archive, desktop_hash), ("cli", cli_archive, cli_hash)):
        entry = entries[kind]
        if archive.name != entry["name"] or digest != entry["sha256"] or archive.stat().st_size != entry["size"]:
            raise ValueError("Input archive differs from the pinned source-release manifest")
    require_native_ubuntu(architecture)
    desktop_archive = desktop_archive.absolute()
    cli_archive = cli_archive.absolute()
    verify_archive(desktop_archive, desktop_hash)
    verify_archive(cli_archive, cli_hash)
    output = output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    names = [f"{name}_{release['debianVersion']}_{architecture}.deb" for name in ("ledgesync", "ledgesync-cli")]
    metadata_name = f"packaging-{architecture}.json"
    sums_name = f"SHA256SUMS-{architecture}"
    destinations = [output / name for name in names + [n + ".sha256" for n in names] + [metadata_name, sums_name]]
    if any(os.path.lexists(path) for path in destinations):
        raise ValueError("Refusing to replace an existing Debian package, checksum, or build report")
    with tempfile.TemporaryDirectory(prefix=".ledgesync-deb-", dir=output) as temporary:
        work = Path(temporary)
        desktop = extract_release(desktop_archive, work / "desktop-source", f"ledgesync-desktop-{release['version']}-linux-{architecture}")
        cli = extract_release(cli_archive, work / "cli-source", f"ledgesync-{release['version']}-linux-{architecture}")
        desktop_binary, cli_binary = desktop / "LedgeSync", cli / "ledgesync"
        desktop_binary_hash = verify_elf(desktop_binary, architecture)
        cli_binary_hash = verify_elf(cli_binary, architecture)
        desktop_needed = needed_libraries(desktop_binary)
        cli_needed = needed_libraries(cli_binary)
        if not REQUIRED_DESKTOP_LIBRARIES.issubset(desktop_needed):
            raise ValueError("Release desktop does not link the expected GTK3/WebKit4.1 libraries")
        if cli_needed:
            raise ValueError("The released CLI must be static and free of graphical dependencies")
        if run([str(cli_binary), "--version"]).strip() != "LedgeSync " + release["version"]:
            raise ValueError("CLI version differs from the immutable release")
        desktop_depends = dependencies(desktop_binary, work, architecture, maintainer)
        manifest = {"schemaVersion": 1, "releaseVersion": release["version"],
                    "debianVersion": release["debianVersion"], "sourceRevision": release["applicationSourceCommit"],
                    "sourceDateEpoch": release["sourceDateEpoch"], "sourceManifestSha256": release["manifestSha256"], "architecture": architecture,
                    "distribution": "ubuntu", "distributionVersion": "24.04",
                    "inputArchives": {"desktop": {"name": desktop_archive.name, "sha256": desktop_hash},
                                      "cli": {"name": cli_archive.name, "sha256": cli_hash}},
                    "packages": []}
        generated = []
        for package_name, source_tree, source_binary, binary_hash, needed in (
                ("ledgesync", desktop, desktop_binary, desktop_binary_hash, desktop_needed),
                ("ledgesync-cli", cli, cli_binary, cli_binary_hash, cli_needed)):
            stage = work / package_name
            (stage / "usr/bin").mkdir(parents=True)
            binary_name = "ledgesync-desktop" if package_name == "ledgesync" else "ledgesync"
            installed_binary = stage / "usr/bin" / binary_name
            shutil.copyfile(source_binary, installed_binary)
            notices = copy_notices(source_tree, stage / "usr/share/doc" / package_name)
            depends = ""
            if package_name == "ledgesync":
                depends = f"ledgesync-cli (= {release['debianVersion']}), {desktop_depends}"
                applications = stage / "usr/share/applications"
                applications.mkdir(parents=True)
                desktop_file = applications / "com.ledgesync.app.desktop"
                shutil.copyfile(ROOT / "deploy/linux/com.ledgesync.app.desktop", desktop_file)
                icons = stage / "usr/share/icons/hicolor/scalable/apps"
                icons.mkdir(parents=True)
                shutil.copyfile(ROOT / "deploy/linux/ledgesync.svg", icons / "ledgesync.svg")
                run(["desktop-file-validate", str(desktop_file)])
            control = write_control(stage, package_name, architecture, depends, maintainer, release)
            archive = work / f"{package_name}_{release['debianVersion']}_{architecture}.deb"
            run(["dpkg-deb", "--root-owner-group", "--uniform-compression", "--threads-max=1", "-Zxz", "--build", str(stage), str(archive)], timeout=300, source_date_epoch=release["sourceDateEpoch"])
            extracted = work / (package_name + "-verified")
            run(["dpkg-deb", "--raw-extract", str(archive), str(extracted)])
            if sha256(extracted / "usr/bin" / binary_name) != binary_hash:
                raise ValueError("Published application bytes changed inside the Debian package")
            if (extracted / "DEBIAN/control").read_text(encoding="utf-8") != control:
                raise ValueError("Debian control metadata changed")
            if {p.name for p in (extracted / "DEBIAN").iterdir()} != {"control", "md5sums"}:
                raise ValueError("Unexpected maintainer scripts or control files")
            doc = extracted / "usr/share/doc" / package_name
            verify_notices(doc)
            for relative, digest in notices.items():
                if sha256(doc / relative) != digest:
                    raise ValueError(f"Notice changed inside package: {relative}")
            digest = sha256(archive)
            checksum = archive.with_name(archive.name + ".sha256")
            checksum.write_text(f"{digest}  {archive.name}\n", encoding="utf-8")
            generated.extend((archive, checksum))
            manifest["packages"].append({"package": package_name, "file": archive.name,
                "version": release["debianVersion"], "architecture": architecture, "sha256": digest,
                "size": archive.stat().st_size, "depends": depends,
                "recommends": "gnome-keyring" if package_name == "ledgesync" else "", "binaryPath": "usr/bin/" + binary_name,
                "binarySha256": binary_hash, "neededLibraries": needed, "noticeSha256": notices})
        # Verify both original downloads and staged executable inputs again before publication.
        verify_archive(desktop_archive, desktop_hash)
        verify_archive(cli_archive, cli_hash)
        if sha256(desktop_binary) != desktop_binary_hash or sha256(cli_binary) != cli_binary_hash:
            raise ValueError("Input executable changed during packaging")
        metadata = work / metadata_name
        metadata.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        sums = work / sums_name
        sums.write_text("".join(f"{p['sha256']}  {p['file']}\n" for p in manifest["packages"]), encoding="utf-8")
        generated.extend((metadata, sums))
        publish_outputs([(path, output / path.name) for path in generated])
    return [output / name for name in names]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--arch", choices=ELF_MACHINES, required=True)
    parser.add_argument("--desktop-archive", type=Path, required=True)
    parser.add_argument("--desktop-sha256", required=True)
    parser.add_argument("--cli-archive", type=Path, required=True)
    parser.add_argument("--cli-sha256", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--maintainer", default=DEFAULT_MAINTAINER)
    parser.add_argument("--manifest", type=Path, default=DEFAULT_MANIFEST)
    parser.add_argument("--version", help="Require the pinned release version to match")
    args = parser.parse_args()
    try:
        packages = package(args.arch, args.desktop_archive, args.desktop_sha256,
                           args.cli_archive, args.cli_sha256, args.output, args.maintainer, args.manifest, args.version)
    except (ValueError, OSError, tarfile.TarError, subprocess.TimeoutExpired) as exc:
        print(f"Debian packaging failed: {exc}", file=sys.stderr)
        return 1
    for path in packages:
        print(path)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
