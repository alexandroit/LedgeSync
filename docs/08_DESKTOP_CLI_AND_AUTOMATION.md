# 08 — Desktop Explorer, CLI, and Automation Workflows

**Product direction:** the desktop application is the primary experience. The CLI is a secondary interface over the same Go application services; its currently implemented commands are listed below. The visual model should feel immediately familiar to users of the **current Google Drive web app / desktop experience** or desktop file managers without copying Google trademarks, artwork, proprietary assets, or pixel-for-pixel layout. Familiar patterns include: left navigation, top breadcrumb/path bar, search entry, list/grid toggle, file rows/cards with status, details pane, and activity/history views.

## Current implementation: alpha.4 source candidate

Public alpha.3 packages connect a Google account and provide local/offline
previews. The unreleased alpha.4 source adds an explicit desktop folder-copy
workflow: choose a local source, connect, choose My Drive or an existing folder
through Google's system-browser Picker, preview, then approve **Upload folder**.
Account and immutable destination ID are visible before approval. The candidate
preserves `drive.file`; it does not gain visibility over all existing Drive files.

The local root becomes a managed child folder inside the destination, with
included files and empty directories retained. Active ignore rules apply.
Verified copies are reused without upload; changed local files keep both versions
under a stable `.ledgesync-` suffix. No existing file is overwritten or deleted.
Completion reports verified counts, not merely bytes sent. The success action
opens the verified folder in the system browser.

Transfers are explicit and cancellable. Cancellation keeps completed files, and
a new preview reconciles journaled object IDs before continuing. Restarting the
app does not start a job; select the same source/destination and preview again.
Incomplete file uploads may restart because session URLs are not persisted.
The per-user SQLite journal contains plans, IDs, checksums and rule-source
history, but no OAuth tokens or upload-session URLs. Missing previously observed
rules block uploads after restart as well as during the current session.

Source/account/destination changes are blocked while a transfer runs. Disconnect,
confirmed remote revocation and shutdown drain active transfers first. Declining
revocation leaves work running. The native close prompt defaults to **Keep Open**.
Scheduler/watch triggers, shared-drive transfers, restore/download, remote
overwrite/deletion and bidirectional synchronization remain unavailable.

The screen catalogue below is the product design contract, including future
features; it is not a claim that every screen or interaction is implemented.
See [live acceptance](research/DRIVE_UPLOAD_ACCEPTANCE.md) and
[current status](17_AGENT_HANDOFF_AND_STATUS.md).

## Desktop information architecture

Use one native desktop window with stable left navigation, a path/breadcrumb toolbar, a central file browser, a details/inspector panel, and a transfer/activity surface. The user should be able to understand what is local, what is cloud, what is selected for synchronization, and why an item is excluded without opening a terminal.

Suggested primary navigation:

- **Files** — default Drive-style explorer for local roots and authorized cloud locations.
- **Sync pairs / Projects** — configured Local ↔ Cloud relationships and status.
- **Policies** — rule sources, adapters, precedence, validation, and compatibility status.
- **Activity** — live transfer queue, progress, pause/cancel, and retry state.
- **History & Recovery** — verified runs, conflicts, recovery objects, reports.
- **Connections** — Google account and future provider capabilities.
- **Settings** — resource limits, privacy, appearance, accessibility, optional automation.

## Drive-style Files explorer

The main browser supports list and grid modes, breadcrumbs, back/forward navigation, sorting, search/filter, file/folder icons, size/modified metadata, multi-select, context actions, keyboard navigation, drag selection, and refresh. Cloud-native documents and shortcuts must be visually distinct from ordinary files.

Each row/card may expose compact state badges such as `Local only`, `Cloud only`, `Verified`, `Pending`, `Uploading`, `Excluded`, `Conflict`, `Error`, or `Unsupported`. Never encode state by color alone.

