# LedgeSync — Canonical Project Identity

**Status:** authoritative for naming and product identity.
**Updated:** 2026-10-01.

- **Product name:** LedgeSync
- **CLI / executable:** `ledgesync`
- **Website / primary domain:** `ledgesync.com`
- **Specification version:** 0.2.1
- **Former working name:** Confirmar (historical only; do not use for new code, UI, docs, package names, commands, or product copy)

## Mandatory agent rule

Before implementation, Codex or any other coding agent must read this file first. If another document still contains `Confirmar`, treat it as historical context unless that passage explicitly describes the old Drive folder or prior specification history. New implementation artifacts must use **LedgeSync**, `ledgesync`, and `ledgesync.com`.

## Product surface

The desktop GUI is the primary product experience. It should feel familiar to users of the **current Google Drive interface** through common interaction patterns such as left navigation, breadcrumbs/path navigation, search, list/grid views, details/inspector, file status, activity/history, and Local ↔ Cloud comparison. Do not copy Google branding, logos, proprietary icons, or pixel-perfect layouts.

The CLI is a secondary automation/headless interface over the same core, using the command `ledgesync`.
