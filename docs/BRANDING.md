# Google branding assets and public links

The existing LedgeSync application mark is available without redesign:

- [PNG logo](https://ledgesync.com/assets/ledgesync-logo.png): 512 × 512 pixels,
  transparent outer area, 4,072 bytes. This is a byte-for-byte copy of the existing
  `build/appicon.png`, produced from `frontend/native/mark.json` by the native
  asset preparation script. Use this PNG in Google's logo upload field.
- [SVG source](https://ledgesync.com/assets/ledgesync-logo.svg): unchanged copy
  of `deploy/linux/ledgesync.svg`, for vector use. Google branding accepts PNG,
  JPG and BMP; use the PNG above for that form.

Public branding fields:

| Field | Value |
| --- | --- |
| App name | LedgeSync |
| Application home page | `https://ledgesync.com/` |
| Application privacy policy | `https://ledgesync.com/privacy-policy` |
| Application terms of service | `https://ledgesync.com/public-term` |
| Public support and privacy contact | `alex@alexandro.net` |

The owner supplied the public contact for these pages. Google limits which
addresses are selectable as its support email; publishing this contact on the
website does not add it to that dropdown. Choose the corresponding eligible
Google account or managed group in the console.

[Google's branding guidance](https://support.google.com/cloud/answer/15549049?hl=en)
requires a square image under 1 MB and recommends 120 × 120 pixels for best
display. The provided original PNG is square and well below that file limit.
No new logo, Google trademark, or redesigned symbol was introduced. Uploading a
logo does not itself establish brand verification or Google's approval.

Both pages describe current alpha behavior. The privacy policy distinguishes
local account credentials from website request logs, discloses Cloudflare,
Google Fonts and GitHub, and gives local disconnection and Google revocation
options. Terms preserve Apache-2.0 rights and third-party licenses. These are
project disclosures, not a claim that counsel has reviewed them or that all
jurisdiction-specific obligations have been assessed.

No Google Cloud branding, client, consent, audience or scope settings were changed
by publishing the pages. In the shared Cloud project, review the effect on other
clients before changing project-wide branding.
