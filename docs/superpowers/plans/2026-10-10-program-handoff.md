# ERP Town A–H — cross-agent handoff

2026-10-12 live-demo review continuation: user requested simultaneous, repeating scene work and a moving truck. The dedicated test demo now advertises `motion_mode: "loop"`; four independent visual team cycles and one circulating truck continue after the real bounded ERP round. Scene/HUD copy identifies demo motion; actual ERP statuses and ordinary finite transport remain authoritative. Pause/hidden, replay, ERP failure and scope-exit cleanup are covered. See `docs/town-simulation-dashboard-evidence-2026-10-12.md` for updated evidence. No commit/push/deployment occurred.

Recorded 2026-10-10 on `feat/go-platform` by Codex. The user authorized local Git
commits to make progress discoverable by other agents. The user wants the entire
A–H program completed, with strictly sequential work and no parallel agents.
The program is not complete. Read this record alongside the implementation plans
and Git history; do not infer additional operational authorization from it.

Scope update, 2026-10-10: the user deferred PromptPay and LINE OA (F) and authorized
continuing A/B/C/D/E/G/H through their plans. Ordinary Payment Entry remains in the
sales-to-cash slice. This continuation does not certify the open art score/gates,
authorize dependency/manifest changes, provision credentials or deploy a pilot.
The owning art chat is now also implementing the next client protocol step in
this primary checkout, sequentially, preserving both checkouts' existing changes.

Code checkpoint: `9666837` (`feat(world): implement recoverable world state and websocket sync`).

Latest local continuation, 2026-10-11: the user approved the protected local
E/G/H scope in `../specs/2026-10-11-program-completion-design.md`. The table below
reflects those implementations; older evidence paragraphs remain historical
checkpoints. Current Git HEAD is `b719ca5`, branch `feat/go-platform`; this work
is uncommitted. Exclusions still cover production/pilot operations, permanent
credentials, paid/external model calls, hook installation and Git publication.

## Current scope and ownership

| Part | State in this checkout | Next action |
| --- | --- | --- |
| A: art | Existing work is owned by another chat in `D:/ERP_Town-art-direction/`, branch `feat/art-direction`; G-A3/G-A4 remain open in the inspected scorecard. | Reuse its reviewed tokens, assets and references; do not recreate art or overwrite its uncommitted work. |
| B: foundation | Prior foundation and Go platform work is recorded in Git. This change adds readiness fixes on top. | Retain existing contracts and deferred pilot follow-ups. |
| C: world state | Implementation and local acceptance complete; a temporary test-site reader and live browser path passed. Dev/pilot reader provisioning remains outstanding. | Follow `docs/world-state-operations.md` for separately authorized dev/pilot operations. |
| D: town client | Real bilingual 3D/document client and actual sales-to-cash acceptance. Glass transmission optimization reduces peak draws to 275, or 294 with two humans/one AI, within the unchanged 300 budget. | Observe mobile/real connected load, texture/GPU memory, postprocessing and named art gates; provision dev/pilot separately. |
| E: agents | Typed dual-credential policy, durable claims/audit/sliding rate limits, transactional River worker/outbox, native loopback SDK contracts, authenticated internal MCP and AI-only progress implemented. Actual test ERP draft/restart/denial acceptance passed. | Native external models remain off. Codex selection fails closed while its ERP-only capability boundary is unproven. Production launcher/credentials/uncertain-write reconciliation operations remain separate. |
| F: PromptPay/LINE | Deferred by the user on 2026-10-10. | Keep out of the current completion scope; retain ordinary ERP Payment Entry. |
| G: avatar/replay | Session replay, self consent/appearance, ephemeral presence, three existing development character assets and bounded retained business history implemented. Actual opt-out/company/history tests passed. | Experience routes are opt-in host entry points. Observe offline account-removal integration and named character review; no human XP/tracking. |
| H: dev-agent bridge | ERP-free separate loopback/token ingestion, safe Claude/Codex hook normalization, transactional duplicate/sequence guards and IT events/rendering implemented. Real local HTTP/concurrency/shutdown checks passed. | No hooks installed or token provisioned. Codex notification reports a finished turn only; host supplies trusted task/start metadata. |

