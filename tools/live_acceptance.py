#!/usr/bin/env python3
"""Live Google Drive acceptance for a configured native ledgesync CLI.

Run from the repository root after `ledgesync auth connect` in the same user
session, with an account the owner authorized for testing:

    python3 tools/live_acceptance.py --cli build/cache/live/ledgesync --work build/cache/live

The script only creates a synthetic fixture under --work and copies it into a
new "LedgeSync acceptance <timestamp>" pair in My Drive. It never reads other
files, never deletes Drive files and never prints tokens, account e-mail or
Drive object IDs. Remove the acceptance folder from Drive manually afterwards.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import pty
import re
import select
import shutil
import signal
import socket
import subprocess
import sys
import threading
import time
from pathlib import Path

DIGEST = re.compile(r'"planDigest": "([0-9a-f]{64})"')


class Failure(Exception):
    pass


def run(cli: Path, args: list[str], env: dict | None = None, timeout: int = 900) -> tuple[int, str, str]:
    p = subprocess.run([str(cli), *args], capture_output=True, text=True, timeout=timeout, env=env)
    return p.returncode, p.stdout, p.stderr


def interactive(cli: Path, args: list[str], answer_digest: bool = True, env: dict | None = None, kill_when=None, timeout: int = 1800) -> tuple[int | None, str]:
    """Run an approval command in a pseudo-terminal and type the shown digest."""
    pid, fd = pty.fork()
    if pid == 0:
        os.execve(str(cli), [str(cli), *args], env or os.environ.copy())
    output = b''
    answered = False
    deadline = time.time() + timeout
    status = None
    try:
        while time.time() < deadline:
            ready, _, _ = select.select([fd], [], [], 0.25)
            if ready:
                try:
                    chunk = os.read(fd, 65536)
                except OSError:
                    chunk = b''
                if not chunk:
                    # The terminal closed because the CLI exited; keep its status.
                    _, wait = os.waitpid(pid, 0)
                    status = os.waitstatus_to_exitcode(wait)
                    break
                output += chunk
            text = output.decode('utf-8', 'replace')
            if answer_digest and not answered and 'press Enter' in text:
                match = DIGEST.search(text)
                if not match:
                    raise Failure('preview did not show a digest')
                os.write(fd, (match.group(1) + '\n').encode())
                answered = True
            if kill_when and answered and kill_when(text):
                os.kill(pid, signal.SIGKILL)
                os.waitpid(pid, 0)
                return None, text
            done, wait = os.waitpid(pid, os.WNOHANG)
            if done:
                status = os.waitstatus_to_exitcode(wait)
                # Drain remaining output.
                try:
                    while True:
                        chunk = os.read(fd, 65536)
                        if not chunk:
                            break
                        output += chunk
                except OSError:
                    pass
                break
    finally:
        try:
            os.close(fd)
        except OSError:
            pass
    if status is None:
        try:
            os.kill(pid, signal.SIGKILL)
            os.waitpid(pid, 0)
        except (ProcessLookupError, ChildProcessError):
            pass
    return status, output.decode('utf-8', 'replace')


def all_json(text: str) -> list[dict]:
    out, depth, start = [], 0, None
    for i, c in enumerate(text):
        if c == '{':
            if depth == 0:
                start = i
            depth += 1
        elif c == '}':
            depth -= 1
            if depth == 0 and start is not None:
                try:
                    out.append(json.loads(text[start:i + 1]))
                except json.JSONDecodeError:
                    pass
    return out


def final_status(text: str) -> dict:
    """Return the last transfer status, skipping a trailing error report."""
    statuses = [o for o in all_json(text) if 'state' in o]
    return statuses[-1] if statuses else {}


def error_code(text: str) -> str | None:
    """Return the code of the CLI's error report, which never contains secrets."""
    reports = [o for o in all_json(text) if set(o) == {'code', 'message'}]
    return reports[-1]['code'] if reports else None


def tree_digest(root: Path, ignored=lambda rel: False) -> dict[str, str]:
    result = {}
    for path in sorted(root.rglob('*')):
        rel = path.relative_to(root).as_posix()
        if ignored(rel) or path.is_symlink():
            continue
        if path.is_dir():
            result[rel] = 'dir'
        else:
            result[rel] = hashlib.sha256(path.read_bytes()).hexdigest()
    return result


