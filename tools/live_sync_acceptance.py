#!/usr/bin/env python3
"""Live two-way sync acceptance for a configured native ledgesync CLI.

Run from the repository root after `ledgesync auth connect` (full Drive access)
with an account the owner authorized for testing:

    python3 tools/live_sync_acceptance.py --cli build/cache/live/ledgesync --work build/live-sync

Two local folders with the same name ("device A" and "device B") join the same
new Drive folder in My Drive, standing in for two computers. The script only
creates synthetic files under --work, never deletes Drive files itself (sync
moves test files it deleted to the Drive trash), and never prints tokens,
e-mail addresses or Drive object IDs. Remove the "LedgeSync sync test" folder
from Drive afterwards.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import subprocess
import sys
import time
from pathlib import Path


class Failure(Exception):
    pass


def tree(root: Path) -> dict[str, str]:
    out = {}
    for p in sorted(root.rglob('*')):
        rel = p.relative_to(root).as_posix()
        if rel.split('/')[0] == '.ledgesync-trash' or p.name.endswith('.log'):
            continue
        out[rel] = 'dir' if p.is_dir() else hashlib.sha256(p.read_bytes()).hexdigest()
    return out


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--cli', required=True, type=Path)
    parser.add_argument('--work', required=True, type=Path)
    args = parser.parse_args()
    cli, work = args.cli.resolve(), args.work.resolve()
    stamp = time.strftime('%Y%m%d-%H%M%S')
    name = f'LedgeSync sync test {stamp}'
    a, b = work / 'device-a' / name, work / 'device-b' / name
    report: dict = {'started': time.strftime('%Y-%m-%dT%H:%M:%S%z'), 'cli': subprocess.run([str(cli), '--version'], capture_output=True, text=True).stdout.strip(), 'steps': []}
    pairs: dict[str, str] = {}

    def step(title: str, ok: bool, **details):
        report['steps'].append({'step': title, 'ok': ok, **details})
        print(('PASS ' if ok else 'FAIL ') + title, json.dumps(details, ensure_ascii=False)[:300], flush=True)
        if not ok:
            raise Failure(title)

    def sync(*argv: str, expect: tuple[int, ...] = (0,)) -> tuple[int, object]:
        p = subprocess.run([str(cli), 'sync', *argv], capture_output=True, text=True, timeout=1800)
        try:
            data = json.loads(p.stdout) if p.stdout.strip() else None
        except json.JSONDecodeError:
            data = None
        if p.returncode not in expect:
            code = ''
            try:
                code = json.loads(p.stderr[p.stderr.index('{'):]).get('code', '')
            except (ValueError, json.JSONDecodeError):
                pass
            raise Failure(f'sync {argv[0]} exited {p.returncode} {code}')
        return p.returncode, data

    def run(device: str) -> dict:
        _, data = sync('run', '--pair', pairs[device])
        return data[0]['result']

    def write(root: Path, rel: str, content: bytes | str):
        path = root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content if isinstance(content, bytes) else content.encode())
        later = time.time() + 2
        os.utime(path, (later, later))

    try:
        status = json.loads(subprocess.run([str(cli), 'auth', 'status'], capture_output=True, text=True).stdout)
        step('connected with full Drive access', status.get('state') == 'connected' and status.get('scope') == 'https://www.googleapis.com/auth/drive', state=status.get('state'))
        for rel, content in {'.gitignore': '*.log\n', 'a.txt': 'alpha', 'c.txt': 'shared', 'docs/b.txt': 'bravo', 'nome com espaços/ação.txt': 'unicode', 'app.log': 'ignored', 'media/big.bin': os.urandom(20 * 1024 * 1024 + 99)}.items():
            write(a, rel, content)
        (a / 'empty folder').mkdir()
        b.mkdir(parents=True)

        _, first = sync('add', '--root', str(a))
        pairs['A'] = first['pair']
        step('device A: first sync uploads the folder', first['result']['uploaded'] == 6 and first['state'] == 'synced', uploaded=first['result']['uploaded'], folders=first['result']['foldersMade'])
        _, joined = sync('add', '--root', str(b))
        pairs['B'] = joined['pair']
        step('device B: joining downloads everything', joined['result']['downloaded'] == 6 and tree(a) == tree(b), downloaded=joined['result']['downloaded'])

        write(a, 'a.txt', 'alpha edited on A')
        run('A')
        got = run('B')
        step('edit on A reaches B', (b / 'a.txt').read_text() == 'alpha edited on A' and got['downloaded'] == 1)

        write(b, 'from-b.txt', 'created on B')
        run('B')
        got = run('A')
        step('new file on B reaches A', (a / 'from-b.txt').read_text() == 'created on B' and got['downloaded'] == 1)

        (a / 'docs' / 'b.txt').unlink()
        sent = run('A')
        got = run('B')
        trashed = list((b / '.ledgesync-trash').rglob('b.txt'))
        step('deletion on A moves B\'s copy to its local trash', sent['deletedRemote'] == 1 and got['deletedLocal'] == 1 and not (b / 'docs' / 'b.txt').exists() and len(trashed) == 1)

        write(a, 'c.txt', 'version from A')
        write(b, 'c.txt', 'version from B')
        run('A')
        got = run('B')
        run('B')
        run('A')
        conflicts_a = [p.name for p in a.iterdir() if p.name.startswith('c (conflict ')]
        conflicts_b = [p.name for p in b.iterdir() if p.name.startswith('c (conflict ')]
        step('edits on both sides keep both versions everywhere', got['conflicts'] == 1 and (a / 'c.txt').read_text() == 'version from A' and (b / 'c.txt').read_text() == 'version from A'
             and len(conflicts_a) == 1 and conflicts_a == conflicts_b and (a / conflicts_a[0]).read_text() == 'version from B')

        for i in range(25):
            write(a, f'bulk/file-{i:02d}.txt', f'bulk {i}')
        run('A')
        run('B')
        for i in range(25):
            (a / f'bulk/file-{i:02d}.txt').unlink()
        code, held = sync('run', '--pair', pairs['A'], expect=(5,))
        step('deleting 25 files at once waits for confirmation', code == 5 and held[0]['remoteDeletes'] == 25 and len(list((b / 'bulk').iterdir())) == 25)
        sync('restore-deletes', '--pair', pairs['A'])
        step('restore brings the files back instead of deleting them', len(list((a / 'bulk').iterdir())) == 25)

        watcher = subprocess.Popen([str(cli), 'sync', 'watch'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            write(a, 'while-watching.txt', 'sent automatically')
            deadline = time.time() + 180
            while time.time() < deadline and not (b / 'while-watching.txt').exists():
                time.sleep(2)
            step('background sync sends and receives without commands', (b / 'while-watching.txt').exists() and (b / 'while-watching.txt').read_text() == 'sent automatically', seconds=int(180 - (deadline - time.time())))
        finally:
            watcher.terminate()
            watcher.wait(timeout=60)
        report['result'] = 'passed'
    except (Failure, subprocess.TimeoutExpired, OSError, KeyError, IndexError, TypeError) as error:
        report['result'] = 'failed'
        report['failure'] = str(error)
    finally:
        _, listing = sync('list', expect=(0, 1, 2))
        report['driveFolderListed'] = bool(listing)
        for device in pairs:
            subprocess.run([str(cli), 'sync', 'remove', '--pair', pairs[device]], capture_output=True)
    report['finished'] = time.strftime('%Y-%m-%dT%H:%M:%S%z')
    out = work / f'sync-acceptance-{stamp}.json'
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(report, indent=2, ensure_ascii=False))
    print(json.dumps({'result': report['result'], 'report': str(out)}))
    return 0 if report['result'] == 'passed' else 1


if __name__ == '__main__':
    sys.exit(main())
