# Commit ID map

On 2026-10-02 the repository history was rewritten to clean up commit metadata
and documentation. Every commit received a new ID; the application source is
unchanged, and the release tags point to the new commits.

Release assets (`RELEASE.json`, `INSTALLERS_RELEASE.json`), APT snapshot names,
website release directories and the evidence files in [research](research/)
were created before the rewrite. They cite the previous IDs. Use this table to
find the corresponding commit, newest first.

| Previous ID | Current ID | Date | Subject |
|---|---|---|---|
| `6e58e9d` | `c1e00b1` | 2026-10-02 | Fix live Google Drive uploads (session_crd, detected media types); alpha.6 (#3) |
| `b73c54d` | `79d1ff6` | 2026-10-02 | docs: record verified alpha.5 release, installers, APT and website [skip ci] |
| `3695675` | `22fe7a2` | 2026-10-02 | site: publish alpha.5 downloads and update privacy and terms |
| `063f766` | `996102e` | 2026-10-02 | build: package the alpha.5 release archives into native installers |
| `64cf420` | `6fde029` | 2026-10-02 | build: assemble release assets from an official CI run |
| `df7e0c5` | `1be8f5b` | 2026-10-02 | fix: share one catalog connection per process |
| `45d91f9` | `877e5da` | 2026-10-02 | fix: record run history before a run's terminal state is visible |
| `050d31a` | `fc562c5` | 2026-10-02 | fix: give upload chunks and ranged downloads time on slow connections |
| `884dfb9` | `9ef0705` | 2026-10-02 | test: create migration fixtures with the protected state directory |
| `488fead` | `7250e64` | 2026-10-02 | docs: record sync failure fix, decisions, dispositions and signing needs |
| `4df7fce` | `d828d0f` | 2026-10-02 | build: publisher signing pipeline and live acceptance runner |
| `0dbc4b7` | `d33fda8` | 2026-10-02 | feat: restore copies to a new folder and server CLI for saved pairs |
| `5840afd` | `a90e8aa` | 2026-10-02 | feat(desktop): pairs, activity, history, settings and policy editor |
| `a8a03a5` | `cb0ecdb` | 2026-10-02 | feat: saved sync pairs, history and opt-in automatic copies |
| `7d667aa` | `4f9201d` | 2026-10-02 | fix: make approved Drive copies work on real folders and My Drive |
| `94dbd59` | `c6e189a` | 2026-10-02 | docs: add continuation handoff and portable paths |
| `6f0200d` | `0e4f07d` | 2026-10-02 | docs: record verified alpha.4 downloads, native security and production deployment [skip ci] |
| `6c8f1d1` | `400bec2` | 2026-10-02 | site: publish approved Drive copy downloads and privacy disclosures [skip ci] |
| `1334214` | `661a79b` | 2026-10-02 | build: package approved Drive copies for alpha.4 [skip ci] |
| `fcd5784` | `0130dea` | 2026-10-02 | test: allow native journal flush latency on hosted runners |
| `17fe0d5` | `f0d6887` | 2026-10-02 | feat: share approved Drive copies with native server CLI |
| `daf8b10` | `5fd6dc8` | 2026-10-01 | fix: enforce native storage protections across platforms |
| `5179dbe` | `a3cd55f` | 2026-10-01 | feat: add approved Google Drive folder uploads |
| `232bfd2` | `988058b` | 2026-10-01 | Keep legal contacts readable and record verified website publication [skip ci] |
| `43f7ab6` | `2d97f6b` | 2026-10-01 | Add public privacy policy, terms and existing branding assets |
| `4452150` | `6c245e4` | 2026-10-01 | fix: clarify cleanup warning lifetime and record OAuth checks [skip ci] |
| `ad941e3` | `7ca4c14` | 2026-10-01 | fix: harden OAuth callbacks and confirmed credential lifecycle |
| `c3d36d9` | `efd9f21` | 2026-10-01 | docs: record verified alpha.3 OAuth release and deployment [skip ci] |
| `5c70207` | `6c8ba82` | 2026-10-01 | site: prepare alpha.3 one-click Google Drive downloads [skip ci] |
| `0ec2f3f` | `3a572c7` | 2026-10-01 | build: pin verified alpha.3 payloads for native installers [skip ci] |
| `4da377c` | `390a74d` | 2026-10-01 | feat: connect Google Drive with bundled Desktop OAuth client [skip ci] |
| `638a093` | `5f82ecd` | 2026-10-01 | docs: record verified alpha.2 release and production deployment [skip ci] |
| `82b2f6b` | `6701f2f` | 2026-10-01 | site: publish alpha.2 downloads and Google Drive setup guidance [skip ci] |
| `ad2cddf` | `a053585` | 2026-10-01 | fix: validate APT inputs against the selected release version [skip ci] |
| `e765027` | `188aa60` | 2026-10-01 | build: pin verified alpha.2 payloads for native installers [skip ci] |
| `b4e3d72` | `959570b` | 2026-10-01 | docs: describe Google Drive setup in macOS install instructions [skip ci] |
| `42474b5` | `b21d65e` | 2026-10-01 | feat: add native Google Drive authorization and credential vaults [skip ci] |
| `8bbdc35` | `60fc791` | 2026-10-01 | Record secondary Pages verification and upload cleanup [skip ci] |
| `e1db0df` | `3d00096` | 2026-10-01 | Record verified installer publication and Ubuntu deployment [skip ci] |
| `2890e90` | `8e1e90d` | 2026-10-01 | Publish Windows setup downloads and signed Ubuntu APT installation [skip ci] |
| `8bcb696` | `77cdf33` | 2026-10-01 | Honor the selected Windows Start menu group [skip ci] |
| `55bc826` | `94ea4ce` | 2026-10-01 | Verify public APT with the documented curl onboarding client [skip ci] |
| `0ef12bd` | `8f432bb` | 2026-10-01 | Wait for initialized Windows setup wizard controls [skip ci] |
| `e796bc5` | `bad200d` | 2026-10-01 | Fix Inno Pascal declarations and allow targeted installer validation [skip ci] |
| `2d81542` | `30025a7` | 2026-10-01 | Add native Windows installers and signed Ubuntu APT packaging [skip ci] |
| `6f02e7d` | `70ec199` | 2026-10-01 | Record verified graphical DMG delivery and website update [skip ci] |
| `188ba5d` | `bd56c69` | 2026-10-01 | Publish graphical downloads and macOS DMG links [skip ci] |
| `06ca1e2` | `8eef5db` | 2026-10-01 | Package macOS graphical app as verified DMG images |
| `92f262a` | `62ee246` | 2026-10-01 | Document verified Ubuntu website deployment [skip ci] |
| `92eb75b` | `92b6ebc` | 2026-10-01 | Prepare LedgeSync canonical Ubuntu website [skip ci] |
| `11e05c1` | `619e795` | 2026-10-01 | Document validated alpha and link downloads [skip ci] |
| `89a9121` | `d199b3b` | 2026-10-01 | Implement LedgeSync offline desktop alpha and platform builds |
| `bbfc8a8` | `c2cedbe` | 2026-10-01 | Use owner-selected LedgeSync identity throughout the project |
| `b135d4d` | `f6ab3ce` | 2026-10-01 | Initialize Apache-licensed Confirmar project, source audit, and website |
