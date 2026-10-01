#!/usr/bin/env python3
"""Build a development-only local lsf oracle from the audited rclone checkout.

Example:
  python3 tools/build_rclone_reference.py --source /outside/project/rclone
  LEDGESYNC_RCLONE_REFERENCE="$PWD/build/reference/rclone-local-reference" go test ./internal/filters

This does not download/modify the source checkout, use cloud credentials or add
rclone to LedgeSync's production module. Only the local backend and lsf command
are registered. The output is a reference test tool, never a product artifact.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time


ROOT = Path(__file__).resolve().parents[1]
TAG = "v1.75.1"
SHA = "687d264b689b8c49a67e2e52a8a5e0caa01c04ce"
MAIN = '''package main

import (
    _ "github.com/rclone/rclone/backend/local"
    "github.com/rclone/rclone/cmd"
    _ "github.com/rclone/rclone/cmd/lsf"
)

func main() { cmd.Main() }
'''


def capture(command, cwd, env=None):
    result = subprocess.run(command, cwd=cwd, env=env, capture_output=True, text=True, check=False, timeout=60)
    if result.returncode:
        raise RuntimeError(f"{command[0]} failed: {result.stderr.strip()}")
    return result.stdout.strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, required=True, help="Clean official rclone checkout outside LedgeSync, at the exact audited commit")
    parser.add_argument("--output", type=Path, default=ROOT / "build/reference", help="New/empty development artifact directory")
    args = parser.parse_args()
    source = args.source.resolve(strict=True)
    if source == ROOT or source.is_relative_to(ROOT):
        parser.error("reference source must be outside the LedgeSync project tree")
    if capture(["git", "rev-parse", "HEAD"], source) != SHA:
        parser.error("reference checkout does not match the audited full commit")
    if capture(["git", "status", "--porcelain"], source):
        parser.error("reference checkout is not clean; preserve the changes and use another checkout")
    if capture(["git", "remote", "get-url", "origin"], source) != "https://github.com/rclone/rclone.git":
        parser.error("reference origin is not the audited official repository")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    if any(output.iterdir()):
        parser.error("output directory must be empty; existing artifacts are never overwritten")
    go = shutil.which("go")
    if not go:
        parser.error("Go is unavailable")
    module_cache = capture([go, "env", "GOMODCACHE"], ROOT)
    start = time.monotonic()
    with tempfile.TemporaryDirectory(prefix="ledgesync-rclone-reference-") as temporary:
        sandbox = Path(temporary)
        for name in ("home", "config", "cache", "tmp", "driver", "fixtures"):
            (sandbox / name).mkdir()
        empty_config = sandbox / "empty-rclone.conf"
        empty_config.write_text("", encoding="utf-8")
        env = {"PATH": os.path.dirname(go) + os.pathsep + os.defpath, "HOME": str(sandbox / "home"), "XDG_CONFIG_HOME": str(sandbox / "config"), "XDG_CACHE_HOME": str(sandbox / "cache"), "TMPDIR": str(sandbox / "tmp"), "GOMODCACHE": module_cache, "GOCACHE": str(sandbox / "cache/go-build"), "GOTOOLCHAIN": "go1.27.1", "GOENV": "off", "GOWORK": "off", "CGO_ENABLED": "0", "GOPROXY": "https://proxy.golang.org", "GOSUMDB": "sum.golang.org", "RCLONE_CONFIG": str(empty_config)}
        if os.name == "nt":
            env["SystemRoot"] = os.environ["SystemRoot"]
        driver = sandbox / "driver"
        (driver / "main.go").write_text(MAIN, encoding="utf-8")
        (driver / "go.mod").write_text(f"module ledgesync.local/rclone-reference\n\ngo 1.27.1\n\nrequire github.com/rclone/rclone {TAG}\n", encoding="utf-8")
        capture([go, "mod", "edit", f"-replace=github.com/rclone/rclone={source}"], driver, env)
        binary = output / ("rclone-local-reference.exe" if os.name == "nt" else "rclone-local-reference")
        build = [go, "build", "-mod=mod", "-trimpath", "-o", str(binary), "."]
        with (output / "build.log").open("w", encoding="utf-8") as log:
            result = subprocess.run(build, cwd=driver, env=env, stdout=log, stderr=subprocess.STDOUT, check=False, timeout=300)
        if result.returncode:
            raise RuntimeError("Reference build failed; inspect the local build.log")
        (sandbox / "fixtures/keep.txt").write_text("synthetic fixture\n", encoding="utf-8")
        (sandbox / "fixtures/skip.log").write_text("synthetic fixture\n", encoding="utf-8")
        smoke = [str(binary), "lsf", "--files-only", "--recursive", "--exclude", "*.log", str(sandbox / "fixtures")]
        listed = capture(smoke, sandbox, env)
        if listed != "keep.txt":
            raise RuntimeError("Reference local-only smoke selection differed")
        evidence = {"source_repository": "https://github.com/rclone/rclone.git", "source_tag": TAG, "source_commit": SHA, "source_clean": True, "go_version": capture([go, "version"], driver, env), "driver_sha256": hashlib.sha256(MAIN.encode()).hexdigest(), "registered_backend": "local", "registered_command": "lsf", "build_command": ["go", "build", "-mod=mod", "-trimpath", "-o", "${OUTPUT}/rclone-local-reference", "."], "build_exit": result.returncode, "smoke_command": ["rclone-local-reference", "lsf", "--files-only", "--recursive", "--exclude", "*.log", "${FIXTURE}"], "smoke_exit": 0, "smoke_stdout": listed + "\n", "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(), "go_sum_sha256": hashlib.sha256((driver / "go.sum").read_bytes()).hexdigest(), "wall_duration_seconds": round(time.monotonic() - start, 3), "isolation": "Fresh HOME/XDG/TMPDIR, empty explicit RCLONE_CONFIG, allowlisted environment; temporary synthetic local fixtures only; no cloud credentials", "distribution": "Development reference only; never package with LedgeSync"}
        (output / "BUILD_EVIDENCE.json").write_text(json.dumps(evidence, indent=2) + "\n", encoding="utf-8")
    print(str(binary))


if __name__ == "__main__":
    main()
