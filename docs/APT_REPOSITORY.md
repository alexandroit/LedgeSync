# LedgeSync APT repository

The repository URL is `https://ledgesync.com/apt`, suite `stable`, component
`main`, architectures `amd64` and `arm64`. Its current target is Ubuntu 24.04.
Package `ledgesync` installs the graphical application and matching
`ledgesync-cli`; `ledgesync-cli` alone has no graphical dependencies.
See [user installation instructions](PLATFORMS.md#ubuntu-apt).

## Trust and server layout

Repository paths in this document are relative to the checkout root; Markdown
link targets are relative to this document. The absolute server paths below
describe the installed APT repository, not a developer's checkout. They remain
unchanged when the checkout moves. Do not relocate repository data or signing
keys as part of a workspace move. URL routes such as `/apt/` are also unchanged.

The public key is [tracked in the repository](../deploy/apt/ledgesync-archive-keyring.gpg)
and served at `/apt/ledgesync-archive-keyring.gpg`. The fingerprint is
`11B35F4E066806C33AA8653A51AD694F4729F5AB`. The source definition uses this key
through `Signed-By`, scoped to this repository. No `apt-key`, `trusted=yes`,
insecure-repository option or disabled TLS check is required.

The existing Ubuntu production server stores repository state separately from
website releases:

- `/var/lib/ledgesync-apt/gnupg`: root-only mode 0700 signing-key directory.
  Never expose it through Nginx, export it to Git or upload it as a CI artifact.
- `/var/lib/ledgesync-apt/snapshots/<publication-id>`: complete public snapshots.
- `/var/lib/ledgesync-apt/public`: atomic symlink to the active snapshot.
- `/var/lib/ledgesync-apt/packages`: verified distribution `.deb` files.

Nginx's separate `/apt/` location exposes only the public snapshot and requests
cache revalidation. Updating the main website cannot remove the package
repository. The repository contains signed `InRelease` and `Release.gpg`,
per-architecture indexes, SHA256/SHA512 by-hash indexes and immutable pool files.

The initial RSA4096 signing key expires in October 2028. Operators must monitor
its expiry and perform a reviewed key renewal or rotation before then. Key
renewal/publication is not an application auto-update service.

## Build and publication

1. The [native installer workflow](../.github/workflows/installers.yml) downloads
   the pinned existing alpha archives, checks their exact SHA-256 and builds
   packages on native Ubuntu runners. It derives dependencies with
   `dpkg-shlibdeps` and tests install, CLI execution, graphical startup and removal.
2. Download the successful workflow's artifacts. Check package checksums,
   architecture, version and recorded source-binary hashes. Never replace a
   previously published package filename with different bytes; create a new
   Debian revision for changed packaging.
3. Transfer only reviewed `.deb` files and the repository publication tool to
   the server. No application is installed on the production host by this flow.
4. Run `tools/build_apt_repository.py` with `--packages`, a new `--output`
   snapshot path, the protected `--gnupghome`, and the full public `--key`
   fingerprint. Supply the exact Debian package version explicitly, for example
   `--version '0.1.0~alpha.4-1'`; all four input packages must match it. For
   subsequent publications pass `--previous` with the prior
   public snapshot to retain old pool files and by-hash indexes. The tool signs
   with the existing server key and verifies the resulting signature. It never
   exports private key material or activates a snapshot itself.
5. Record the previous public symlink target, then create a sibling temporary
   symlink to the verified new snapshot and replace `public` atomically. Preserve
   previous snapshots for rollback. Content-only updates need no Nginx reload.
6. Dispatch `installers.yml` with `verify_public_apt=true` and the expected
   application `version` (for example `0.1.0-alpha.4`). Both native Ubuntu
   runners fetch the HTTPS public key, check it against the pinned key, reject
   tampered signed metadata, force by-hash fetching, run `apt-get install
   ledgesync`, remove it, install the headless CLI separately and verify removal
   preserves synthetic user data. Successful shell commands on the server alone
   are not evidence of successful public APT installation.

To restore repository metadata, atomically point `public` back to a retained
snapshot, then verify public signed metadata and APT behavior. This does not
downgrade applications already installed on client machines. Do not rewrite
historical package files or alter unrelated Nginx vhosts.

## Initial publication evidence

The initial snapshot is `20261002-alpha1-e796bc5`, published on 2026-10-02 UTC
with Debian version `0.1.0~alpha.1-1`. The two Ubuntu packaging jobs and local
APT lifecycle job in [run 36946272426](https://github.com/alexandroit/LedgeSync/actions/runs/36946272426)
passed. That overall run also contained an initial Windows compiler failure;
it is not recorded as an all-platform success.

The subsequent [public APT run 36946856179](https://github.com/alexandroit/LedgeSync/actions/runs/36946856179)
passed on both native architectures. Both reports verify the pinned key,
signed metadata, tamper rejection, forced by-hash indexes, desktop installation,
headless CLI installation and preservation of synthetic data during removal.
Cloudflare rejects the default Python urllib client with error 1010; onboarding
uses unmodified curl, as documented, followed by the real unmodified APT client.
No Cloudflare protection or TLS/signature validation was disabled.

## Historical alpha.2 publication

Historical snapshot: `20261002-alpha2-ad2cddf`, Debian version `0.1.0~alpha.2-1`.
The existing signing key is unchanged. The prior snapshot and all twelve prior
pool/by-hash files remain available; immutable package bytes were not replaced.
The repository builder now requires an explicit Debian version and rejects a
mixed or incomplete set before signing.

[Installer run 36950139047](https://github.com/alexandroit/LedgeSync/actions/runs/36950139047)
passed both native Ubuntu package checks and local signed APT installation.
[Public run 36950554723](https://github.com/alexandroit/LedgeSync/actions/runs/36950554723)
passed on amd64 and arm64 against `https://ledgesync.com/apt`. Both installed
alpha.2 desktop with its GNOME Keyring recommendation, then tested the separate
CLI and removal preserving synthetic user data. See
[deployment evidence](research/OAUTH_DEPLOYMENT_VERIFICATION.json).

## Historical alpha.3 publication

Historical snapshot: `20261002-alpha3-0ec2f3f`, Debian version `0.1.0~alpha.3-1`.
It includes the bundled Desktop OAuth client in the graphical packages. The
existing signing key and all 24 previous pool/by-hash files are unchanged.
The alpha.2 snapshot remains available for rollback. No application package was
installed on the production server and no Nginx reload was needed.

[Installer run 36954256173](https://github.com/alexandroit/LedgeSync/actions/runs/36954256173)
passed both native Ubuntu package checks and the local signed APT lifecycle.
[Public run 36954499569](https://github.com/alexandroit/LedgeSync/actions/runs/36954499569)
passed on native amd64 and arm64 against the public HTTPS repository. Both
reports confirm the pinned key, signed metadata, tamper rejection, forced
by-hash indexes, desktop installation with Secret Service, separate headless
CLI installation and removal preserving synthetic user data. The downloaded
reports were checked against GitHub's artifact ZIP digests. See
[alpha.3 deployment evidence](research/OAUTH_ONECLICK_DEPLOYMENT_VERIFICATION.json).

## Current alpha.8 publication

Current snapshot: `20261003-alpha8-d9f5234`, Debian version `0.1.0~alpha.8-1`,
built from the published alpha.8 archives (application `d9f5234`, packaging
`2629d00`) with the existing server key. Before activation, all 84 previous pool
and by-hash files were verified unchanged in the new snapshot, the alpha.8
packages were listed for amd64 and arm64, and the `InRelease` signature was
verified. The `public` symlink was replaced atomically; the alpha.7 snapshot is
retained for rollback. [Installer run 37096792431](https://github.com/alexandroit/LedgeSync/actions/runs/37096792431)
passed native packaging and the local signed lifecycle.
[Public run 37097029131](https://github.com/alexandroit/LedgeSync/actions/runs/37097029131)
passed on amd64 and arm64 against `https://ledgesync.com/apt`. No package was
installed on the production server and Nginx was not reloaded. See
[alpha.8 evidence](research/DRIVE_SYNC_ALPHA8_RELEASE.json).

## Previous alpha.7 publication

Current snapshot: `20261003-alpha7-f9319c1`, Debian version `0.1.0~alpha.7-1`,
built from the published alpha.7 archives (application `f9319c1`, packaging
`37dea38`) with the existing server key. Before activation, all 72 previous pool
and by-hash files were verified unchanged in the new snapshot, the alpha.7
packages were listed for amd64 and arm64, and the `InRelease` signature was
verified. The `public` symlink was replaced atomically; the alpha.6 snapshot is
retained for rollback. [Installer run 37092218276](https://github.com/alexandroit/LedgeSync/actions/runs/37092218276)
passed native packaging and the local signed lifecycle.
[Public run 37092498889](https://github.com/alexandroit/LedgeSync/actions/runs/37092498889)
passed on amd64 and arm64 against `https://ledgesync.com/apt`. No package was
installed on the production server and Nginx was not reloaded. See
[alpha.7 evidence](research/DRIVE_SYNC_ALPHA7_RELEASE.json).

## Previous alpha.6 publication

Current snapshot: `20261002-alpha6-c1e00b1`, Debian version `0.1.0~alpha.6-1`,
built from the published alpha.6 archives (application `c1e00b1`, packaging
`f930634`) with the existing server key. Before activation, the new snapshot
was checked:
- all 60 previous pool and by-hash files are unchanged;
- the alpha.6 packages are listed for amd64 and arm64;
- the `InRelease` signature verifies.

The `public` symlink was replaced atomically, and the alpha.5 snapshot is
retained for rollback. [Installer run 37066914046](https://github.com/alexandroit/LedgeSync/actions/runs/37066914046)
passed native packaging and the local signed lifecycle.
[Public run 37067329919](https://github.com/alexandroit/LedgeSync/actions/runs/37067329919)
passed on amd64 and arm64 against `https://ledgesync.com/apt`: pinned key,
signature, tamper rejection, by-hash, desktop and CLI installation, and removal
that preserves user data. No package was installed on the production server and
Nginx was not reloaded. See [alpha.6 evidence](research/DRIVE_SYNC_ALPHA6_RELEASE.json).

## Previous alpha.5 publication (superseded: cannot upload files)

Current snapshot: `20261002-alpha5-64cf420`, Debian version `0.1.0~alpha.5-1`,
built from the published alpha.5 archives (application `64cf420`, packaging
`063f766`) with the existing server key. Before activation, all 48 previous
pool and by-hash files were verified unchanged in the new snapshot and the
`InRelease` signature was verified. The `public` symlink was replaced atomically;
the alpha.4 snapshot is retained for rollback. [Installer run 37054397393](https://github.com/alexandroit/LedgeSync/actions/runs/37054397393)
passed native packaging and the local signed lifecycle;
[public run 37054874133](https://github.com/alexandroit/LedgeSync/actions/runs/37054874133)
passed on amd64 and arm64 against `https://ledgesync.com/apt` (pinned key,
signature, tamper rejection, by-hash, desktop and CLI installation, removal
preserving user data). No package was installed on the production server and
Nginx was not reloaded. See [alpha.5 evidence](research/DRIVE_SYNC_ALPHA5_RELEASE.json).

## Historical alpha.4 publication

Current snapshot: `20261002-alpha4-fcd5784`, Debian version `0.1.0~alpha.4-1`.
Both desktop and native CLI packages include the configured publisher Desktop
client and the approved manual Drive-copy engine. Desktop recommends GNOME
Keyring; the CLI only suggests it and has no graphical-library dependency.
Online CLI use still requires a user D-Bus session and an unlocked Secret Service
collection; package installation alone does not establish either prerequisite.
See the [CLI guide](CLI.md).

[Installer run 36964667348](https://github.com/alexandroit/LedgeSync/actions/runs/36964667348)
passed native packaging, install/removal and local signed APT lifecycle at
packaging commit `13342144685824daa38c774cb3ef7bdf14e315d3`, using the immutable
application commit `fcd578488d07f627372e7f5dd2221e162634bf05`.
[Public APT run 36965018881](https://github.com/alexandroit/LedgeSync/actions/runs/36965018881)
passed on both amd64 and arm64 against the deployed HTTPS repository. Both
reports confirm the pinned key/source, signature and tamper rejection, forced
by-hash downloads, desktop installation, separate CLI installation/execution
and removal preserving synthetic user data. Original workflow ZIP digests were
verified before report extraction.

The existing signing key and all 36 prior pool/by-hash files remain unchanged.
The alpha.3 snapshot remains available for rollback. No application package was
installed on the production server and no Nginx reload was required. These
checks establish package distribution, not live Google consent or an unlocked
headless server session. Exact package and publication evidence is recorded in
[installer evidence](research/DRIVE_COPY_INSTALLERS_RELEASE.json),
[public assets](research/DRIVE_COPY_PUBLIC_ASSETS.json) and [deployment evidence](research/DRIVE_COPY_DEPLOYMENT_VERIFICATION.json).
