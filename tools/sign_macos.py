#!/usr/bin/env python3
"""Sign, notarize, staple and verify LedgeSync macOS artifacts.

Requires a "Developer ID Application" identity in a keychain and notarization
credentials. Nothing secret is read from arguments or printed:

  Identity:  --identity "Developer ID Application: NAME (TEAMID)"
             or LEDGESYNC_MACOS_IDENTITY
  Notary:    --keychain-profile PROFILE (created once with
             `xcrun notarytool store-credentials`), or, in CI, the App Store
             Connect API key environment: LEDGESYNC_NOTARY_KEY_FILE,
             LEDGESYNC_NOTARY_KEY_ID, LEDGESYNC_NOTARY_ISSUER.

Order: sign the app inside-out with the hardened runtime and a secure
timestamp, notarize a zip of the app and staple the app; build the DMG from the
stapled app, sign the DMG, notarize and staple it. Standalone CLI binaries are
signed and notarized (Apple cannot staple a bare Mach-O; Gatekeeper checks its
ticket online). Every step is verified, including `spctl` assessment.

  python3 tools/sign_macos.py --app build/bin/LedgeSync.app --dmg-arch arm64 \\
      --version 0.1.0-alpha.6 --output build/packages [--cli path/to/ledgesync]
  python3 tools/sign_macos.py --check   # report whether signing can run here
"""
from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ENTITLEMENTS = ROOT / 'deploy/macos/LedgeSync.entitlements'


class SigningError(RuntimeError):
    pass


def run(*args: str, capture: bool = False) -> str:
    result = subprocess.run(list(args), capture_output=True, text=True)
    if result.returncode != 0:
        # Tool diagnostics never contain credentials, but keep them short.
        detail = (result.stderr or result.stdout).strip().splitlines()[-5:]
        raise SigningError(f'{Path(args[0]).name} {args[1] if len(args) > 1 else ""} failed: ' + ' | '.join(detail))
    return result.stdout + result.stderr if capture else ''


def developer_id(identity: str | None) -> str:
    identity = identity or os.environ.get('LEDGESYNC_MACOS_IDENTITY', '')
    listing = subprocess.run(['security', 'find-identity', '-v', '-p', 'codesigning'], capture_output=True, text=True).stdout
    available = [line.split('"')[1] for line in listing.splitlines() if '"Developer ID Application:' in line]
    if identity and identity in available:
        return identity
    if not identity and len(available) == 1:
        return available[0]
    raise SigningError('No usable "Developer ID Application" identity is in the keychain. '
                       'An "Apple Development" certificate cannot be notarized for distribution.')


def notary_args(profile: str | None) -> list[str]:
    if profile:
        return ['--keychain-profile', profile]
    key = os.environ.get('LEDGESYNC_NOTARY_KEY_FILE')
    key_id = os.environ.get('LEDGESYNC_NOTARY_KEY_ID')
    issuer = os.environ.get('LEDGESYNC_NOTARY_ISSUER')
    if key and key_id and issuer and Path(key).is_file():
        return ['--key', key, '--key-id', key_id, '--issuer', issuer]
    raise SigningError('No notarization credentials: use --keychain-profile or the LEDGESYNC_NOTARY_* API key environment.')


def sign(path: Path, identity: str, entitlements: bool) -> None:
    command = ['codesign', '--force', '--timestamp', '--options', 'runtime', '--sign', identity]
    if entitlements:
        command += ['--entitlements', str(ENTITLEMENTS)]
    run(*command, str(path))


def sign_app(app: Path, identity: str) -> None:
    # Inside-out: nested code first, then the bundle. Wails bundles contain a
    # single executable today; any future helper or framework is covered.
    nested = [p for p in (app / 'Contents').rglob('*') if p.is_file() and not p.is_symlink() and p.suffix in ('.dylib', '.so') or p.suffix in ('.framework', '.app', '.xpc') and p.is_dir()]
    for item in sorted(nested, key=lambda p: len(p.parts), reverse=True):
        sign(item, identity, entitlements=False)
    sign(app, identity, entitlements=True)
    run('codesign', '--verify', '--deep', '--strict', '--verbose=2', str(app))
    details = run('codesign', '-dvv', str(app), capture=True)
    if 'flags=0x10000(runtime)' not in details or 'Authority=Developer ID Application' not in details or 'Timestamp=' not in details:
        raise SigningError('The app signature lacks the hardened runtime, Developer ID authority or secure timestamp.')


