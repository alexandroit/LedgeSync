#!/usr/bin/env python3
"""Verify required distribution notices and hashes in the recorded inventories.

This checks the recorded files, not whether the inventories cover a current
dependency graph. Regenerate and review inventories after dependency changes.
"""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import sys


ROOT = Path(__file__).resolve().parents[1]
REQUIRED = (
    "LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md",
    "third_party/rclone.LICENSE", "third_party/wails.LICENSE", "third_party/go.LICENSE",
    "third_party/GO_RUNTIME_LICENSES.json", "third_party/FRONTEND_LICENSES.json",
)


def _required_file(root: Path, relative: str) -> Path:
    file = root / relative
    # Keep archives self-contained; do not accept notice symlinks, even if they
    # currently resolve inside the checkout.
    current = root
    for part in PurePosixPath(relative).parts:
        current /= part
        if current.is_symlink():
            raise ValueError(f"Notice path contains a symlink: {relative}")
    if not file.is_file() or file.stat().st_size == 0:
        raise ValueError(f"Required notice file is missing or empty: {relative}")
    return file


def _unique_object(pairs: list[tuple[str, object]]) -> dict:
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError(f"Duplicate manifest key: {key}")
        value[key] = item
    return value


def _nonempty_list(value: object, label: str) -> list:
    if not isinstance(value, list) or not value:
        raise ValueError(f"Expected a nonempty list: {label}")
    return value


def _records(value: object):
    if isinstance(value, dict):
        if "file" in value or "sha256" in value:
            if "file" not in value or "sha256" not in value:
                raise ValueError("Notice record requires both file and sha256")
            yield value
        for item in value.values():
            yield from _records(item)
    elif isinstance(value, list):
        for item in value:
            yield from _records(item)


def verify_notices(root: Path) -> int:
    """Return the verified hash-record count; raise ValueError/OSError on failure."""
    root = root.resolve(strict=True)
    for relative in REQUIRED:
        _required_file(root, relative)
    count = 0
    for name, collection in (("GO_RUNTIME_LICENSES.json", "modules"), ("FRONTEND_LICENSES.json", "packages")):
        manifest = root / "third_party" / name
        data = json.loads(manifest.read_text(encoding="utf-8"), object_pairs_hook=_unique_object)
        if not isinstance(data, dict) or type(data.get("schema_version")) is not int or data["schema_version"] != 1:
            raise ValueError(f"Unsupported notice manifest schema: {name}")
        for entry in _nonempty_list(data.get(collection), f"{name}:{collection}"):
            if not isinstance(entry, dict):
                raise ValueError(f"Invalid notice collection entry: {name}")
            for record in _nonempty_list(entry.get("notices"), f"{name}:notices"):
                if not isinstance(record, dict) or "file" not in record or "sha256" not in record:
                    raise ValueError(f"Invalid notice record: {name}")
        if collection == "modules":
            standard = data.get("standard_library_license")
            if not isinstance(standard, dict) or "file" not in standard or "sha256" not in standard:
                raise ValueError("Missing standard-library license record")
            for record in _nonempty_list(data.get("standard_library_notices"), "standard_library_notices"):
                if not isinstance(record, dict) or "file" not in record or "sha256" not in record:
                    raise ValueError("Invalid standard-library notice record")
        for record in _records(data):
            relative, digest = record["file"], record["sha256"]
            if not isinstance(relative, str) or not relative.startswith("third_party/") or "\\" in relative or ":" in relative or any(ord(c) < 32 or ord(c) == 127 for c in relative):
                raise ValueError(f"Unsafe notice path in {name}: {relative!r}")
            if any(part in ("", ".", "..") for part in relative.split("/")):
                raise ValueError(f"Unsafe notice path in {name}: {relative!r}")
            if not isinstance(digest, str) or not re.fullmatch(r"[0-9a-fA-F]{64}", digest):
                raise ValueError(f"Invalid SHA-256 for notice: {relative}")
            file = _required_file(root, relative)
            if not file.resolve(strict=True).is_relative_to(root / "third_party"):
                raise ValueError(f"Notice path escapes third_party: {relative}")
            if hashlib.sha256(file.read_bytes()).hexdigest() != digest.lower():
                raise ValueError(f"Notice hash mismatch: {relative}")
            count += 1
    return count


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT, help="Repository root (defaults to this checkout).")
    args = parser.parse_args()
    try:
        count = verify_notices(args.root)
    except (ValueError, OSError, UnicodeError) as exc:
        print(f"Notice verification failed: {exc}", file=sys.stderr)
        return 1
    print(f"Verified required distribution notices and {count} recorded file hashes.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
