#!/usr/bin/env python3
"""Validate repository documentation/contracts, or the original package manifest."""
from __future__ import annotations
import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import sys
from urllib.parse import unquote
ROOT = Path(__file__).resolve().parents[1]

def canonical_digest(data: dict) -> str:
    value = {k:v for k,v in data.items() if k != 'planDigest'}
    raw = json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(',', ':'), allow_nan=False)
    return hashlib.sha256(raw.encode('utf-8')).hexdigest()

def semantic_config(data: dict) -> None:
    groups = data['filters']['groups']
    if len({g['id'] for g in groups}) != len(groups):
        raise ValueError('duplicate group IDs')
    if len({g['priority'] for g in groups}) != len(groups):
        raise ValueError('duplicate group priorities')
    for group in groups:
        seen = set()
        for source in group['sources']:
            name = source['value']
            stype = source['type']
            dialect = group['dialect']
            if not name or '\x00' in name:
                raise ValueError(f'unsafe rule source: {name!r}')
            if stype in ('recursive-basename', 'root-file'):
                if PurePosixPath(name).is_absolute() or '\\' in name or ':' in name or any(p in ('', '.', '..') for p in name.split('/')):
                    raise ValueError(f'unsafe file-backed rule source: {name!r}')
            if stype == 'recursive-basename':
                allowed = {'gitignore','p4ignore','cvsignore','npmignore'}
                if '/' in name or dialect not in allowed:
                    raise ValueError(f'invalid recursive selector for {dialect}')
            if stype == 'recursive-vcs-property':
                expected = {'svn-ignore':'svn:ignore','svn-global-ignores':'svn:global-ignores'}
                if expected.get(dialect) != name:
                    raise ValueError(f'invalid VCS property source for {dialect}: {name!r}')
            if stype == 'root-file' and dialect in {'svn-ignore','svn-global-ignores'}:
                raise ValueError('SVN ignore profiles require property sources')
            key = (source['type'], name)
            if key in seen:
                raise ValueError('duplicate selector within group')
            seen.add(key)

def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--allow-missing-manifest', action='store_true', help='Bootstrap authoring only.')
    parser.add_argument('--package', action='store_true', help='Also validate the original documentation-package manifest; use only in an untouched extracted package.')
    args = parser.parse_args()
    failures, skips, checks = [], [], []
    excluded_dirs = {'.git', '.venv', 'node_modules', '__pycache__', 'build', 'bin', 'artifacts', 'wailsjs', 'dist'}
    import os
    all_files = []
    for directory, dirs, filenames in os.walk(ROOT):
        dirs[:] = [d for d in dirs if d not in excluded_dirs]
        all_files.extend(Path(directory) / name for name in filenames if name != '.DS_Store' and not name.startswith('._'))
    all_files.sort()
    markdown = [p for p in all_files if p.suffix == '.md']
    link_count = 0
    for p in markdown:
        text = p.read_text(encoding='utf-8')
        if text.count('```') % 2:
            failures.append(f'unbalanced fenced block: {p.relative_to(ROOT)}')
        for destination in re.findall(r'(?<!!)\[[^\]]*\]\(([^)]+)\)', text):
            destination = destination.split(' ', 1)[0]
            if re.match(r'^[a-zA-Z][a-zA-Z0-9+.-]*:', destination) or destination.startswith('#'):
                continue
            path = unquote(destination.split('#',1)[0])
            if not path:
                continue
            resolved = (p.parent / path).resolve()
            link_count += 1
            if not resolved.is_relative_to(ROOT.resolve()) or not resolved.exists():
                failures.append(f'broken/escaping local link: {p.relative_to(ROOT)} -> {destination}')
    checks.append(f'{len(markdown)} Markdown files and {link_count} local links checked')
    parsed = {}
    for p in all_files:
        if p.suffix == '.json':
            try:
                parsed[str(p.relative_to(ROOT))] = json.loads(p.read_text(encoding='utf-8'))
            except (ValueError, UnicodeError) as exc:
                failures.append(f'invalid JSON: {p.name}: {exc}')
    checks.append(f'{len(parsed)} JSON files parsed')
    try:
        from jsonschema import Draft202012Validator, FormatChecker
    except ImportError:
        skips.append('JSON Schema validation: optional jsonschema dependency unavailable')
    else:
        for schema_path in ('schemas/project.schema.json', 'schemas/plan.schema.json'):
            Draft202012Validator.check_schema(parsed[schema_path])
        for name, data in parsed.items():
            if name.startswith('examples/project.'):
                Draft202012Validator(parsed['schemas/project.schema.json'], format_checker=FormatChecker()).validate(data)
                semantic_config(data)
            if name == 'examples/plan.example.json':
                Draft202012Validator(parsed['schemas/plan.schema.json'], format_checker=FormatChecker()).validate(data)
                if data['planDigest'] != canonical_digest(data):
                    failures.append('example plan digest mismatch')
                if data['summary']['operationCount'] != len(data['operations']):
                    failures.append('example plan summary mismatch')
        project_count = sum(1 for name in parsed if name.startswith('examples/project.'))
        checks.append(f'2 schemas and {project_count + 1} configuration/plan examples validated; plan digest verified')
    fixture = parsed['tests/filter-conformance.json']
    cases = fixture['gitCases'] + fixture['rcloneCases'] + fixture['compositionCases']
    if len({c['id'] for c in cases}) != len(cases):
        failures.append('duplicate filter fixture IDs')
    for case in fixture['gitCases'] + fixture['rcloneCases']:
        if not set(case['expectedExcluded']).issubset(case['files']):
            failures.append(f"unknown excluded path in {case['id']}")
    safety = parsed['tests/safety-scenarios.json']['scenarios']
    if len({s['id'] for s in safety}) != len(safety):
        failures.append('duplicate safety fixture IDs')
    checks.append(f'{len(cases)} filter fixtures and {len(safety)} safety scenarios checked structurally')
    manifest_path = ROOT / 'PACKAGE_MANIFEST.json'
    if args.package and manifest_path.exists():
        manifest = parsed['PACKAGE_MANIFEST.json']
        indexed = {f['path']:f for f in manifest['files']}
        actual = {str(p.relative_to(ROOT)):p for p in all_files if p != manifest_path}
        if set(indexed) != set(actual):
            failures.append('manifest file inventory differs from package')
        for name, metadata in indexed.items():
            if name not in actual:
                continue
            content = actual[name].read_bytes()
            if hashlib.sha256(content).hexdigest() != metadata['sha256'] or len(content) != metadata['bytes']:
                failures.append(f'manifest content mismatch: {name}')
        checks.append(f'{len(indexed)} manifest entries checked (manifest excludes itself)')
    elif not args.package:
        checks.append('repository mode: original package manifest retained as historical provenance (not a live source inventory)')
    elif args.allow_missing_manifest:
        skips.append('manifest intentionally not yet generated (authoring mode)')
    else:
        failures.append('PACKAGE_MANIFEST.json is missing')
    for check in checks: print('PASS:', check)
    for skip in skips: print('SKIP:', skip)
    for failure in failures: print('FAIL:', failure, file=sys.stderr)
    print(f'Validation result: {len(failures)} failure(s), {len(skips)} skipped check(s).')
    return 1 if failures else 0

if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f'ERROR: {exc}', file=sys.stderr)
        raise SystemExit(1)