A **Show excluded files** toggle is first-class. When disabled, excluded paths are hidden from the ordinary file list but still counted in policy summaries. When enabled, they appear visually de-emphasized with an exclusion badge. Selecting one opens an explanation containing the adapter, policy source, file/property/setting location, rule or property entry, line when meaningful, ancestor condition, group composition result, and final decision.

The explorer must not confuse “excluded from this transfer” with “deleted.” An excluded cloud object remains untouched unless a separately authorized managed-mirror plan explicitly proposes deletion and all safety gates pass.

## Paired Local ↔ Cloud view

A project can switch from ordinary Files view to a paired comparison:

```text
LOCAL                                     GOOGLE DRIVE
Project/                                  Project/
├─ src/               ───────────────▶    ├─ src/
├─ tests/             ───────────────▶    ├─ tests/
├─ node_modules/      EXCLUDED             │
├─ package.json       VERIFIED         =   ├─ package.json
└─ build.zip          CONFLICT         !   └─ build.zip
```

The view shows planned direction rather than implying bidirectional sync. Columns must support status, selected policy, size/hash evidence, last verified run, and intended operation. Filters such as `Changes`, `Excluded`, `Conflicts`, `Errors`, and `All` help review large plans. A user can open Explain from any row.

The paired view is also the main preview surface. Counts and byte totals remain visible while browsing individual operations. No change is applied simply by selecting or dragging files in this comparison unless a specific future manual-copy workflow defines and confirms that action.

## Primary screens and responsibilities

| Screen | Required information and actions |
|---|---|
| Files | Drive-style local/cloud browser, breadcrumbs, list/grid, search, status badges, Show excluded toggle, Explain |
| Sync pairs / Projects | Source/destination identity, last verified run, status, next permitted trigger; create/edit/pause |
| Connection | Google account, granted capability summary, disconnect/revoke distinction, authorized folder picker |
| Project setup | Source picker, root boundary, app-managed destination, copy default, safety summary |
| Policies | Adapter/source type, filename/property/setting, dialect, group, scope, hierarchy, order, enabled/required flags, parse/capability diagnostics |
| Preview / Paired view | Included/excluded/conflicts/errors, counts/bytes, search, operation types, content-verification policy, Local ↔ Cloud comparison |
| Explain | Exact source/provenance, raw pattern/property entry, group order, ancestor blockers, final decision |
| Activity | Acknowledged progress, retry/pause reason, cancellation, per-operation verification |
| History / recovery | Run outcomes, recovery availability, restore-to-new-location, redacted report export |
| Settings | Concurrency, optional schedule/watch, log retention, privacy, capability limitations, UI preferences |

## Policy-source editor

Offer `.gitignore` as the default Git selector, but model every source with an explicit adapter and dialect. Adding `.ignore` or a custom basename requires a visible dialect mapping. Built-in presets may cover Git, Mercurial `.hgignore`, SVN properties, Perforce `.p4ignore`/`p4ignore.txt`, CVS `.cvsignore`, Bazaar/Breezy `.bzrignore`, Fossil ignore-glob, rclone filter/include/exclude, and separately tested code-tool formats.

SVN is presented as a property source (`svn:ignore` / `svn:global-ignores`), not a fake file. The UI displays whether the optional SVN metadata capability is available and which client/profile version is being used. It must never silently downgrade unavailable SVN policy to “no ignores.”

Provide a capability matrix for each configured adapter: `Available`, `Unavailable`, `Unsupported syntax`, `Needs review`, or `Verified profile`. A parse/read-only preview shows source counts and errors before a job can run.

Allow multiple filenames in the same compatible group only when the specification defines their ordering. Allow separate groups for independent policies. In conservative mode, label the behavior “Exclude when any group excludes”; reordering groups must not imply override behavior. In ordered mode, show higher priority first and label “First decisive group wins.” Switching modes requires a selection diff.

## Setup-to-first-copy journey

The user chooses a local root, authorizes an account, selects/creates a dedicated destination, and optionally enables discovered policy adapters. The app inventories the root, reports detected VCS/code-management policy sources, validates their capabilities, and opens the Files/paired preview rather than dumping a command transcript.

