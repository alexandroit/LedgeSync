#!/usr/bin/env python3
"""Assemble release assets from an official "Build and test" run.

Downloads the six native desktop artifacts of RUN_ID with the GitHub CLI,
verifies every sidecar checksum, rejects mixed versions, and writes a combined
SHA256SUMS plus RELEASE.json evidence (source commit, run, per-target signing
state and asset hashes). It never uploads or publishes anything.

    python3 tools/assemble_release.py --run-id RUN_ID --version 0.1.0-alpha.5 --output build/release
"""
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import shutil
import subprocess
import sys
from pathlib import Path

RUNNERS = {
    "macos-15": "darwin-arm64", "macos-15-intel": "darwin-amd64",
    "ubuntu-24.04": "linux-amd64", "ubuntu-24.04-arm": "linux-arm64",
    "windows-2022": "windows-amd64", "windows-11-arm": "windows-arm64",
}


def sha256(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for block in iter(lambda: f.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def gh(*args: str) -> str:
    return subprocess.run(["gh", *args], check=True, capture_output=True, text=True).stdout


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--run-id", required=True, type=int)
    parser.add_argument("--version", required=True)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--repo", default="alexandroit/LedgeSync")
    args = parser.parse_args()
    run = json.loads(gh("run", "view", str(args.run_id), "--repo", args.repo, "--json", "headSha,headBranch,event,conclusion,workflowName,url"))
    if run["workflowName"] != "Build and test" or run["conclusion"] != "success":
        sys.exit("The run must be a successful 'Build and test' run.")
    if run["headBranch"] != "main" or run["event"] not in ("push", "workflow_dispatch"):
        sys.exit("Release assets must come from an official main push or dispatch build.")
    output = args.output.resolve()
    if output.exists() and any(output.iterdir()):
        sys.exit("Refusing to reuse a non-empty output directory.")
    download = output / "artifacts"
    download.mkdir(parents=True)
    gh("run", "download", str(args.run_id), "--repo", args.repo, "--pattern", "ledgesync-desktop-*", "--dir", str(download))
    assets = output / "assets"
    assets.mkdir()
    targets = {}
    for runner, target in RUNNERS.items():
        folder = download / f"ledgesync-desktop-{runner}"
        if not folder.is_dir():
            sys.exit(f"Missing artifact for {runner}.")
        state = (folder / "SIGNING-STATE.txt").read_text().strip()
        files = sorted(p for p in folder.iterdir() if p.is_file() and p.name not in ("SHA256SUMS", "SIGNING-STATE.txt") and not p.name.startswith("macos-signing-"))
        for f in files:
            if args.version not in f.name:
                sys.exit(f"Unexpected version in {f.name}.")
            if f.suffix == ".sha256":
                continue
            sidecar = f.with_name(f.name + ".sha256")
            if sidecar.exists() and sidecar.read_text().split()[0] != sha256(f):
                sys.exit(f"Checksum sidecar mismatch for {f.name}.")
            shutil.copy2(f, assets / f.name)
            if sidecar.exists():
                shutil.copy2(sidecar, assets / sidecar.name)
        targets[target] = {"runner": runner, "signingState": state}
    entries = []
    for f in sorted(assets.iterdir()):
        if f.suffix != ".sha256":
            entries.append({"name": f.name, "sha256": sha256(f), "size": f.stat().st_size})
    (assets / "SHA256SUMS").write_text("".join(f"{e['sha256']}  {e['name']}\n" for e in entries))
    signed = all(t["signingState"] == "publisher-signed" for t in targets.values())
    release = {
        "product": "LedgeSync", "version": args.version,
        "sourceCommit": run["headSha"], "ciRunId": args.run_id, "ciRunUrl": run["url"],
        "assembledAt": dt.datetime.now(dt.timezone.utc).isoformat(),
        "trustedPublisherSignature": signed,
        "notarized": all(t["signingState"] == "publisher-signed" for k, t in targets.items() if k.startswith("darwin")),
        "targets": targets, "artifacts": entries,
        "limitations": [
            "Unsigned targets are developer builds; operating systems may warn or block them. Do not disable security protections.",
            "Live Google acceptance is recorded separately in docs/research/DRIVE_UPLOAD_ACCEPTANCE.md.",
        ],
    }
    (assets / "RELEASE.json").write_text(json.dumps(release, indent=2) + "\n")
    print(json.dumps({"assets": len(entries) + 2, "signed": signed, "sourceCommit": run["headSha"]}))
    return 0


if __name__ == "__main__":
    sys.exit(main())