## Implementation evidence

The world implementation includes immutable company projections, strict format-2
PostgreSQL snapshots, bounded raw Redis recovery independent of durable dedupe,
read-only ERP reconciliation, per-company rooms and protobuf WebSocket sync.
JWT company/role/private visibility is enforced server-side. Resume tokens are
short-lived, single-use and bound to the same identity/company/roles/version.

Recovery guards pending entries and trim races; one advisory-lock owner runs each
stream/group. Room/WebSocket cancellation is checked with leak tests. The Go world
reader uses a separate read-only credential. A temporary reader was created only
on `test.localhost` for the 2026-10-11 acceptance and removed by snapshot restoration.
No development/pilot reader has been provisioned. Retained snapshots cannot recover old approval/error history once both
the snapshot and Redis history are unavailable; ERP rebuild restores ERP facts.

Readiness fixes recover consumer panics, count/log stale sequences after commit,
correlate safe gateway access logs with ERP HTTP retries, and prevent a later
outbox event from overtaking a Pending subject head in backoff or under another
publisher's lock. Independent subjects still progress, with the existing total
batch limit. The Frappe publisher was not changed.

Observed checks on 2026-10-10, recorded at the point they ran:

- TypeScript typecheck and 73 package tests passed.
- The full Go race suite passed 412 tests in 215.454 s before the final WebSocket
  shutdown regression and outbox ordering change. After the shutdown fix, all
  WebSocket tests passed with `-race` in 7.624 s.
- After the outbox fix, the complete `internal/events` suite passed with
  `-race -p 1 -count=1` in 96.235 s. All three new ordering regressions were observed
  failing before implementation. The entire Go suite was not rerun after this fix.
- Python checks passed: 45 unit and 189 integration tests, using `test.localhost`
  only, with one existing Chromium PDF skip and the existing session API
  `limit_page_length` deprecation warning. Ruff was unavailable; it was not installed.
- The local 200-client acceptance measured 4,000 patches: p95 89.5308 ms, max
  95.7054 ms, with the race detector. It measures from Redis entry timestamps;
  this is not a pilot deployment or a WebGL performance result.
- Full `pnpm lint` passed after the final code changes. Generated sqlc output was
  produced by the pinned local generator, not hand edited.
- At commit preparation, staged whitespace checks found an extra blank EOF in the
  generated TypeScript protocol. The generation script now normalizes its EOF;
  `pnpm world:gen` succeeded twice with byte-identical Go/TS output, the two protocol
  tests passed, and full lint passed again. No dependency versions changed for this.

Detailed evidence and remaining follow-ups:

- `docs/superpowers/plans/2026-10-10-world-state.md`
- `docs/superpowers/specs/2026-10-10-world-state-design.md`
- `docs/superpowers/plans/2026-10-09-go-platform-followups.md`
- `docs/world-state-operations.md`

This continuation added D's in-memory `WorldClientState` and injectable
`WorldConnection` in `packages/world-proto`, with no protocol/generated changes.
Observed: 42 package tests, package typecheck and full root lint passed. The
tests cover real binary frames through injected sockets and native factory type
compatibility. The user then approved D's exact dependencies and manifests. The
new client passed all six browser cases on development and production builds,
including actual GLB rendering, binary fixture sync, expiry clearing and fallback.
All workspace unit tests passed (138), both package typechecks passed, build and
full lint passed. Rendering measured 219,650 triangles/241 calls using SwiftShader;
environment compression reduced 7,672,772 bytes to 764,804. No live ERP/Go browser
handshake, reader provisioning or hardware FPS acceptance had run at that checkpoint. These edits are
uncommitted; the earlier local commit authorization belongs to the earlier chat
and does not authorize commits in this continuation.