The approval surface shows account, immutable destination identity, source, mode, counts, new objects, collisions, potential duplicates, verification method, excluded count, policy-source summary, and a non-destructive default. The user approves the exact plan. Execution reports verified completion and does not silently enable schedules afterward.

## Conflict and destructive-action presentation

Use explicit terms: “copy new file,” “keep both,” “update managed file after recovery,” and “move managed file to trash.” Avoid one generic “sync” control that hides these differences. Mirror requires a separate setup/review workflow. Display every proposed trash entry and recovery availability, count/percentage limits, and single-writer limitations. A modal confirmation cannot compensate for an unsafe planner.

When remote contents changed unexpectedly, show the evidence and safe choices without selecting overwrite by default. When a source becomes excluded, explain that its remote copy will remain untouched. When a disconnected volume reappears at a different identity, request reattachment review rather than using the old path blindly.

## Automation

Unavailable in the current candidate. Interval and file-watch triggers below are
planned only after the interactive browser/preview path is reliable; both must
use the same planner. Minimum interval is a product resource-control setting,
initially proposed as 60 seconds. A watch event is a hint followed by
debounce/reconciliation, not a guarantee of immediate durable upload.

A scheduler requires explicit copy-preauthorization for unchanged project/policy/root identity and allowed operation classes. It cannot approve updates, adoption, mirror deletion, scope expansion, or a newly unavailable required policy source. Pause on those changes and show a review queue. Do not run two jobs for one pair simultaneously; coalesce triggers and reconcile after sleep/restart.

The current native close prompt asks about an active operation and defaults to
**Keep Open**. Background/tray operation and launch-at-login remain future opt-in,
reversible settings. The installer must not silently create a service/daemon or
modify system-wide startup.

## Accessibility and responsiveness

All controls require keyboard operation, visible focus, meaningful labels, adequate contrast, and screen-reader status updates. Never communicate allow/deny/error only through color. Virtualize long file/preview lists without losing search/filter/selection semantics. Rendering progress cannot block transfer cancellation or core work.

Date/time is displayed in the user's locale/timezone but persisted in UTC. Byte units and transfer estimates have explicit units. Initial repository/UI strings are English; future localization uses message IDs. Preserve original Unicode filenames in display and expose safe escaped forms for ambiguous/control characters.

## CLI parity

Every safety-relevant desktop action has a corresponding typed application service usable by the CLI, but the CLI does not dictate the product UX. CLI output supports human-readable and versioned JSON forms for automation, tests, CI, and advanced users. Headless usage must never open a browser unexpectedly; return `AUTH_REQUIRED` with an explicit authorization workflow. The CLI must not bypass safeguards that appear in the desktop.

The current CLI exposes local browse/explain/configuration/plan inspection and
read-only authentication status. **There is no CLI copy/upload/apply command.**
Drive folder selection and approved execution exist in shared typed services,
but their headless command adapter is not implemented. This is a product
limitation, not separate CLI transfer logic or permission to bypass the GUI's
approval boundary. Server packages currently provide those offline tools only.

Current source adds `ledgesync auth status`, using the GUI's `connections.NewGoogleDrive`
factory and `driveauth.Service.Status`. It returns the safe status/account DTO and
`onlineVerified: false`; a saved connection is not evidence of a valid live grant.
This explicit command may ask the OS to unlock its vault. Preview/help commands
do not read credentials. Missing build configuration and vault failures return
redacted JSON errors with exit 6; canceled status returns 130. Successful local
status reads return 0, including disconnected/reconnect-required states.

Connect/check/disconnect/revoke remain GUI actions. Those CLI subcommands return
`AUTH_REQUIRED` and direct the user to Connections without creating the service.
This read-only boundary avoids adding concurrent GUI/CLI token mutations before
cross-process credential-operation locking is implemented. See
[native CLI build configuration](OAUTH_BUILD.md); existing portable packages do
not contain the publisher client or gain authentication through this source edit.