def make_fixture(root: Path) -> None:
    files = {
        '.gitignore': 'node_modules/\nbuild/\n*.log\n',
        'README.md': '# LedgeSync live acceptance fixture\n',
        'zero-byte.bin': b'',
        'docs/guide.txt': 'nested\n',
        'docs/deep/notes.txt': 'deeper\n',
        'nome com espaços/ação ü.txt': 'unicode and spaces\n',
        'app.log': 'ignored log\n',
        'build/output.o': b'ignored build',
        'node_modules/pkg/index.js': 'ignored module\n',
    }
    for rel, content in files.items():
        p = root / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_bytes(content if isinstance(content, bytes) else content.encode())
    (root / 'empty folder').mkdir()
    (root / 'docs/empty-nested').mkdir(parents=True)
    (root / 'media').mkdir()
    (root / 'media/multi-chunk.bin').write_bytes(os.urandom(20 * 1024 * 1024 + 4321))
    (root / 'node_modules/.bin').mkdir(parents=True)
    os.symlink('../pkg/index.js', root / 'node_modules/.bin/tool')


def ignored(rel: str) -> bool:
    return rel.endswith('.log') or rel == 'build' or rel.startswith('build/') or rel == 'node_modules' or rel.startswith('node_modules/')


class TunnelProxy:
    """A minimal HTTP CONNECT proxy whose connections can be cut on demand."""

    def __init__(self):
        self.server = socket.socket()
        self.server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self.server.bind(('127.0.0.1', 0))
        self.server.listen(16)
        self.port = self.server.getsockname()[1]
        self.sockets: list[socket.socket] = []
        self.sent = 0
        self.open = True
        threading.Thread(target=self.accept, daemon=True).start()

    def accept(self):
        while self.open:
            try:
                client, _ = self.server.accept()
            except OSError:
                return
            threading.Thread(target=self.handle, args=(client,), daemon=True).start()

    def handle(self, client: socket.socket):
        try:
            request = b''
            while b'\r\n\r\n' not in request:
                chunk = client.recv(4096)
                if not chunk:
                    return
                request += chunk
            target = request.split(b' ')[1].decode()
            host, port = target.rsplit(':', 1)
            upstream = socket.create_connection((host, int(port)), timeout=30)
            client.sendall(b'HTTP/1.1 200 Connection established\r\n\r\n')
            self.sockets += [client, upstream]
            threading.Thread(target=self.pipe, args=(client, upstream, True), daemon=True).start()
            self.pipe(upstream, client, False)
        except OSError:
            pass

    def pipe(self, a: socket.socket, b: socket.socket, outbound: bool):
        try:
            while self.open:
                data = a.recv(65536)
                if not data:
                    break
                if outbound:
                    self.sent += len(data)
                b.sendall(data)
        except OSError:
            pass

    def cut(self):
        self.open = False
        for s in [self.server, *self.sockets]:
            try:
                s.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
            s.close()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--cli', required=True, type=Path)
    parser.add_argument('--work', required=True, type=Path)
    args = parser.parse_args()
    cli = args.cli.resolve()
    work = args.work.resolve()
    work.mkdir(parents=True, exist_ok=True)
    stamp = time.strftime('%Y%m%d-%H%M%S')
    report: dict = {'started': time.strftime('%Y-%m-%dT%H:%M:%S%z'), 'cli': {'version': run(cli, ['--version'])[1].strip(), 'sha256': hashlib.sha256(cli.read_bytes()).hexdigest()}, 'steps': []}

    def step(name: str, ok: bool, **details):
        report['steps'].append({'step': name, 'ok': ok, **details})
        print(('PASS ' if ok else 'FAIL ') + name, json.dumps(details, ensure_ascii=False)[:400], flush=True)
        if not ok:
            raise Failure(name)

    try:
        code, out, err = run(cli, ['auth', 'status'])
        status = json.loads(out) if out.strip().startswith('{') else {}
        step('connected account', code == 0 and status.get('state') == 'connected', state=status.get('state'))

        fixture = work / f'LedgeSync acceptance {stamp}'
        make_fixture(fixture)
        expected = tree_digest(fixture, ignored)
        original_guide = expected['docs/guide.txt']
        code, out, err = run(cli, ['pairs', 'add', '--root', str(fixture), '--destination', 'root'])
        pair = json.loads(out)['id'] if code == 0 else ''
        step('pair saved for My Drive', code == 0 and bool(pair), destination='My Drive')

        code, out = interactive(cli, ['copy', '--pair', pair])
        final = final_status(out)
        plan = all_json(out)[0]
        step('first copy verified', code == 0 and final.get('state') == 'succeeded', state=final.get('state'), error=error_code(out), files=final.get('completedFiles'), bytes=final.get('uploadedBytes'), excluded=plan.get('excludedCount'), links=plan.get('unsupportedCount'))

        restored = work / f'restore-1-{stamp}'
        code, out, err = run(cli, ['restore', '--pair', pair, '--to', str(restored)])
        got = tree_digest(restored)
        step('independent download matches source hashes', code == 0 and got == expected, error=error_code(err), files=sum(1 for v in got.values() if v != 'dir'), folders=sum(1 for v in got.values() if v == 'dir'))

        code, out = interactive(cli, ['copy', '--pair', pair])
        plan, final = all_json(out)[0], final_status(out)
        actions = {e['action'] for e in plan.get('entries', [])}
        step('unchanged repeat makes no copies', code == 0 and actions <= {'skip', 'unsupported'} and final.get('sentBytes', 0) == 0, actions=sorted(actions), state=final.get('state'), error=error_code(out))

        (fixture / 'docs/guide.txt').write_text('changed content\n')
        code, out = interactive(cli, ['copy', '--pair', pair])
        plan, final = all_json(out)[0], final_status(out)
        changed = [e for e in plan.get('entries', []) if e['relativePath'] == 'docs/guide.txt']
        step('changed file keeps both versions', code == 0 and bool(changed) and changed[0]['action'] == 'keep-both' and final.get('state') == 'succeeded', action=changed[0]['action'] if changed else None, state=final.get('state'), error=error_code(out))

        big = fixture / 'media/interrupted.bin'
        big.write_bytes(os.urandom(64 * 1024 * 1024))
        sizes = re.compile(r'"sentBytes": (\d+)')
        code, out = interactive(cli, ['copy', '--pair', pair], kill_when=lambda text: any(int(m) > 9 * 1024 * 1024 for m in sizes.findall(text)))
        step('process killed during a multi-chunk upload', code is None)
        code, out = interactive(cli, ['copy', '--pair', pair])
        plan, final = all_json(out)[0], final_status(out)
        resumed = [e for e in plan.get('entries', []) if e['relativePath'] == 'media/interrupted.bin']
        step('restart resumes the reserved identity', code == 0 and bool(resumed) and resumed[0]['action'] == 'resume' and final.get('state') == 'succeeded', action=resumed[0]['action'] if resumed else None, state=final.get('state'), error=error_code(out))

        net = fixture / 'media/network-loss.bin'
        net.write_bytes(os.urandom(24 * 1024 * 1024))
        proxy = TunnelProxy()
        env = os.environ.copy()
        env['HTTPS_PROXY'] = f'http://127.0.0.1:{proxy.port}'
        threading.Thread(target=lambda: (time.sleep(0.5), [time.sleep(0.2) for _ in iter(lambda: proxy.sent < 10 * 1024 * 1024, False)], proxy.cut()), daemon=True).start()
        code, out = interactive(cli, ['copy', '--pair', pair], env=env)
        final = final_status(out)
        step('network loss stops without claiming success', code != 0 and final.get('state') != 'succeeded', state=final.get('state'), code=final.get('errorCode'), error=error_code(out))
        code, out = interactive(cli, ['copy', '--pair', pair])
        final = final_status(out)
        step('copy continues after the network returns', code == 0 and final.get('state') == 'succeeded', state=final.get('state'), error=error_code(out))

        restored = work / f'restore-2-{stamp}'
        code, out, err = run(cli, ['restore', '--pair', pair, '--to', str(restored)])
        got = tree_digest(restored)
        expected = tree_digest(fixture, ignored)
        # Drive files are never overwritten: the first copy keeps its original
        # content and the changed file is restored as its separate version.
        kept = [got.pop(k) for k in [k for k in got if k.startswith('docs/guide.txt.ledgesync-')]]
        first = got.pop('docs/guide.txt', None)
        current = expected.pop('docs/guide.txt')
        step('final copy matches after interruptions and changes', code == 0 and got == expected and first == original_guide and kept == [current], error=error_code(err), keptVersions=len(kept), originalKept=first == original_guide)

        code, out = interactive(cli, ['automatic', 'enable', '--pair', pair, '--every', '5'])
        step('automatic copies authorized from a reviewed preview', code == 0, error=error_code(out))
        (fixture / 'docs/added-by-automation.txt').write_text('automatic\n')
        code, out, err = run(cli, ['automatic', 'run', '--pair', pair])
        step('authorized automatic run copies new files', code == 0 and '"state": "succeeded"' in out, error=error_code(err))
        with open(fixture / '.gitignore', 'a') as f:
            f.write('*.txt\n')
        code, out, err = run(cli, ['automatic', 'run', '--pair', pair])
        step('changed ignore rules pause automatic copies', code != 0 and 'AUTOMATION_REVIEW_REQUIRED' in out, error=error_code(err))
        report['result'] = 'passed'
    except (Failure, subprocess.TimeoutExpired, json.JSONDecodeError, KeyError, IndexError) as error:
        report['result'] = 'failed'
        report['failure'] = str(error)
    report['finished'] = time.strftime('%Y-%m-%dT%H:%M:%S%z')
    (work / f'acceptance-{stamp}.json').write_text(json.dumps(report, indent=2, ensure_ascii=False))
    print(json.dumps({'result': report['result'], 'report': str(work / f'acceptance-{stamp}.json')}))
    return 0 if report['result'] == 'passed' else 1


if __name__ == '__main__':
    sys.exit(main())