Continuation, 2026-10-11: the user authorized the proposed bounded local test scope.
`tests/integration/client-acceptance.ts` uses the existing snapshot/seed/restore
harness, temporary reader credentials, an ephemeral JWT issuer and test-only
same-origin routing. The built 3D client passed real session login, projection
snapshot, gateway Sales Order creation/submission, ERP/outbox/Redis/world update,
Classic ERP navigation, bilingual status, disconnect and real JWT expiry clearing.
Actual ERP work statuses and canonical `/desk` routes are now handled. The reader's
read/write/company restrictions are checked against ERP, not an injected mock.
The live race/leak test and site restoration passed; 139 workspace tests, six
existing browser cases, three helper unit tests and five existing helper integration
tests passed. The normal port-5173 preview remains disconnected. No new dependencies,
production/pilot configuration, source art edits, commits or external writes occurred.

Document continuation, 2026-10-11: delivery/invoice/payment GET/POST routes now
complete the existing Sales Order/submit gateway. Source company/status checks
precede writes; ERP remains the role authority. The connected document desk
creates drafts, reads actual details and confirms separately, with exact decimal
strings, Classic alternatives, bilingual keyboard/mobile/no-WebGL controls and
in-memory stale/uncertain-write protection. Both live browser cases passed,
including the real sales-to-cash UI with departmental sessions, payment-draft
non-settlement and zero outstanding after receipt submission. Go race/leak checks
and test-site restoration passed. Eight fixture cases passed; the client now has
35 unit tests. Build/typecheck, root lint and integration-tag Go lint passed.
The final approval race regression passed after a focused fix. Development/pilot
provisioning, hardware metrics and art gates remain open. This increment is uncommitted.

Outbox readiness continuation, 2026-10-11: the dedicated Redis publisher disables
hidden command retries, honors context deadlines and caps dial/pool/socket waits
at 5 s. Each XADD has a separate default 5 s deadline; lost acknowledgments remain
durable failures with the original event id, and parent cancellation does not spend
an attempt. Published-row cleanup runs at startup and daily in bounded batches
(default 500), interleaved with publishing. It uses published_at and the existing
400-day Foundation retention, skips locked rows and preserves unresolved rows and
subject seq counters. This adds a sqlc query regenerated by the pinned generator;
no migration, dependency, ERP, credential or deployment change was made. Six new
regression tests passed with race/leak checks in 33.104 s after the original three
regressions were observed failing. The complete fresh Go suite passed 436 tests
with `-race -count=1` in 122.883 s, including gateway, world, store, cmd wiring and
the full events suite. Root lint passed with 0 Go issues, and `git diff --check`
passed. This is E's event-pipeline prerequisite, not an implemented agent runtime
or provider call. No live ERP/browser acceptance or pilot load test was rerun for
this backend-only increment. Changes remain uncommitted on `feat/go-platform`,
with HEAD still `b719ca5`.

Agent-core continuation, 2026-10-11: `go/internal/agents` now implements a local,
single-use provider-independent run loop with whole-batch tool validation, copied
inputs/history, successful-call replay caching, turn/tool/reported-token/payload
limits and context deadlines. Unknown/conflicting intents, human-review outcomes,
uncertain tools and audit failures stop dispatch. Tool outcomes are audited after
cancellation using a bounded cleanup context; adapter panics are contained without
raw error payloads. Forty tests passed with race/leak checks in 1.467 s, including
concurrent reuse and real cancellation sequencing; root lint passed with 0 Go
issues. Initial behavior tests and later panic regressions were observed failing.
The package is not wired into cmd, gateway, MCP, events or the client. There are
no SDK, queue, migration, auth or credential changes and no external provider calls.
The existing full-Go checkpoint remains the preceding 436-test run; this isolated
package has no production callers. Operational policy, durable claims/audit,
restart recovery, subject ownership and provider integration still require E's
separate design/authorization. This increment remains uncommitted.

