#!/usr/bin/env python3
"""Build a signed LedgeSync APT snapshot using an existing server-side key.

This never installs packages, exports private keys, or activates the snapshot.
An operator can atomically point the production public symlink at the result.
"""
import argparse
import gzip
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
ARCHES = ('amd64', 'arm64')
PACKAGES = ('ledgesync', 'ledgesync-cli')


def run(arguments, cwd=None):
    return subprocess.check_output(arguments, cwd=cwd, timeout=120)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def validate_packages(packages, expected_version):
    if (not isinstance(expected_version, str) or len(expected_version) > 80
            or not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+(?:~[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?-[1-9][0-9]*', expected_version)):
        raise ValueError('Use an explicit LedgeSync Debian package version, for example 0.1.0~alpha.2-1')
    inputs = sorted(packages.glob('*.deb'))
    identities = set()
    for package in inputs:
        if package.is_symlink() or not package.is_file() or package.stat().st_size > 512 * 1024 * 1024:
            raise ValueError('APT input packages must be bounded regular files')
        fields = run(['dpkg-deb', '-f', str(package), 'Package', 'Version', 'Architecture']).decode()
        control = dict(line.split(': ', 1) for line in fields.splitlines())
        name, version, arch = (control[field] for field in ('Package', 'Version', 'Architecture'))
        if name not in PACKAGES or version != expected_version or arch not in ARCHES:
            raise ValueError(f'Unexpected package metadata: {package.name}')
        if package.name != f'{name}_{version}_{arch}.deb' or (name, arch) in identities:
            raise ValueError('Unexpected or duplicate package filename')
        identities.add((name, arch))
    if identities != {(name, arch) for name in PACKAGES for arch in ARCHES}:
        raise ValueError('Both desktop and CLI packages are required for both architectures')
    return inputs


def build(packages, destination, gnupghome, key, previous=None, *, version):
    if not re.fullmatch(r'[A-Fa-f0-9]{40}', key):
        raise ValueError('Use the full fingerprint of the existing signing key')
    if destination.exists() or destination.is_symlink():
        raise ValueError('Refusing to replace an APT snapshot')
    inputs = validate_packages(packages, version)
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.apt-snapshot-', dir=destination.parent) as temporary:
        stage = Path(temporary) / 'public'
        if previous:
            shutil.copytree(previous, stage, symlinks=False)
        else:
            stage.mkdir()
        pool = stage / 'pool/main/l/ledgesync'
        pool.mkdir(parents=True, exist_ok=True)
        for package in inputs:
            target = pool / package.name
            if target.exists():
                if digest(target) != digest(package):
                    raise ValueError(f'Existing package identity would change: {target.name}')
            else:
                shutil.copy2(package, target)
        release_dir = stage / 'dists/stable'
        current_indexes = []
        for arch in ARCHES:
            index_dir = release_dir / f'main/binary-{arch}'
            index_dir.mkdir(parents=True, exist_ok=True)
            raw = run(['apt-ftparchive', '--arch', arch, 'packages', 'pool'], cwd=stage)
            paragraphs = [block for block in raw.decode().strip().split('\n\n') if block]
            if len(paragraphs) < 2:
                raise ValueError('APT index is missing package entries')
            for block in paragraphs:
                fields = dict(line.split(': ', 1) for line in block.splitlines() if ': ' in line and not line.startswith(' '))
                if fields.get('Architecture') != arch or fields.get('Package') not in PACKAGES:
                    raise ValueError('APT architecture index contains an unexpected package')
            index = index_dir / 'Packages'
            index.write_bytes(raw)
            compressed = index_dir / 'Packages.gz'
            compressed.write_bytes(gzip.compress(raw, mtime=0))
            current_indexes.extend((index, compressed))
        # Retain by-hash files from prior snapshots so in-flight clients can
        # complete an update even when a newer signed Release has been published.
        for index in current_indexes:
            for algorithm in ('sha256', 'sha512'):
                index_hash = hashlib.new(algorithm, index.read_bytes()).hexdigest()
                hashed = index.parent / 'by-hash' / algorithm.upper() / index_hash
                hashed.parent.mkdir(parents=True, exist_ok=True)
                if not hashed.exists():
                    shutil.copyfile(index, hashed)
        options = {
            'Origin': 'LedgeSync', 'Label': 'LedgeSync', 'Suite': 'stable',
            'Codename': 'stable', 'Architectures': 'amd64 arm64',
            'Components': 'main', 'Description': 'LedgeSync for Ubuntu 24.04 LTS',
            'Acquire-By-Hash': 'yes',
        }
        arguments = ['apt-ftparchive']
        for name, value in options.items():
            arguments += ['-o', f'APT::FTPArchive::Release::{name}={value}']
        release = release_dir / 'Release'
        # Avoid indexing previous signed metadata while creating a new snapshot.
        for filename in ('Release', 'InRelease', 'Release.gpg'):
            (release_dir / filename).unlink(missing_ok=True)
        release.write_bytes(run(arguments + ['release', 'dists/stable'], cwd=stage))
        gpg = ['gpg', '--homedir', str(gnupghome), '--batch', '--yes', '--local-user', key, '--digest-algo', 'SHA256']
        run(gpg + ['--clearsign', '--output', str(release_dir / 'InRelease'), str(release)])
        run(gpg + ['--armor', '--detach-sign', '--output', str(release_dir / 'Release.gpg'), str(release)])
        keyring = stage / 'ledgesync-archive-keyring.gpg'
        keyring.write_bytes(run(['gpg', '--homedir', str(gnupghome), '--batch', '--export-options', 'export-minimal', '--export', key]))
        if not keyring.stat().st_size:
            raise ValueError('Public key export is empty')
        run(['gpgv', '--keyring', str(keyring), str(release_dir / 'InRelease')])
        shutil.copyfile(ROOT / 'deploy/apt/ledgesync.sources', stage / 'ledgesync.sources')
        (stage / 'signing-key-fingerprint.txt').write_text(key.upper() + '\n')
        evidence = {'version': version, 'packages': {p.name: digest(p) for p in inputs}, 'signingFingerprint': key.upper()}
        (stage / 'repository.json').write_text(json.dumps(evidence, indent=2) + '\n')
        for path in stage.rglob('*'):
            path.chmod(0o755 if path.is_dir() else 0o644)
        stage.chmod(0o755)
        stage.rename(destination)
    print(destination)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--packages', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--gnupghome', type=Path, required=True)
    parser.add_argument('--key', required=True)
    parser.add_argument('--previous', type=Path)
    parser.add_argument('--version', required=True, help='Exact Debian package version, e.g. 0.1.0~alpha.2-1')
    args = parser.parse_args()
    build(args.packages.resolve(), args.output.resolve(), args.gnupghome.resolve(), args.key, args.previous, version=args.version)


if __name__ == '__main__':
    main()
