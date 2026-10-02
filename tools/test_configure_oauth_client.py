#!/usr/bin/env python3
"""Synthetic-only guards for build-time Google Desktop client injection."""
import contextlib
import io
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import configure_oauth_client as tool

CLIENT = {'installed': {
    'client_id': '123-synthetic.apps.googleusercontent.com',
    'project_id': 'synthetic-project',
    'client_secret': 'synthetic-client-secret-not-valid-at-google',
    'auth_uri': 'https://accounts.google.com/o/oauth2/auth',
    'token_uri': 'https://oauth2.googleapis.com/token',
    'auth_provider_x509_cert_url': 'https://www.googleapis.com/oauth2/v1/certs',
    'redirect_uris': ['http://localhost'],
}}


def data():
    return json.dumps(CLIENT).encode()


class OAuthBuildClientGuards(unittest.TestCase):
    def temporary(self):
        # macOS /var is an OS symlink; tests intentionally use its canonical path.
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        return Path(directory.name).resolve()

    def test_valid_client_and_fixed_google_endpoints(self):
        packaged = json.loads(tool.validate_client(data()))['installed']
        self.assertEqual(set(packaged), {'client_id', 'client_secret', 'auth_uri', 'token_uri', 'redirect_uris'})
        for key, value in packaged.items():
            self.assertEqual(value, CLIENT['installed'][key])
        for endpoint in ('https://accounts.google.com/o/oauth2/auth', 'https://accounts.google.com/o/oauth2/v2/auth'):
            changed = json.loads(data())
            changed['installed']['auth_uri'] = endpoint
            tool.validate_client(json.dumps(changed).encode())

    def test_tokens_web_service_accounts_unknown_fields_and_duplicates_rejected(self):
        invalid = [{'web': CLIENT['installed']}, {'type': 'service_account'},
                   {'installed': CLIENT['installed'], 'access_token': 'synthetic-user-token'}]
        for field in ('access_token', 'refresh_token', 'id_token', 'token', 'extra'):
            changed = json.loads(data()); changed['installed'][field] = 'synthetic-user-token'
            invalid.append(changed)
        for value in invalid:
            with self.assertRaises(tool.ConfigurationError): tool.validate_client(json.dumps(value).encode())
        for value in (b'{"installed":{},"installed":{}}', data().replace(b'"client_id":', b'"client_id":"duplicate","client_id":'),
                      data()+b'{}', b'{', b'\xff', b'NaN', b'null', b'[]', b''):
            with self.assertRaises(tool.ConfigurationError): tool.validate_client(value)

    def test_endpoints_redirects_types_and_bounds_fail_closed(self):
        for key, bad in (('client_id','synthetic.example.test'), ('client_secret',''), ('client_secret','line\nbreak'),
                         ('client_secret','x'*1025), ('token_uri','https://example.test/token'),
                         ('auth_uri','https://example.test/auth'), ('auth_provider_x509_cert_url','http://example.test/cert'),
                         ('redirect_uris',[]), ('redirect_uris',['http://localhost:1234']),
                         ('redirect_uris',['http://localhost/callback']), ('redirect_uris',['http://evil.test']),
                         ('redirect_uris',['http://user@localhost']), ('redirect_uris',[None]), ('project_id',False)):
            changed = json.loads(data()); changed['installed'][key] = bad
            with self.subTest(key=key), self.assertRaises(tool.ConfigurationError):
                tool.validate_client(json.dumps(changed).encode())
        with self.assertRaises(tool.ConfigurationError): tool.validate_client(b' '*(tool.MAX_BYTES+1))

    def test_file_reader_rejects_symlink_parent_and_large_input(self):
        root = self.temporary(); source = root/'client.json'; source.write_bytes(data())
        self.assertEqual(tool.read_client_file(source), data())
        alias = root/'alias.json'
        try: alias.symlink_to(source)
        except OSError:
            if os.name != 'nt': raise
        else:
            with self.assertRaises(tool.ConfigurationError): tool.read_client_file(alias)
        source.write_bytes(b'x'*(tool.MAX_BYTES+1))
        with self.assertRaises(tool.ConfigurationError): tool.read_client_file(source)
        with self.assertRaises(tool.ConfigurationError): tool.read_client_file(root)

    def test_atomic_private_output_no_replace_and_owned_cleanup(self):
        root = self.temporary(); destination = root/tool.OUTPUT.name
        tool.generate(data(), destination)
        if os.name != 'nt': self.assertEqual(stat.S_IMODE(destination.stat().st_mode), 0o600)
        self.assertTrue(destination.read_bytes().startswith(tool.MARKER))
        self.assertIn(b'//go:build desktop', destination.read_bytes())
        original = destination.read_bytes()
        for excluded in (b'synthetic-project', b'project_id', b'auth_provider_x509_cert_url', b'oauth2/v1/certs'):
            self.assertNotIn(excluded, original)
        with self.assertRaises(tool.ConfigurationError): tool.generate(data(), destination)
        self.assertEqual(destination.read_bytes(), original)
        self.assertEqual(list(root.iterdir()), [destination])
        tool.clean(destination)
        self.assertFalse(destination.exists())
        destination.write_bytes(b'unrelated source')
        with self.assertRaises(tool.ConfigurationError): tool.clean(destination)
        self.assertEqual(destination.read_bytes(), b'unrelated source')

    def test_output_symlink_and_parent_symlink_are_rejected(self):
        root = self.temporary(); target = root/'target'; target.write_bytes(b'preserved')
        destination = root/tool.OUTPUT.name
        try: destination.symlink_to(target)
        except OSError:
            if os.name == 'nt': self.skipTest('test user cannot create symbolic links')
            raise
        with self.assertRaises(tool.ConfigurationError): tool.generate(data(), destination)
        with self.assertRaises(tool.ConfigurationError): tool.clean(destination)
        self.assertEqual(target.read_bytes(), b'preserved')
        directory = root/'directory'; directory.mkdir()
        alias = root/'directory-link'; alias.symlink_to(directory, target_is_directory=True)
        with self.assertRaises(tool.ConfigurationError): tool.generate(data(), alias/tool.OUTPUT.name)

    def test_failure_before_publication_leaves_no_client_file(self):
        root = self.temporary(); destination = root/tool.OUTPUT.name
        with patch.object(tool.os, 'link', side_effect=OSError('synthetic failure')):
            with self.assertRaises(OSError): tool.generate(data(), destination)
        self.assertEqual(list(root.iterdir()), [])

    def test_safe_diagnostics_and_environment_removal(self):
        root = self.temporary(); destination = root/tool.OUTPUT.name
        stdout, stderr = io.StringIO(), io.StringIO()
        generate = tool.generate
        with patch.dict(os.environ, {tool.ENVIRONMENT_KEY: data().decode()}), \
             patch.object(tool, 'generate', side_effect=lambda value: generate(value, destination)), \
             contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            self.assertEqual(tool.main([]), 0)
            self.assertNotIn(tool.ENVIRONMENT_KEY, os.environ)
        with patch.dict(os.environ, {tool.ENVIRONMENT_KEY: 'synthetic-invalid-sensitive-value'}), \
             contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            self.assertEqual(tool.main([]), 1)
        output = stdout.getvalue()+stderr.getvalue()
        for text in ('synthetic-client-secret', 'synthetic-invalid-sensitive-value', '123-synthetic', str(root)):
            self.assertNotIn(text, output)
        self.assertIn(tool.FAILURE, output)

    def test_generated_go_compiles_without_client_in_build_metadata(self):
        if shutil.which('go') is None: self.skipTest('Go compiler unavailable')
        root = self.temporary(); generated = root/tool.OUTPUT.name
        tool.generate(data(), generated)
        (root/'go.mod').write_text('module synthetic-client-build\n\ngo 1.21\n')
        (root/'client.go').write_text('package connections\nvar bundledClientJSON string\n')
        (root/'client_test.go').write_text('package connections\nimport "testing"\nfunc TestClient(t *testing.T) { if bundledClientJSON == "" { t.Fatal("client missing") } }\n')
        executable = root/('synthetic.test.exe' if os.name=='nt' else 'synthetic.test')
        env = dict(os.environ); env.pop(tool.ENVIRONMENT_KEY, None)
        for tag in ('desktop', 'oauth'):
            subprocess.run(['go','test','-c','-tags',tag,'-o',str(executable)],cwd=root,env=env,check=True,capture_output=True,timeout=120)
            subprocess.run([str(executable)],cwd=root,env=env,check=True,capture_output=True,timeout=30)
            for excluded in (b'synthetic-project', b'auth_provider_x509_cert_url'):
                self.assertNotIn(excluded, executable.read_bytes())
        metadata = subprocess.run(['go','version','-m',str(executable)],cwd=root,env=env,check=True,capture_output=True,timeout=30).stdout
        for value in ('synthetic-client-secret', '123-synthetic', 'installed'):
            self.assertNotIn(value.encode(),metadata)


if __name__ == '__main__': unittest.main()
