# Website deployment

The public website is **<https://ledgesync.com/>**. At the owner's explicit
request, it is hosted on the existing Ubuntu server that also serves
HiperMusicas. The existing Cloudflare DNS routing was retained. The earlier
proposal to point the domain at GitHub Pages is superseded.

GitHub Pages remains a secondary public copy at
<https://alexandroit.github.io/LedgeSync/>; `.github/workflows/pages.yml` publishes
`dist/` there. The Sites project in `.openai/hosting.json` is a separate private
preview, not the production origin.

## Path conventions

Repository paths in this document are relative to the checkout root; Markdown
link targets are relative to this document. The absolute server paths below
are installed production locations, not paths to a developer's checkout. They
remain unchanged when the checkout moves. Do not create these directories
beneath the checkout or relocate server data as part of a workspace move.

## Production layout

The website consists of tracked HTML/CSS in `dist/`, including the
`privacy-policy/` and `public-term/` pages and existing logo assets in `assets/`.
It needs no application process, Node service, database or backend.

- Nginx configuration: `/etc/nginx/conf.d/ledgesync.conf`, from the tracked
  [configuration template](../deploy/nginx/ledgesync.conf).
- Immutable content: `/var/www/ledgesync/releases/<full-source-commit>/`.
- Active content: `/var/www/ledgesync/current`, a symlink to a release directory.
- Logs: `/var/log/nginx/ledgesync.access.log` and `ledgesync.error.log`.
- APT: separate `/apt/` location serving `/var/lib/ledgesync-apt/public/`; see
  [signed repository operations](APT_REPOSITORY.md). Website switches cannot
  remove APT snapshots or expose the private signing-key directory.
- ACME webroot: `/var/www/ledgesync/acme`.
- TLS certificate: `/etc/letsencrypt/live/ledgesync.com/fullchain.pem` and its
  private key, managed only on the server. Never copy credentials into Git.

Nginx serves HTTPS at the apex and redirects `www` to the apex. HTTP redirects
to HTTPS; Cloudflare currently performs the public HTTP redirect before the
origin's redirect. Missing files return 404, dotfiles are denied, and HTML
requires cache revalidation. CSS may be cached by Cloudflare for four hours;
future stylesheet changes should use a versioned URL or a targeted cache purge.

Certbot uses the server's existing Cloudflare DNS authenticator and credential
file to create transient ACME validation TXT records. No A, AAAA, CNAME,
nameserver or mail records were changed. The existing `certbot.timer` handles
renewal. The certificate-specific renewal hook tests Nginx configuration before
reloading it: `/usr/sbin/nginx -t && /usr/bin/systemctl reload nginx`.

## Verified alpha.8 deployment

Verified on **2026-10-03 UTC**. Website source `a5ef61c8f02447b43e3c43937ba7cf9e40aca903`
publishes the alpha.8 downloads. The release was installed under
`/var/www/ledgesync/releases/<commit>/` with root ownership and 0755/0644
modes. Every file was verified by SHA-256, and the release was activated by
atomic `current` symlink replacement; the previous release `c1c3e2d…` is
retained.

Checks after activation:
- The origin homepage is byte-identical to the source, and the four DMG and
  setup links answer.
