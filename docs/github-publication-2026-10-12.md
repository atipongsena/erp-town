# GitHub publication and playable demonstrations

Date: 2026-10-12. User authorized repository upload, explanatory README, screenshots,
animated recordings and a live demonstration; the user selected a **private**
`atipongsena/erp-town` repository and requested GitHub Pages if available.

## Published scope

- Source repository: <https://github.com/atipongsena/erp-town>, private.
- Online showcase: <https://atipongsena.github.io/erp-town/>, public static sample.
- `main` contains the coherent development source and cross-chat work, with its
  existing history preserved. Publication work is committed locally on
  `codex/publish-erp-town`, rather than the local default branch.
- `gh-pages` contains 55 compiled static files, authored/licensed GLBs, fonts and
  decoder notices. Its updated artifact commit is
  `c692c3aabc3fb68fdbbe0db5e6590cac31b1988b`.
- No environment files, databases, credentials, raw frame capture folders or
  ignored worktree notes are included. Only the documented `.env.example` is tracked.

`VITE_TOWN_STATIC_DEMO=true` forces the isolated demonstration application at the
public origin. It ignores connection-related URL parameters and never mounts the
live ERP host. The showcase uses fictional documents and illustrative role policies;
its workflow actions do not issue ERP commands or call models. Observer motion uses
three staff and one AI with independent cycles, plus one circulating truck. Other
roles retain filtered story handoffs.

GitHub Pages cannot run the Go service, ERPNext/HRMS or databases. The whole-system
demonstration remains local: `node scripts/live-demo.ts --rounds=1` starts the actual
ERP test-site scenario and prints its URL. `stop` shuts down and restores the test
database. A remotely hosted real ERP installation still needs an appropriate Docker
host and the separate pilot acceptance work; no production environment is activated.

## Recorded media

- [Desktop connected dashboard](town-simulation-final-dispatch-2026-10-12.png):
  previously observed actual submitted dispatch, with desktop HUD.
- [Current concurrent local view](media/live-town.jpg): captured from the actual
  completed local round, with explicit demo-loop labels.
- [GIF](media/live-town.gif) and [MP4](media/live-town.mp4): 80 actual browser captures,
  encoded at eight frames per second into approximately ten seconds of motion.
  The GIF is about 1.0 MiB; it is not an AI-generated or simulated recording.
- Published website: [online scene](media/online-showcase.jpg),
  [completed sample handoff](media/online-workflow-completed.jpg), and
  [phone view](media/online-showcase-phone.jpg).

GitHub's Pages API confirmed the updated artifact above as `built`, with no build
error and HTTPS enforced. Browser inspection at the published origin confirmed
the demo banner, four staff/AI actors and one truck. Actual online UI interactions
advanced all seven fictional scenes by switching AI → Manager → Warehouse → AI →
Accounts roles, completing `DEMO-PE-001`. The 390-pixel check found no horizontal
overflow; the desktop layout contract is covered by the final browser regression.

The bounded business round and visual circulation are deliberately separate.
Repeated staff/truck movement does not create more orders, dispatches or payments.

## Observed checks

| Check | Result |
| --- | --- |
| `pnpm test` | Passed: 299 workspace TypeScript tests, 627 Go tests with `-race`, 205 Frappe tests |
| Frappe skip | One existing `test_tax_invoice_pdf_through_chrome_embeds_sarabun` skip; no failure |
| Full isolated client browser suite before final CSS scoping | 35 passed in 4.2 minutes |
| Final affected client/showcase/HUD suite | 18 passed in 1.6 minutes, including the added desktop regression |
| Launcher lifecycle tests | Six passed |
| Client TypeScript and normal/static Vite builds | Passed |
| Root lint after final CSS | 281 files checked; zero Go lint issues |
| Git whitespace checks | Passed |
| Credential pattern audit | 764 candidate files (~25.9 MB) plus current source history; no matches for service tokens, private-key headers or long credential literals |

The audit is a bounded local pattern check, not a comprehensive security assessment.
Existing ignored environment files were not read or uploaded. The separate art
checkout was not changed; development GLB exports and their provenance were reused.

Two regressions were corrected during packaging: paused looping actors/trucks
continued pose sampling and frame invalidation, and connected HUD layout selectors
also applied to the desktop showcase. Existing pause and newly failing layout tests
established the defects; relevant cases passed after the fixes. The final CSS change
did not alter Go/Frappe behavior; their full-suite results above precede that CSS-only
change. Broader pilot, art-gate, maximum-population and mobile hardware acceptance
remain open as recorded in the program handoff.
