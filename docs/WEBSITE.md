# Website deployment

The public website is **<https://ledgesync.com/>**. At the owner's explicit
request, it is hosted on the existing Ubuntu server that also serves
HiperMusicas. The existing Cloudflare DNS routing was retained. The earlier
proposal to point the domain at GitHub Pages is superseded.

GitHub Pages remains a secondary public copy at
<https://alexandroit.github.io/LedgeSync/>; `.github/workflows/pages.yml` publishes
`dist/` there. The Sites project in `.openai/hosting.json` is a separate private
preview, not the production origin.

## Production layout

The website consists of the tracked `dist/index.html` and `dist/style.css`.
It needs no application process, Node service, database or backend.

- Nginx configuration: `/etc/nginx/conf.d/ledgesync.conf`, from the tracked
  [configuration template](../deploy/nginx/ledgesync.conf).
- Immutable content: `/var/www/ledgesync/releases/<full-source-commit>/`.
- Active content: `/var/www/ledgesync/current`, a symlink to a release directory.
- Logs: `/var/log/nginx/ledgesync.access.log` and `ledgesync.error.log`.
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

## Verified deployment

Verified on **2026-10-01 America/Toronto (2026-10-02 UTC)**. The deployed website
source is commit `92eb75b51d978565e1dd8ef939d6cf6320da2299`.

| Check | Observed result |
|---|---|
| Public HTTPS apex and stylesheet | HTTP 200; SHA-256 matches the tracked files below |
| Public HTTP apex | 301 to `https://ledgesync.com/` |
| Public HTTPS www | 308 to `https://ledgesync.com/` |
| Direct origin HTTPS | Validated TLS, HTTP 200; no certificate bypass |
| Direct origin www path/query | 308 preserving path and query at the apex |
| Missing path / hidden Git path | 404 / 403 |
| Canonical metadata | `https://ledgesync.com/` |
| Origin certificate | Let's Encrypt; apex and www SANs; expires 2026-12-30 23:05:10 UTC |
| Nginx configuration and services | `nginx -t` passed; Nginx and Certbot timer active |
| Certificate renewal simulation | Scoped Certbot dry run passed, including the Nginx deploy hook |
| Existing HiperMusicas | Public and origin HTTP 200; same Supervisor PID; shared configuration hashes unchanged |

SHA-256:

```text
441cb0da7e6884ef813f469414ef95ed460dc532703cd8b1474b8ac7fdea12aa  dist/index.html
e03a6c3838152b78220e0f985fb835b21e9928e63ce5bc0823e2eb5e79062152  dist/style.css
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
saved LedgeSync vhost, test and reload. This first deployment has no prior live
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
