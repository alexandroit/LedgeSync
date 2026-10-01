# Website deployment

The canonical product domain is **ledgesync.com**. The currently published
public site is <https://alexandroit.github.io/LedgeSync/>. GitHub Pages deploys
the tracked `dist/` directory through `.github/workflows/pages.yml`.

The Sites project recorded in `.openai/hosting.json` is a separate, owner-private
preview. It does not establish ownership or public routing for ledgesync.com.

## Domain activation

At the 2026-10-01 check, ledgesync.com returned HTTP 403, resolved to
172.64.80.1, and used Cloudflare nameservers. It was not configured as this
repository's GitHub Pages custom domain. The owner requested that DNS records
be prepared for now; activation is intentionally deferred.

When DNS access is available, verify ledgesync.com in the owner's GitHub account,
then set the repository's Pages custom domain to `ledgesync.com` before changing
DNS. Use these records for the public GitHub Pages deployment:

| Type | Name | Value |
|---|---|---|
| A | @ | 185.199.108.153 |
| A | @ | 185.199.109.153 |
| A | @ | 185.199.110.153 |
| A | @ | 185.199.111.153 |
| CNAME | www | alexandroit.github.io |

Use DNS-only mode during initial certificate validation. Preserve unrelated
records. Review conflicting apex A/AAAA/CNAME records before replacing them;
do not change mail records or nameservers. GitHub provides the account-specific
TXT verification value. No verification value is stored or invented here.

The authoritative values and activation sequence are in
[GitHub's custom domain documentation](https://docs.github.com/en/pages/configuring-a-custom-domain-for-your-github-pages-site/managing-a-custom-domain-for-your-github-pages-site).
For this workflow-based deployment, GitHub reads the custom domain setting;
a repository `CNAME` file is not required.

## Acceptance

Confirm the DNS records, wait for GitHub's certificate, and enable HTTPS
enforcement. Verify HTTP 200, LedgeSync content, stylesheet loading, and the
www-to-apex redirect. Only then replace the repository homepage and provisional
links with `https://ledgesync.com/` and add that URL as the site's canonical
metadata. Record the actual verification date and deployment commit.

Keep the existing public URL working until the domain activation can be
completed. A successful workflow is not evidence that DNS or TLS works.