def notarize(path: Path, credentials: list[str]) -> dict:
    output = run('xcrun', 'notarytool', 'submit', str(path), *credentials, '--wait', '--output-format', 'json', capture=True)
    start = output.find('{')
    result = json.loads(output[start:output.rfind('}') + 1]) if start >= 0 else {}
    if result.get('status') != 'Accepted':
        submission = result.get('id', '')
        log = ''
        if submission:
            log = subprocess.run(['xcrun', 'notarytool', 'log', submission, *credentials], capture_output=True, text=True).stdout[-2000:]
        raise SigningError(f'Notarization was not accepted ({result.get("status", "unknown")}). {log}')
    return {'id': result.get('id'), 'status': 'Accepted'}


def assess(path: Path, kind: str) -> str:
    args = ['spctl', '--assess', '-vvv', '--type', kind]
    if kind == 'open':
        args += ['--context', 'context:primary-signature']
    return run(*args, str(path), capture=True).strip()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--app', type=Path)
    parser.add_argument('--dmg-arch', choices=['arm64', 'amd64'])
    parser.add_argument('--version')
    parser.add_argument('--output', type=Path, default=ROOT / 'build/packages')
    parser.add_argument('--cli', type=Path, action='append', default=[])
    parser.add_argument('--identity')
    parser.add_argument('--keychain-profile')
    parser.add_argument('--report', type=Path)
    parser.add_argument('--check', action='store_true', help='only report whether signing prerequisites exist')
    parser.add_argument('--cli-only', action='store_true', help='sign and notarize only the --cli binaries')
    args = parser.parse_args()
    report: dict = {'tool': 'sign_macos', 'artifacts': []}
    try:
        identity = developer_id(args.identity)
        credentials = notary_args(args.keychain_profile)
        if args.check:
            print(json.dumps({'ready': True, 'identity': identity.split(':', 1)[0]}))
            return 0
        if args.cli_only:
            for cli in args.cli:
                cli = cli.resolve()
                sign(cli, identity, entitlements=False)
                run('codesign', '--verify', '--strict', '--verbose=2', str(cli))
                with tempfile.TemporaryDirectory(prefix='ledgesync-notary-') as temp:
                    archive = Path(temp) / (cli.name + '.zip')
                    run('ditto', '-c', '-k', '--keepParent', str(cli), str(archive))
                    report['artifacts'].append({'name': cli.name, 'notarization': notarize(archive, credentials), 'stapled': False})
            report['result'] = 'signed'
            print(json.dumps(report, indent=2))
            return 0
        if not (args.app and args.dmg_arch and args.version):
            parser.error('--app, --dmg-arch and --version are required')
        app = args.app.resolve()
        sign_app(app, identity)
        with tempfile.TemporaryDirectory(prefix='ledgesync-notary-') as temp:
            archive = Path(temp) / 'LedgeSync.zip'
            run('ditto', '-c', '-k', '--keepParent', str(app), str(archive))
            report['artifacts'].append({'name': 'LedgeSync.app', 'notarization': notarize(archive, credentials)})
        run('xcrun', 'stapler', 'staple', str(app))
        run('xcrun', 'stapler', 'validate', str(app))
        report['artifacts'][-1]['assessment'] = assess(app, 'exec')
        subprocess.run([sys.executable, str(ROOT / 'tools/package_dmg.py'), '--app', str(app), '--arch', args.dmg_arch, '--version', args.version, '--output', str(args.output)], check=True)
        dmg = args.output / f'LedgeSync-{args.version}-macos-{args.dmg_arch}.dmg'
        if not dmg.is_file():
            raise SigningError('The disk image was not created.')
        run('codesign', '--force', '--timestamp', '--sign', identity, str(dmg))
        run('codesign', '--verify', '--strict', '--verbose=2', str(dmg))
        report['artifacts'].append({'name': dmg.name, 'notarization': notarize(dmg, credentials)})
        run('xcrun', 'stapler', 'staple', str(dmg))
        run('xcrun', 'stapler', 'validate', str(dmg))
        report['artifacts'][-1]['assessment'] = assess(dmg, 'open')
        for cli in args.cli:
            cli = cli.resolve()
            sign(cli, identity, entitlements=False)
            run('codesign', '--verify', '--strict', '--verbose=2', str(cli))
            with tempfile.TemporaryDirectory(prefix='ledgesync-notary-') as temp:
                archive = Path(temp) / (cli.name + '.zip')
                run('ditto', '-c', '-k', '--keepParent', str(cli), str(archive))
                report['artifacts'].append({'name': cli.name, 'notarization': notarize(archive, credentials), 'stapled': False})
        report['result'] = 'signed'
    except SigningError as error:
        report['result'] = 'blocked'
        report['reason'] = str(error)
        print(json.dumps(report, indent=2))
        if args.report:
            args.report.write_text(json.dumps(report, indent=2))
        return 3
    print(json.dumps(report, indent=2))
    if args.report:
        args.report.write_text(json.dumps(report, indent=2))
    return 0


if __name__ == '__main__':
    sys.exit(main())
