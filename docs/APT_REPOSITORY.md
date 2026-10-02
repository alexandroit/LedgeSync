# LedgeSync APT repository

The repository URL is `https://ledgesync.com/apt`, suite `stable`, component
`main`, architectures `amd64` and `arm64`. Its current target is Ubuntu 24.04.
Package `ledgesync` installs the graphical application and matching
`ledgesync-cli`; `ledgesync-cli` alone has no graphical dependencies.
See [user installation instructions](PLATFORMS.md#ubuntu-apt).

## Trust and server layout

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
   fingerprint. For subsequent publications pass `--previous` with the prior
   public snapshot to retain old pool files and by-hash indexes. The tool signs
   with the existing server key and verifies the resulting signature. It never
   exports private key material or activates a snapshot itself.
5. Record the previous public symlink target, then create a sibling temporary
   symlink to the verified new snapshot and replace `public` atomically. Preserve
   previous snapshots for rollback. Content-only updates need no Nginx reload.
6. Dispatch `installers.yml` with `verify_public_apt=true`. Both native Ubuntu
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
