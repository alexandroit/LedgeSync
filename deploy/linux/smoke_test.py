#!/usr/bin/env python3
"""Validate .deb payloads; optionally install/remove only on disposable hosted CI.

This script never authorizes package installation on a production host. Native
installation requires --install AND GitHub's github-hosted runner markers.
"""
from __future__ import annotations

import argparse
import io
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tarfile
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools"))
from package_deb import DEFAULT_MANIFEST, load_release, require_native_ubuntu, sha256, verify_elf
from verify_licenses import verify_notices


def command(arguments: list[str], *, timeout: int = 120, check: bool = True) -> subprocess.CompletedProcess:
    result = subprocess.run(arguments, env={**os.environ, "LC_ALL": "C"},
                            capture_output=True, text=True, timeout=timeout, check=False)
    if check and result.returncode:
        raise ValueError(f"{arguments[0]} failed ({result.returncode}): {result.stderr.strip()}")
    return result


def validate_package(archive: Path, record: dict, architecture: str, work: Path) -> None:
    if archive.is_symlink() or not archive.is_file() or sha256(archive) != record["sha256"]:
        raise ValueError("Debian package does not match its packaging report")
    expected = {"Package": record["package"], "Version": record["version"],
                "Architecture": architecture, "Depends": record["depends"],
                "Recommends": record["recommends"]}
    for field, value in expected.items():
        actual = command(["dpkg-deb", "--field", str(archive), field]).stdout.strip()
        if actual != value:
            raise ValueError(f"Unexpected {field} in {archive.name}")
    raw = subprocess.run(["dpkg-deb", "--fsys-tarfile", str(archive)], capture_output=True, check=True, timeout=120).stdout
    package = record["package"]
    doc_prefix = f"usr/share/doc/{package}/"
    allowed = {record["binaryPath"], doc_prefix + "copyright"}
    allowed.update(doc_prefix + name for name in record["noticeSha256"])
    if package == "ledgesync":
        allowed.update({"usr/share/applications/com.ledgesync.app.desktop",
                        "usr/share/icons/hicolor/scalable/apps/ledgesync.svg"})
    found = set()
    with tarfile.open(fileobj=io.BytesIO(raw)) as contents:
        for entry in contents:
            name = entry.name.removeprefix("./").rstrip("/")
            if name in ("", "."):
                continue
            if entry.uid != 0 or entry.gid != 0 or entry.mode & 0o7000:
                raise ValueError("Package ownership or privileged mode is invalid")
            if entry.isdir():
                if not any(path.startswith(name + "/") for path in allowed):
                    raise ValueError(f"Unexpected installed directory: {name}")
                continue
            if not entry.isreg() or name not in allowed or name in found:
                raise ValueError(f"Unexpected installed file: {name}")
            found.add(name)
    if found != allowed:
        raise ValueError("Package is missing required application or notice files")
    extracted = work / package
    command(["dpkg-deb", "--raw-extract", str(archive), str(extracted)])
    if {p.name for p in (extracted / "DEBIAN").iterdir()} != {"control", "md5sums"}:
        raise ValueError("Maintainer scripts or unexpected control files are forbidden")
    if verify_elf(extracted / record["binaryPath"], architecture) != record["binarySha256"]:
        raise ValueError("Installed binary differs from the original release")
    doc = extracted / "usr/share/doc" / package
    verify_notices(doc)
    for relative, digest in record["noticeSha256"].items():
        if sha256(doc / relative) != digest:
            raise ValueError("A release notice changed in the package")
    if package == "ledgesync":
        command(["desktop-file-validate", str(extracted / "usr/share/applications/com.ledgesync.app.desktop")])
    elif record["depends"] or record["recommends"] or record["neededLibraries"]:
        raise ValueError("The headless CLI unexpectedly has runtime/graphical dependencies")


