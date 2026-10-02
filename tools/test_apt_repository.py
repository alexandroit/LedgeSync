#!/usr/bin/env python3
"""Exercise signed APT install/remove only on disposable GitHub Ubuntu runners."""
import argparse
import functools
import http.server
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading

from build_apt_repository import build
from package_deb import debian_version

ROOT = Path(__file__).resolve().parents[1]


def run(args, check=True):
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=600)
    print(result.stdout, end='', flush=True)
    if check and result.returncode:
        raise RuntimeError(f'Command failed ({result.returncode}): {args[0]}')
    return result


def download(url, path):
    # Exercise the documented curl onboarding client. Cloudflare's browser
    # integrity check rejects Python urllib before requests reach this origin.
    # Keep curl and APT's native user agents; do not impersonate a browser.
    run(['curl', '--fail', '--show-error', '--silent', '--location',
         '--max-time', '60', '--max-filesize', str(1024 * 1024),
         '--output', str(path), url])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument('--packages', type=Path)
    mode.add_argument('--public', action='store_true')
    parser.add_argument('--version', required=True, help='Expected application release version')
    args = parser.parse_args()
    version = debian_version(args.version)
    release = Path('/etc/os-release').read_text() if Path('/etc/os-release').exists() else ''
    if os.environ.get('GITHUB_ACTIONS') != 'true' or os.environ.get('RUNNER_ENVIRONMENT') != 'github-hosted' or 'VERSION_ID="24.04"' not in release or 'ID=ubuntu' not in release:
        raise SystemExit('Refusing package lifecycle test outside disposable GitHub Ubuntu 24.04 runners')
    for package in ('ledgesync', 'ledgesync-cli'):
        if run(['dpkg-query', '-W', '-f=${db:Status-Status}', package], check=False).stdout.strip() == 'installed':
            raise SystemExit('Refusing to touch an existing LedgeSync installation')
    source_path = Path('/etc/apt/sources.list.d/ledgesync-test.sources')
    if source_path.exists():
        raise SystemExit('Refusing to replace an existing test source')
    sudo_apt = ['sudo', '-n', 'env', 'DEBIAN_FRONTEND=noninteractive', 'apt-get']
    server = None
    requests = []
    with tempfile.TemporaryDirectory(prefix='ledgesync-apt-test-') as temp:
        work = Path(temp)
        work.chmod(0o755)
        keyfile = work / 'archive.gpg'
        if args.public:
            base = 'https://ledgesync.com/apt'
            download(base + '/ledgesync-archive-keyring.gpg', keyfile)
            if keyfile.read_bytes() != (ROOT / 'deploy/apt/ledgesync-archive-keyring.gpg').read_bytes():
                raise ValueError('Public key differs from the pinned project key')
            public_source = work / 'published.sources'
            download(base + '/ledgesync.sources', public_source)
            if public_source.read_bytes() != (ROOT / 'deploy/apt/ledgesync.sources').read_bytes():
                raise ValueError('Public source definition differs from the pinned project source')
        else:
            gnupg = work / 'gnupg'
            gnupg.mkdir(mode=0o700)
            run(['gpg', '--homedir', str(gnupg), '--batch', '--pinentry-mode', 'loopback', '--passphrase', '', '--quick-generate-key', 'LedgeSync disposable CI archive', 'ed25519', 'sign', '1d'])
            keys = run(['gpg', '--homedir', str(gnupg), '--batch', '--with-colons', '--list-keys']).stdout
            fingerprint = next(line.split(':')[9] for line in keys.splitlines() if line.startswith('fpr:'))
            public = work / 'public'
            build(args.packages.resolve(), public, gnupg, fingerprint, version=version)
            keyfile.write_bytes((public / 'ledgesync-archive-keyring.gpg').read_bytes())

            class Handler(http.server.SimpleHTTPRequestHandler):
                def log_message(self, format, *values):
                    requests.append(self.path)

            server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), functools.partial(Handler, directory=str(public)))
            threading.Thread(target=server.serve_forever, daemon=True).start()
            base = f'http://127.0.0.1:{server.server_port}'
        keyfile.chmod(0o644)
        inrelease = work / 'InRelease'
        download(base + '/dists/stable/InRelease', inrelease)
        run(['gpgv', '--keyring', str(keyfile), str(inrelease)])
        modified = inrelease.read_bytes().replace(b'Origin: LedgeSync', b'Origin: alteredxx', 1)
        if modified == inrelease.read_bytes():
            raise ValueError('Expected Origin field missing from signed metadata')
        tampered = work / 'tampered-InRelease'
        tampered.write_bytes(modified)
        if run(['gpgv', '--keyring', str(keyfile), str(tampered)], check=False).returncode == 0:
            raise ValueError('Tampered repository metadata was accepted')
        source = work / 'ledgesync-test.sources'
        arch = run(['dpkg', '--print-architecture']).stdout.strip()
        source.write_text(f'Types: deb\nURIs: {base}\nSuites: stable\nComponents: main\nArchitectures: {arch}\nSigned-By: {keyfile}\n')
        fixture = Path.home() / '.config/ledgesync-installer-test/user-data.txt'
        fixture.parent.mkdir(parents=True, exist_ok=False)
        fixture.write_text('Synthetic user data must survive package removal.\n')
        try:
            run(['sudo', '-n', 'install', '-m', '0644', str(source), str(source_path)])
            run(sudo_apt + ['update', '-o', 'Acquire::By-Hash=force', '-o', 'APT::Update::Error-Mode=any'])
            policy = run(['apt-cache', 'policy', 'ledgesync']).stdout
            if f'Candidate: {version}' not in policy or base not in policy:
                raise ValueError('APT candidate is not from the expected repository')
            run(sudo_apt + ['install', '-y', 'ledgesync'])
            recommendations = run(['dpkg-query', '-W', '-f=${Recommends}', 'ledgesync']).stdout.strip()
            native_vault_recommended = 'gnome-keyring' in recommendations.split(', ')
            if native_vault_recommended and run(['dpkg-query', '-W', '-f=${db:Status-Status}', 'gnome-keyring']).stdout.strip() != 'installed':
                raise ValueError('Desktop recommendation did not install native Secret Service')
            for package in ('ledgesync', 'ledgesync-cli'):
                installed = run(['dpkg-query', '-W', '-f=${Version}', package]).stdout.strip()
                if installed != version:
                    raise ValueError('Unexpected installed version')
            if run(['ledgesync', '--version']).stdout.strip() != 'LedgeSync ' + args.version:
                raise ValueError('Installed CLI did not execute')
            if not os.access('/usr/bin/ledgesync-desktop', os.X_OK):
                raise ValueError('Desktop launcher was not installed')
            run(sudo_apt + ['remove', '-y', 'ledgesync', 'ledgesync-cli'])
            run(sudo_apt + ['install', '-y', '--no-install-recommends', 'ledgesync-cli'])
            if run(['dpkg-query', '-W', '-f=${db:Status-Status}', 'ledgesync'], check=False).stdout.strip() == 'installed':
                raise ValueError('Headless CLI unexpectedly installed the desktop package')
            run(['ledgesync', '--version'])
            run(sudo_apt + ['remove', '-y', 'ledgesync-cli'])
            if fixture.read_text() != 'Synthetic user data must survive package removal.\n':
                raise ValueError('Package removal changed synthetic user data')
            if not args.public and not any('/by-hash/SHA512/' in path or '/by-hash/SHA256/' in path for path in requests):
                raise ValueError('APT did not exercise immutable by-hash indexes')
            report = ROOT / 'build/apt-validation'
            report.mkdir(parents=True, exist_ok=True)
            (report / f'{"public" if args.public else "local"}-{arch}.json').write_text(json.dumps({'baseURL': base, 'architecture': arch, 'version': version, 'signatureVerified': True, 'tamperedMetadataRejected': True, 'byHashForced': True, 'desktopInstalled': True, 'secretServiceInstalled': native_vault_recommended, 'headlessCliInstalled': True, 'uninstallPreservedUserFixture': True}, indent=2) + '\n')
        finally:
            run(['sudo', '-n', 'rm', '-f', str(source_path)], check=False)
            fixture.unlink(missing_ok=True)
            fixture.parent.rmdir()
            if server:
                server.shutdown()
                server.server_close()


if __name__ == '__main__':
    main()
