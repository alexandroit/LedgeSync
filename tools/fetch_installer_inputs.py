#!/usr/bin/env python3
"""Fetch the immutable, reviewed alpha payloads used by native installers."""
import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import shutil
import stat
import tempfile
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--platform', choices=['linux', 'windows'], required=True)
    parser.add_argument('--arch', choices=['amd64', 'arm64'], required=True)
    parser.add_argument('--output', type=Path, default=ROOT / 'build/installer-inputs')
    args = parser.parse_args()
    target = f'{args.platform}-{args.arch}'
    manifest = json.loads((ROOT / 'deploy/installers/source-release.json').read_text())
    entries = manifest['targets'][target]
    args.output.mkdir(parents=True, exist_ok=True)
    destination = args.output / target
    if destination.exists() or destination.is_symlink():
        parser.error(f'Refusing to replace existing inputs: {destination}')
    with tempfile.TemporaryDirectory(prefix='.installer-inputs-', dir=args.output) as temp:
        stage = Path(temp) / target
        archives = stage / 'archives'
        archives.mkdir(parents=True)
        for kind, entry in entries.items():
            name = entry['name']
            if Path(name).name != name or not entry['url'].startswith(
                'https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.1/'
            ):
                raise ValueError('Unexpected release source')
            archive = archives / name
            with urllib.request.urlopen(entry['url'], timeout=60) as response, archive.open('xb') as output:
                total = 0
                while chunk := response.read(1024 * 1024):
                    total += len(chunk)
                    if total > entry['size']:
                        raise ValueError('Download exceeds the pinned asset size')
                    output.write(chunk)
            if archive.stat().st_size != entry['size'] or hashlib.sha256(archive.read_bytes()).hexdigest() != entry['sha256']:
                raise ValueError(f'Release checksum mismatch: {name}')
            if args.platform == 'windows':
                payload = stage / kind
                payload.mkdir()
                archive_root = name.removesuffix('.zip')
                with zipfile.ZipFile(archive) as source:
                    for member in source.infolist():
                        path = PurePosixPath(member.filename)
                        if path.is_absolute() or '..' in path.parts or '\\' in member.filename or ':' in member.filename or path.parts[0] != archive_root:
                            raise ValueError('Unsafe archive member')
                        if stat.S_ISLNK(member.external_attr >> 16):
                            raise ValueError('Unexpected Windows archive symlink')
                        relative = path.relative_to(archive_root)
                        if str(relative) == '.':
                            continue
                        result = payload.joinpath(*relative.parts)
                        if member.is_dir():
                            result.mkdir(parents=True, exist_ok=True)
                        else:
                            result.parent.mkdir(parents=True, exist_ok=True)
                            with source.open(member) as input_file, result.open('xb') as output_file:
                                shutil.copyfileobj(input_file, output_file)
            print(f'Verified {name}', flush=True)
        (stage / 'inputs.json').write_text(json.dumps(entries, indent=2) + '\n')
        stage.rename(destination)
    print(destination)


if __name__ == '__main__':
    main()
