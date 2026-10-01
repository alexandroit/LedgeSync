#!/usr/bin/env python3
"""Check Gitignore reference fixtures in isolated temporary Git repositories.

This checks the expected fixture answers against Git, not a Confirmar executable.
It performs no network calls and never reads or modifies a user repository.
"""
from __future__ import annotations
import argparse
import datetime as dt
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]

def safe_relative(value: str) -> Path:
    p = PurePosixPath(value)
    if not value or p.is_absolute() or any(c in ('', '.', '..', '.git') for c in value.split('/')) or '\x00' in value or '\\' in value:
        raise ValueError(f'Unsafe fixture path: {value!r}')
    return Path(*p.parts)

def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=None, help='Optional report file; otherwise print only.')
    args = parser.parse_args()
    git = shutil.which('git')
    if not git:
        print('SKIPPED: Git is not installed. No reference tests ran.', file=sys.stderr)
        return 2
    data = json.loads((ROOT / 'tests/filter-conformance.json').read_text(encoding='utf-8'))
    version = subprocess.run([git, '--version'], check=True, capture_output=True, text=True, timeout=10).stdout.strip()
    results = []
    for case in data['gitCases']:
        with tempfile.TemporaryDirectory(prefix='confirmar-git-reference-') as temporary:
            base = Path(temporary)
            repo = base / 'repo'
            repo.mkdir()
            home = base / 'isolated-home'
            home.mkdir()
            empty_template = base / 'empty-template'
            empty_template.mkdir()
            empty_excludes = base / 'empty-excludes'
            empty_excludes.write_bytes(b'')
            env = {k: v for k, v in os.environ.items() if not k.startswith('GIT_')}
            env.update({'HOME':str(home), 'XDG_CONFIG_HOME':str(home), 'GIT_CONFIG_NOSYSTEM':'1',
                        'GIT_CONFIG_GLOBAL':str(empty_excludes), 'GIT_CONFIG_SYSTEM':str(empty_excludes),
                        'GIT_TERMINAL_PROMPT':'0', 'LC_ALL':'C'})
            prefix = [git, '-c', 'core.ignoreCase=false', '-c', f'core.excludesFile={empty_excludes}',
                      '-c', f'core.hooksPath={empty_template}']
            subprocess.run(prefix + ['init', '--quiet', f'--template={empty_template}'], cwd=repo, env=env,
                           check=True, capture_output=True, timeout=10)
            for name in case['files']:
                p = repo / safe_relative(name)
                p.parent.mkdir(parents=True, exist_ok=True)
                p.write_bytes(b'fixture\n')
            for name, contents in case['ruleFiles'].items():
                p = repo / safe_relative(name)
                p.parent.mkdir(parents=True, exist_ok=True)
                p.write_bytes(contents.encode('utf-8'))
            if case.get('trackedFiles'):
                for name in case['trackedFiles']:
                    safe_relative(name)
                subprocess.run(prefix + ['add', '--force', '--'] + case['trackedFiles'], cwd=repo, env=env,
                               check=True, capture_output=True, timeout=10)
            request = b''.join(name.encode('utf-8') + b'\0' for name in case['files'])
            result = subprocess.run(prefix + ['check-ignore', '--no-index', '--stdin', '-z'], input=request,
                                    cwd=repo, env=env, capture_output=True, timeout=10)
            if result.returncode not in (0, 1):
                raise RuntimeError(f"{case['id']}: Git failed: {result.stderr.decode('utf-8', 'replace')}")
            actual = sorted(x.decode('utf-8') for x in result.stdout.split(b'\0') if x)
            expected = sorted(case['expectedExcluded'])
            passed = actual == expected
            results.append({'id':case['id'], 'passed':passed, 'expectedExcluded':expected, 'actualExcluded':actual})
            print(('PASS' if passed else 'FAIL') + ' ' + case['id'] + ': ' + case['description'])
    report = {'testedAt':dt.datetime.now(dt.timezone.utc).isoformat(), 'reference':version,
              'scope':'Git expected fixture answers only; no Confirmar or rclone executable tested.',
              'caseCount':len(results), 'passed':sum(x['passed'] for x in results),
              'failed':sum(not x['passed'] for x in results), 'results':results}
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n', encoding='utf-8')
    print(f"{report['passed']}/{report['caseCount']} reference cases passed ({version}).")
    return 0 if report['failed'] == 0 else 1

if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as exc:
        print(f'ERROR: {exc}', file=sys.stderr)
        raise SystemExit(1)