def require_disposable_runner() -> None:
    if (os.environ.get("GITHUB_ACTIONS") != "true"
            or os.environ.get("RUNNER_ENVIRONMENT") != "github-hosted"):
        raise ValueError("Installation is restricted to an explicitly requested disposable GitHub-hosted runner")
    if os.geteuid() == 0:
        raise ValueError("Run the test as the hosted runner user; only apt commands use sudo")
    for name in ("ledgesync", "ledgesync-cli"):
        result = command(["dpkg-query", "-W", "-f=${Status}", name], check=False)
        if result.returncode == 0 and "installed" in result.stdout:
            raise ValueError(f"Refusing to alter a pre-existing {name} installation")


def gui_startup(work: Path) -> dict:
    for tool in ("xvfb-run", "dbus-run-session"):
        if shutil.which(tool) is None:
            raise ValueError(f"GUI smoke prerequisite missing: {tool}")
    test_home = work / "gui-settings"
    runtime = work / "gui-runtime"
    test_home.mkdir(mode=0o700)
    runtime.mkdir(mode=0o700)
    env = {**os.environ, "XDG_CONFIG_HOME": str(test_home / "config"),
           "XDG_CACHE_HOME": str(test_home / "cache"), "XDG_DATA_HOME": str(test_home / "data"),
           "XDG_RUNTIME_DIR": str(runtime), "GDK_BACKEND": "x11", "LIBGL_ALWAYS_SOFTWARE": "1"}
    process = subprocess.Popen(["xvfb-run", "--auto-servernum", "dbus-run-session", "--", "/usr/bin/ledgesync-desktop"],
                               env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                               text=True, start_new_session=True)
    started = time.monotonic()
    try:
        try:
            output, _ = process.communicate(timeout=8)
        except subprocess.TimeoutExpired:
            if process.poll() is not None:
                raise ValueError("Desktop exited during startup observation")
        else:
            raise ValueError(f"Desktop exited before startup observation completed ({process.returncode}): {output[-4000:]}")
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                output, _ = process.communicate(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                output, _ = process.communicate(timeout=5)
        else:
            output, _ = process.communicate()
        (work / "gui-startup.log").write_text(output or "", encoding="utf-8")
    return {"observedSeconds": round(time.monotonic() - started, 2),
            "processStayedRunning": True, "scope": "Xvfb process startup only; no interactive GUI journey claimed"}


def test(packages: Path, architecture: str, install: bool, gui: bool,
         manifest_path: Path = DEFAULT_MANIFEST, version: str | None = None) -> dict:
    release = load_release(manifest_path, version)
    if gui and not install:
        raise ValueError("--gui-smoke requires --install")
    require_native_ubuntu(architecture)
    packages = packages.resolve(strict=True)
    manifest = json.loads((packages / f"packaging-{architecture}.json").read_text(encoding="utf-8"))
    if (manifest["releaseVersion"] != release["version"]
            or manifest["debianVersion"] != release["debianVersion"]
            or manifest["sourceRevision"] != release["applicationSourceCommit"]
            or manifest["sourceDateEpoch"] != release["sourceDateEpoch"]
            or manifest["sourceManifestSha256"] != release["manifestSha256"]
            or manifest["architecture"] != architecture):
        raise ValueError("Unexpected package report identity")
    records = {p["package"]: p for p in manifest["packages"]}
    if set(records) != {"ledgesync", "ledgesync-cli"} or len(manifest["packages"]) != 2:
        raise ValueError("Expected exactly desktop and CLI packages")
    for name, record in records.items():
        if (record["file"] != f"{name}_{release['debianVersion']}_{architecture}.deb"
                or record["version"] != release["debianVersion"]):
            raise ValueError("Unexpected package filename")
    if records["ledgesync"]["recommends"] != "gnome-keyring":
        raise ValueError("Desktop package must recommend its native Secret Service implementation")
    for kind, entry in release["targets"]["linux-" + architecture].items():
        if manifest["inputArchives"][kind] != {"name": entry["name"], "sha256": entry["sha256"]}:
            raise ValueError("Package report input archive differs from pinned source release")
    report = {"architecture": architecture, "version": release["debianVersion"],
              "payloadVerified": False, "installed": False, "removed": False,
              "guiStartup": None, "packages": {name: record["sha256"] for name, record in records.items()}}
    report_path = packages / f"deb-install-test-{architecture}.json"
    log_path = packages / f"deb-gui-startup-{architecture}.log"
    if os.path.lexists(report_path) or os.path.lexists(log_path):
        raise ValueError("Refusing to replace previous Debian smoke evidence")
    with tempfile.TemporaryDirectory(prefix="ledgesync-deb-smoke-") as temporary:
        work = Path(temporary)
        try:
            for record in records.values():
                validate_package(packages / record["file"], record, architecture, work)
            report["payloadVerified"] = True
            if install:
                require_disposable_runner()
                installed = False
                try:
                    # First prove that the CLI can install by itself. The desktop
                    # then resolves the same-version CLI and native library Depends.
                    installed = True
                    command(["sudo", "-n", "apt-get", "install", "-y", "--no-install-recommends", str(packages / records["ledgesync-cli"]["file"])], timeout=300)
                    if command(["/usr/bin/ledgesync", "--version"]).stdout.strip() != "LedgeSync " + release["version"]:
                        raise ValueError("Installed CLI version mismatch")
                    command(["sudo", "-n", "apt-get", "install", "-y", "--no-install-recommends", str(packages / records["ledgesync"]["file"])], timeout=300)
                    report["installed"] = True
                    for name, record in records.items():
                        binary = Path("/") / record["binaryPath"]
                        if sha256(binary) != record["binarySha256"]:
                            raise ValueError("apt installation changed application bytes")
                        verify_notices(Path("/usr/share/doc") / name)
                    linked = command(["ldd", "/usr/bin/ledgesync-desktop"]).stdout
                    if "not found" in linked:
                        raise ValueError("Installed desktop has unresolved runtime dependencies")
                    command(["desktop-file-validate", "/usr/share/applications/com.ledgesync.app.desktop"])
                    if gui:
                        report["guiStartup"] = gui_startup(work)
                finally:
                    if installed:
                        present = []
                        for name in ("ledgesync", "ledgesync-cli"):
                            state = command(["dpkg-query", "-W", "-f=${db:Status-Status}", name], check=False)
                            if state.returncode == 0 and state.stdout.strip() != "not-installed":
                                present.append(name)
                        if present:
                            command(["sudo", "-n", "apt-get", "purge", "-y", *present], timeout=180)
                        for path in ("/usr/bin/ledgesync", "/usr/bin/ledgesync-desktop", "/usr/share/applications/com.ledgesync.app.desktop", "/usr/share/icons/hicolor/scalable/apps/ledgesync.svg", "/usr/share/doc/ledgesync", "/usr/share/doc/ledgesync-cli"):
                            if os.path.lexists(path):
                                raise ValueError(f"Package removal left an owned path: {path}")
                        report["removed"] = True
            report["status"] = "passed"
        except BaseException as exc:
            report["status"] = "failed"
            report["error"] = str(exc)
            raise
        finally:
            if (work / "gui-startup.log").is_file():
                with log_path.open("xb") as destination:
                    destination.write((work / "gui-startup.log").read_bytes())
                report["guiLog"] = log_path.name
            with report_path.open("x", encoding="utf-8") as destination:
                destination.write(json.dumps(report, indent=2, sort_keys=True) + "\n")
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--packages", type=Path, required=True)
    parser.add_argument("--arch", choices=("amd64", "arm64"), required=True)
    parser.add_argument("--install", action="store_true")
    parser.add_argument("--gui-smoke", action="store_true")
    parser.add_argument("--manifest", type=Path, default=DEFAULT_MANIFEST)
    parser.add_argument("--version", help="Require the pinned release version to match")
    args = parser.parse_args()
    try:
        report = test(args.packages, args.arch, args.install, args.gui_smoke, args.manifest, args.version)
    except (ValueError, OSError, KeyError, subprocess.SubprocessError, tarfile.TarError) as exc:
        print(f"Debian smoke test failed: {exc}", file=sys.stderr)
        return 1
    print(json.dumps(report, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