## Art coordination

Program completion continuation, 2026-10-11: G's client session replay records only
the already displayed document identities/statuses, bounded to 64 versions, 1 MiB
of encoded display facts and 15 minutes. Removal prunes the same identity from
all old points, while connection/company/expiry changes clear the entire history.
A timer expires points without incoming frames. Historical mode deactivates the
document controller and hides document actions until an explicit return live.
No raw world payload, amount, credential or human activity is recorded/persisted.
All 168 workspace tests passed (54 client), client build/typecheck and root lint
passed; ten fixture browser cases passed in 46.9 s. Both real test-site cases then
passed (43.1s, 26.3s), Go race/leak acceptance passed in 81.424s and the test DB was
restored. A remains unchanged in its owning checkout with
G-A3/G-A4 open and score 2.22/3; these gates have not been certified.

The exact remaining E/G/H module/schema/authentication proposal is recorded in
`../specs/2026-10-11-program-completion-design.md`, with sequential tasks in
`2026-10-11-program-completion.md`. Its protected local scope requires explicit
approval under the user's global AGENTS instructions. No module manifest,
migration, authentication, permanent credential, deployment or provider call
changed in this increment. HEAD remains `b719ca5`; all changes remain uncommitted.

Final independent checks: all 169 workspace tests passed (55 client), eleven
fixture browser cases passed in 1.3m, and client build/typecheck plus root lint
passed. The fresh JS-limits regression was observed failing before its fix. The
hardware probe exposed a separate demand-idle animation jump (only two drawn
frames after idle); roof/camera steps now cap delta at 50ms. The same RTX 3080 Ti
D3D11 probe records 64-68 frames per transition, p95 <=16.734ms, up to 619,906
triangles and 413 shadow-inclusive draws. The 300-draw budget is not met.
See `../../client-renderer-evidence-2026-10-11.md` and its raw JSON report.
No mobile/connected-load/GPU-timer-query or final art acceptance is claimed.

See `docs/superpowers/plans/2026-10-10-art-foundation.md` for the existing art
worktree's source paths and inspected HEAD `e211d73`. The canonical existing
direction is Sunny Office Town with floating AI robots. The duplicate muted art
bible/mood board under this checkout's `docs/art/` is explicitly superseded and
retained only as history. Its approval request is withdrawn.

The art worktree has substantial uncommitted work beyond HEAD. It was inspected,
not copied, edited, merged or committed here. Its reported browser/source checks
were not rerun here; G-A3/G-A4 and runtime WebGL metrics remain open. Fixture
financial figures in style frames do not authorize exposing ERP amounts through C.

## Resume and authorization

Use `git log --grep="Plan: world-state" --grep="Plan: program" --grep="Plan: go-platform" --format="%h %s%n%b" -n 20`
to locate these changes and the prior shared record. Continue one bounded task at
a time, checking current Git state and preserving changes owned by other work.

Outbox timeout/retry bounds and Published retention are implemented. The later
approved E/H schema assigns Go-owned Town Agent Run, Town Agent and Town Dev Task
subjects; Go emission on ERP-owned financial subjects is rejected before seq
allocation. Human presence has a separate ephemeral channel and no durable event.
Local E/G/H entry points are implemented; production activation remains separate.

No push, merge, deployment, production/pilot migration, reader provisioning,
provider purchase/call or external publication is authorized by the local commit
request. Existing user-owned `.gitignore` and `.ignore` changes are excluded from
these commits and retained locally.

## Latest approved local implementation evidence

See `2026-10-11-program-completion.md` for the current acceptance checklist and
`../../local-runtime-operations.md` for exact host entry points, limits, disabled
capabilities and the unapplied rollout proposal. Default `town serve` roles are
unchanged; the explicit new hosts never migrate an existing database automatically.