- GitHub Pages ([run 37097043910](https://github.com/alexandroit/LedgeSync/actions/runs/37097043910)) serves the alpha.8 links.
- All 57 shared Nginx/Supervisor configuration hashes were unchanged, and `nginx -t` passed without a reload.
- HiperMusicas kept PID 1521428 and answered HTTP 200.

## Verified alpha.7 deployment

Verified on **2026-10-03 UTC**. Website source `c1c3e2da3f4afe5f8e4fa1398f2bcdeebfe220a4`
publishes the alpha.7 downloads, the two-way sync description, and the updated
privacy policy and terms. These disclose full Drive access, local writes inside
synced folders, and the deletion and trash behavior.

The release was installed under `/var/www/ledgesync/releases/<commit>/` with root
ownership and 0755/0644 modes. Every file was verified by SHA-256, and the
release was activated by atomic `current` symlink replacement; the previous
release `0716392…` is retained.

Checks after activation:
- The origin homepage is byte-identical to the source.
- GitHub Pages ([run 37092584371](https://github.com/alexandroit/LedgeSync/actions/runs/37092584371)) serves the alpha.7 links.
- All 57 shared Nginx/Supervisor configuration hashes were unchanged, and `nginx -t` passed without a reload.
- HiperMusicas kept PID 1521428 and answered HTTP 200 publicly and at the origin.

**Runbook note:** in a `set -e` deployment script, hashing the shared
configuration globs exits non-zero when one directory is empty. Add `|| true`
to that command; the listing is still complete.

**Owner action, still open:** browser requests still receive the Cloudflare Web
Analytics beacon injected at the edge.

## Verified alpha.6 deployment

Verified on **2026-10-02 UTC**. Website source `0716392bbe0803eb34126d81af2d4588c2280c7d`
publishes the alpha.6 downloads. The release was installed under
`/var/www/ledgesync/releases/<commit>/` with root ownership and 0755/0644
modes. Every file was verified against the commit by SHA-256 on the server, and
the release was activated by atomic `current` symlink replacement; the previous
release `3695675…` is retained.

Checks after activation:
- The origin homepage is byte-identical to the source.
- GitHub Pages ([run 37067542662](https://github.com/alexandroit/LedgeSync/actions/runs/37067542662)) serves the alpha.6 links.
- All 57 shared Nginx/Supervisor configuration hashes were unchanged, and `nginx -t` passed without a reload.
- HiperMusicas kept the same process and answered HTTP 200 publicly and at the origin.

**Owner action, still open:** browser requests to the public homepage still
receive the Cloudflare Web Analytics beacon injected at the edge. The source,
the origin and non-browser requests do not contain it. See the alpha.5 note
below.

## Verified alpha.5 deployment

Verified on **2026-10-02 UTC**. Website source `3695675d4c299207d000b0cf55bddb119fb5dbff`
publishes the alpha.5 downloads and updated privacy and terms text (local catalog
of saved pairs and history, opt-in automatic copies, restore). The release was
installed under `/var/www/ledgesync/releases/<commit>/` with root ownership and
0755/0644 modes, verified against the commit, and activated by atomic `current`
symlink replacement; the previous release `6c8f1d1…` is retained. All seven files
match the source at the origin with verified TLS, and GitHub Pages
([run 37055091677](https://github.com/alexandroit/LedgeSync/actions/runs/37055091677))
matches as well. All 57 shared Nginx/Supervisor configuration hashes were
unchanged, `nginx -t` passed without a reload, and HiperMusicas kept the same
process and answered HTTP 200 publicly and at the origin.

**Owner action:** through Cloudflare, the public homepage now also contains a
Cloudflare Web Analytics beacon (`static.cloudflareinsights.com/beacon.min.js`)
injected at the edge; it is not in the source or at the origin and was absent at
the alpha.4 verification. The privacy policy states that the website adds no
analytics product. Either disable Cloudflare Web Analytics automatic injection
for ledgesync.com or disclose it in the privacy policy. Zone settings were not
changed by this deployment.

## Historical alpha.4 deployment

Verified on **2026-10-02 UTC**. Current website source is
`6c8f1d19f8c2aa398d98d0a5b39ba5a1999772d1`. It publishes alpha.4 downloads,
the manual folder-copy workflow, server CLI prerequisites and updated privacy
and terms disclosures. The previous source
`43f7ab6ada2711795ca0b14df6bc333f6344e131` remains on the server for rollback.
The seven tracked HTML/CSS/logo files were packaged directly from that commit,
installed with root ownership and 0755/0644 modes, and activated by atomic
symlink replacement. No Nginx reload or application restart was required.

All seven files match their tracked SHA-256 and size through public HTTPS,
direct origin HTTPS with certificate verification, and the secondary Pages
site. This includes direct HTTP 200 responses at `/privacy-policy` and
`/public-term`. The existing `no-cache, no-transform` handling and contact
`alex@alexandro.net` remain in place. The stylesheet and logo bytes are unchanged;
CSS still uses `?v=1c913bddba4f`.

The four desktop download URLs refer to the verified alpha.4 DMGs/setup EXEs.
All **37 public release assets** were downloaded anonymously and checked against
staged SHA-256/size and GitHub metadata. All **106 older assets**, release IDs and
tag objects remain unchanged. See [public download evidence](research/DRIVE_COPY_PUBLIC_ASSETS.json).
The APT snapshot is separately activated as `20261002-alpha4-fcd5784`;
[public APT run 36965018881](https://github.com/alexandroit/LedgeSync/actions/runs/36965018881)
passed native amd64 and arm64 installation/removal against the signed repository.
The website activation preserved that APT target.

[Deployment evidence](research/DRIVE_COPY_DEPLOYMENT_VERIFICATION.json) records
these exact bytes, the retained previous website/APT snapshots and the existing
service checks. All 17 shared Nginx/Supervisor configuration hashes remain
unchanged; `nginx -t` passed and Nginx stayed active. HiperMusicas retained the
same Supervisor PID and returned HTTP 200 through public and direct-origin
HTTPS. No DNS or Google Cloud settings were changed by this release.

Historical authorization-only distribution and legal-page publication evidence
remain in [alpha.3 deployment evidence](research/OAUTH_ONECLICK_DEPLOYMENT_VERIFICATION.json)
and [legal-site evidence](research/LEGAL_SITE_DEPLOYMENT_VERIFICATION.json).
The initial routing and certificate-renewal setup is unchanged.

```text
e6b3e0d083a29d0facfdb678b26f55e2d521c27f7884118f72d9c120c50bba94  dist/index.html
1c913bddba4fbf3e42d2d59766ae16427a6cbfd56f5342b81aae133b392e1504  dist/style.css
```

## Updates and rollback

1. Review and commit the static content. Package only `dist/`, excluding macOS
   extended attributes and resource-fork files. Do not upload the working tree.
2. Through the established production SSH connection, extract into a new
   `/var/www/ledgesync/releases/<full-source-commit>/` directory. Use root ownership,
   directories mode 0755 and files mode 0644. Verify content hashes.
3. Record the existing `current` symlink target. Create a sibling temporary
   symlink to the new release and atomically rename it over `current` using
   `mv -T`. Keep the old release available for rollback. Content-only updates do
   not require a Nginx reload.
4. If changing the vhost, back up only `ledgesync.conf`, install the tracked
   template, run `sudo nginx -t`, then `sudo systemctl reload nginx`. Restore the
   backup if validation fails. Do not edit shared vhosts or restart other apps.
5. Validate apex HTML/CSS and hashes, redirects, TLS, unknown paths and the
   existing HiperMusicas origin/public endpoints. Check CDN freshness after CSS
   changes and update this document with the deployed commit.

For a content rollback, atomically restore `current` to the recorded previous
release and repeat the public checks. For a configuration rollback, restore the
saved LedgeSync vhost, test and reload. The original deployment had no prior live
LedgeSync release; its HTTP bootstrap configuration was retained under
`/var/backups/ledgesync/initial-92eb75b51d97/` for recovery. It is not a HTTPS
service rollback target.

To check renewal without replacing the live certificate:

```sh
sudo certbot renew --cert-name ledgesync.com --dry-run --non-interactive --run-deploy-hooks
```

The production origin is updated through SSH. A GitHub Pages workflow run does
not deploy to Ubuntu. After publishing website source, dispatch the Pages
workflow as needed to keep the secondary copy current.

The latest secondary Pages publication is
[run 36965146423](https://github.com/alexandroit/LedgeSync/actions/runs/36965146423),
with all seven public file hashes matching source commit
`6c8f1d19f8c2aa398d98d0a5b39ba5a1999772d1`.
