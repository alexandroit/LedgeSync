#!/usr/bin/env python3
"""Collect notices for the actual cross-platform CLI/desktop Go dependency graph.

Run after go mod tidy and frontend compilation. No project code is executed.
The manifest records package loading, not proof of native cross-compilation.
"""

import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile


ROOT = Path(__file__).resolve().parents[1]
TARGETS = [(system, arch) for system in ("darwin", "linux", "windows") for arch in ("amd64", "arm64")]
NOTICE = re.compile(r"^(?:licen[sc]e|copying|notice|copyright|authors|patents)(?:[._-].*)?$", re.I)


def run(args, env=None):
    result = subprocess.run(args, cwd=ROOT, env=env, text=True, capture_output=True, check=False, timeout=180)
    if result.returncode:
        raise RuntimeError(f"{' '.join(args[:5])} failed: {result.stderr.strip()}")
    return result.stdout


def objects(stream):
    decoder = json.JSONDecoder()
    cursor = 0
    while cursor < len(stream):
        while cursor < len(stream) and stream[cursor].isspace():
            cursor += 1
        if cursor == len(stream):
            break
        value, cursor = decoder.raw_decode(stream, cursor)
        yield value


def notices(module_root, package_directories):
    directories = {module_root}
    for package in package_directories:
        parent = package
        while parent.is_relative_to(module_root):
            directories.add(parent)
            if parent == module_root:
                break
            parent = parent.parent
    found = set()
    for directory in directories:
        for candidate in directory.iterdir():
            if candidate.is_file() and NOTICE.match(candidate.name):
                found.add(candidate)
            elif candidate.is_dir() and candidate.name.lower() in ("license", "licenses", "licences"):
                found.update(file for file in candidate.rglob("*") if file.is_file())
    return sorted(found)


def main():
    base_env = {**os.environ, "GOTOOLCHAIN": "go1.27.1"}
    goroot = Path(run(["go", "env", "GOROOT"], base_env).strip())
    standard_directories = set()
    modules = {}
    target_records = []
    for system, arch in TARGETS:
        target = f"{system}/{arch}"
        env = {**base_env, "GOOS": system, "GOARCH": arch, "CGO_ENABLED": "1"}
        command = ["go", "list", "-mod=readonly", "-deps", "-json", "-tags", "desktop,production,webkit2_41", "./cmd/ledgesync", "./cmd/ledgesync-desktop"]
        packages = list(objects(run(command, env)))
        target_records.append({"target": target, "packages": len(packages), "cgo_enabled": True})
        for package in packages:
            if package.get("Error") or package.get("DepsErrors"):
                raise RuntimeError(f"Unresolved package in {target}: {package['ImportPath']}")
            module = package.get("Module")
            if package.get("Standard") and package.get("Dir"):
                directory = Path(package["Dir"])
                if not directory.is_relative_to(goroot):
                    raise RuntimeError("Standard-library package is outside the pinned toolchain")
                standard_directories.add(directory)
            if not module or module.get("Main"):
                continue
            if module.get("Replace"):
                raise RuntimeError(f"Review replacement licensing before collecting: {module['Path']}")
            identity = (module["Path"], module["Version"])
            record = modules.setdefault(identity, {"directory": Path(module["Dir"]), "packages": set(), "directories": set(), "targets": set()})
            record["packages"].add(package["ImportPath"])
            record["directories"].add(Path(package["Dir"]))
            record["targets"].add(target)
        print(f"Loaded notices graph: {target}", flush=True)

    output = ROOT / "third_party"
    output.mkdir(exist_ok=True)
    manifest = {"schema_version": 1, "purpose": "Go runtime dependency notices for the union of listed CLI and desktop target imports", "go_version": run(["go", "version"], base_env).strip(), "build_tags": ["desktop", "production", "webkit2_41"], "targets": target_records, "modules": []}
    with tempfile.TemporaryDirectory(prefix="ledgesync-license-collection-") as temporary:
        stage = Path(temporary)
        for (name, version), record in sorted(modules.items()):
            module_root = record["directory"]
            files = notices(module_root, record["directories"])
            if not files or not any(re.match(r"^(license|licence|copying)", p.name, re.I) for p in files):
                raise RuntimeError(f"Missing applicable license text for {name}@{version}")
            slug = re.sub(r"[^A-Za-z0-9._-]", "_", name + "@" + version)
            entry = {"module": name, "version": version, "packages": sorted(record["packages"]), "targets": sorted(record["targets"]), "notices": []}
            for file in files:
                relative = file.relative_to(module_root)
                destination = stage / slug / relative
                destination.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(file, destination)
                entry["notices"].append({"upstream_path": relative.as_posix(), "file": f"third_party/go_modules/{slug}/{relative.as_posix()}", "sha256": hashlib.sha256(file.read_bytes()).hexdigest()})
            manifest["modules"].append(entry)
        # Do not remove existing notices automatically; older retained versions
        # are harmless and can be removed deliberately when reviewing the diff.
        shutil.copytree(stage, output / "go_modules", dirs_exist_ok=True)

    shutil.copyfile(goroot / "LICENSE", output / "go.LICENSE")
    manifest["standard_library_license"] = {"file": "third_party/go.LICENSE", "sha256": hashlib.sha256((output / "go.LICENSE").read_bytes()).hexdigest()}
    manifest["standard_library_notices"] = []
    for file in notices(goroot, standard_directories):
        relative = file.relative_to(goroot)
        destination = output / "go_standard" / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(file, destination)
        manifest["standard_library_notices"].append({"upstream_path": relative.as_posix(), "file": f"third_party/go_standard/{relative.as_posix()}", "sha256": hashlib.sha256(file.read_bytes()).hexdigest()})
    (output / "GO_RUNTIME_LICENSES.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    frontend_manifest = {"schema_version": 1, "purpose": "Direct frontend build-tool notices, including Vite's bundled module-preload helper", "packages": []}
    declared = json.loads((ROOT / "frontend/package.json").read_text(encoding="utf-8"))["devDependencies"]
    for name in ("vite", "typescript"):
        package_root = ROOT / "frontend/node_modules" / name
        package = json.loads((package_root / "package.json").read_text(encoding="utf-8"))
        if package["version"] != declared[name]:
            raise RuntimeError(f"Installed {name} does not match its exact frontend pin")
        entry = {"package": name, "version": package["version"], "license": package["license"], "usage": "build tool and bundled module-preload helper" if name == "vite" else "build-time compiler; not packaged as a compiler", "notices": []}
        for file in notices(package_root, set()):
            destination = output / "frontend" / f"{name}-{package['version']}" / file.name
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(file, destination)
            entry["notices"].append({"upstream_path": file.name, "file": destination.relative_to(ROOT).as_posix(), "sha256": hashlib.sha256(file.read_bytes()).hexdigest()})
        if not entry["notices"]:
            raise RuntimeError(f"Missing frontend package license: {name}")
        frontend_manifest["packages"].append(entry)
    (output / "FRONTEND_LICENSES.json").write_text(json.dumps(frontend_manifest, indent=2) + "\n", encoding="utf-8")
    print(f"Collected {len(modules)} Go runtime module notice sets.")


if __name__ == "__main__":
    main()