The shared contract now has 22 generated event types. Nine AI/dev types are
Go-owned, with company/System Manager visibility and no raw prompt/code/financial
or human activity fields. Projection/client tests preserve the existing financial
seq namespace, formats and actual visibility. Presence never enters replay.

Observed: 227 workspace tests, client build/typecheck, 12 fixture browser cases
in 1.0m and full root lint. The actual browser host passed its avatar/business
history/session case (47.6s) and departmental SO -> DN -> SI -> PE case (20.2s),
with Go race/leak acceptance in 79.263s; its test-site snapshot was restored.
The latest TestAgentLive after source-policy hardening passed in 8.789s and
restored the site. This agent acceptance creates a real sales-order draft;
all eight policy tools additionally pass the deterministic Port contract flow
with explicit human confirmation and no draft payment settlement.

Final Python run: 48 unit tests passed in 1.678s; 199 integration tests passed in
205.925s with one existing Chromium PDF skip and existing `limit_page_length`
deprecation warning. A fixture-ownership assertion originally expected Frappe
to produce the new Go events; it now requires every ERP producer and asserts
the Go-only fixtures were not emitted by Frappe.

Four post-claim regressions were observed failing: revoked document-read access,
changed source quantity, cancellation and changed customer. The policy now
re-reads both credentials before returning data or creating a source draft, and
stops with `SOURCE_CHANGED` when policy-relevant source facts differ. Durable
denial/source-change outcomes cannot award XP. The full agents race package
passed in 41.084s; later all-eight-tool/post-claim and PostgreSQL outcome cases
also passed. The final all-Go race checkpoint passed **597 tests in 131.029s**;
final root lint passed with 214 Biome files and 0 Go issues, and whitespace checks
passed. HEAD/branch are unchanged and no commit/push was performed.

The source art checkout and canonical Blender file were not modified. Three
character development exports were copied with hashes and existing license
provenance. G-A3/G-A4 remain open. The new RTX 3080 Ti/D3D11 observations measure
275 peak calls without characters and 294 with two humans/one AI, p95 <=16.8ms.
Mobile hardware, real connected-world GPU load and GPU texture memory remain open.

## Simulation/dashboard continuation accepted — 2026-10-12

The approved [simulation/dashboard plan](2026-10-12-town-simulation-dashboard.md)
is implemented through Tasks 0–10. This continuation reuses the existing client,
world/gateway boundaries and owned assets. It adds shared route timing and
bounded work/vehicle presentation, a desktop HUD/narrow drawer, one selection and
camera model, evidence-backed many-to-many document journeys, active workstation
dashboards and consistent replay/scope cleanup. Invoice detail's approved
read-only `against_delivery_notes` field supplies the missing SI→DN evidence.

Observed: 138 client unit tests, 33 isolated browser cases, client build/typecheck,
four affected Go race packages, real two-case `TestClientLive` acceptance and root
lint passed. A separate actual bounded ERP round had submitted SO/DN/SI/PE, invoice
outstanding `0.0`, exactly one observed dispatch, no browser business writes or
page errors, and four idle actors/no trip at completion. Both test harnesses
restored `test.localhost`; the final demo was stopped gracefully.

The rendering fixture on the same RTX 3080 Ti with three actors, two monitors,
one truck and HUD measured 270 peak calls, 368,028 triangles and active frame p95
4.3 ms over 17.25 seconds. Textures dropped 27→9 on scope loss; an idle three-actor
slice rendered six demand frames in three seconds of polling. Total GPU bytes,
maximum-population stress and mobile hardware remain unmeasured. This does not
replace G-A3/G-A4, pilot/restore or external native-model acceptance.

[Detailed evidence and limitations](../../town-simulation-dashboard-evidence-2026-10-12.md)
links the actual ERP trace, renderer metrics and review screenshots. No canonical
art edit, dependency/migration/authentication/deployment change, commit or push was
performed by this slice. Existing uncommitted work and the preceding completion
records were preserved.
