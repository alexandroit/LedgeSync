# Canonical specification reread — 0.2.1

The owner requested another read after updating the Drive documentation on
2026-10-01. Implementation paused while all six root Markdown files and all
twenty technical documents were fetched and compared with the previous read.

## Binding updates

| Updated instruction | Implementation consequence |
|---|---|
| Read PROJECT_IDENTITY.md first | Root agent entry points now link to that file |
| LedgeSync / ledgesync / ledgesync.com | Product, GitHub, commands and website identity follow those exact names |
| GUI is primary | Wails explorer uses the same application service as the secondary CLI |
| Familiar current Google Drive interactions | Sidebar, breadcrumbs, search, list/grid, inspector, status and activity; original LedgeSync branding |
| Local and cloud comparison | Offline alpha labels the simulated destination; native Drive remains a later tested capability |

Architecture, filtering, mutation safety, configuration contracts and backlog
documents 03, 04, 05, 07 and 11 were byte-identical to the prior canonical read.
The source-analysis gate remains mandatory and the completed pinned rclone
audit remains applicable. Existing runtime safety rules were not relaxed.

The updated package's original “not started” statements describe the delivered
specification. Actual implementation status is maintained separately in
[the session handoff](../17_AGENT_HANDOFF_AND_STATUS.md). Local ADR-021 onward
and source/test evidence were preserved instead of overwritten by older status
text from the specification package.

Readable Markdown was retrieved through the connected Google Drive plugin.
The connector did not expose readable manifest content and downloading the
updated raw ZIP returned HTTP 403. Therefore this review establishes the
Markdown contents read, not ZIP integrity or current package-manifest validity.
