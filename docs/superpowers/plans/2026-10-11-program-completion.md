# Program completion, sequential continuation

Spec: `../specs/2026-10-11-program-completion-design.md`.
Scope: A/B/C/D/E/G/H; F remains deferred. No commits, new dependencies,
migrations, credential provisioning or external writes are authorized by this
plan itself. Preserve both checkouts' uncommitted work and user-owned ignore files.

## 1. Independent G session replay

- [x] Add failing retention/privacy/ownership tests and implement bounded history.
- [x] Integrate with TownSession; clear with connection scope and expire by timer.
- [x] Add bilingual controls, historical banner and read-only document boundary.
- [x] Test mobile/reduced-motion history, live updates, removal and expiry in browser.
- [x] Run all workspace unit tests, client build/typecheck and root lint.
- [x] Finish the real test-site replay and unchanged sales-to-cash acceptance run.

Observed initial replay tests: 12 failures against the empty implementation;
18 replay regressions and the integrated timer regression then passed. The timer
test also checks that disconnect releases the scheduled timer. All 168 workspace
unit tests passed (54 client), build/typecheck and full root lint passed. Ten
fixture browser cases passed in 46.9 s. The final CSS pass changes the historical
indicator to amber and makes the return-live button visually active; the rebuilt
client passed both real test-site browser cases (43.1s and 26.3s); Go race/leak
acceptance passed in 81.424s and the site's DB was restored. The first case checks
the actual pre-order snapshot, read-only history and return to live state.
No provider/ERP writes are initiated by replay. The later incomplete-JS-limits
regression was observed failing, fixed and passed with all 169 workspace tests
(55 client); build/typecheck and root lint passed again.
All eleven fixture browser cases subsequently passed in 1.3m, including the new
motion-after-idle regression, reduced motion, no WebGL, bilingual replay and the
unchanged document cases. The live ERP run preceded the later timing/retention
guard changes; no new live run is claimed for those narrow changes.

Independent D observation: the RTX 3080 Ti/D3D11 probe exposed a demand-idle delta
jump, now capped at 50ms in roof/camera animation. A browser regression passed;
six hardware transitions render 64-68 frames with p95 <=16.734ms. Peak draws are
413, above the 300 budget at that checkpoint. The later glass integration closes
that draw-call finding; broader hardware acceptance remains open. See
`../../client-renderer-evidence-2026-10-11.md` for the reproducible report.

## 2. Protected local implementation proposal

- [x] Verify exact Anthropic/OpenAI/MCP/River release versions from primary sources.
- [x] Specify initial L0/L1 policy, no automatic financial confirmation, delegated
  identities, company scope, exact quantities and rate limits.
- [x] Specify Go-owned subject namespaces, durable claim/audit/restart behavior,
  explicit avatar consent and ERP-free loopback dev bridge.
- [x] List the precise dependency/manifest, additive test-container migration,
  authentication/endpoint and temporary test-identity changes for approval.
- [x] Receive explicit approval for the protected local scope in the spec (2026-10-11).

Protected local implementation is approved. Apply migrations only to isolated
test containers; identities remain temporary and test-only. No paid model calls,
private-data disclosure, permanent credentials or pilot operations are in scope.

## 3. E, after approval

- [x] Implement one typed registry and policy executor using erp.Port.
- [x] Add approved module pins; inspect each transitive diff.
- [x] Add durable run/call/audit/rate-window storage and pinned River schema.
- [x] Prove transactional enqueue/outbox and exactly-once claim; unresolved writes
  always need human reconciliation rather than automatic redispatch.
- [x] Add native Anthropic/OpenAI local contract adapters with no hidden retries;
  inspect Codex's installed protocol and leave it disabled while its approved
  ERP-only capability boundary remains unproven (the spec's conditional stop).
- [x] Expose the same registry through authenticated MCP; no policy bypass.
- [x] Add generated agent events, AI-only progress and world/client integration.
- [x] Run test-site agent acceptance with temporary scoped identities and restore.

## 4. G, after approval

- [x] Add authenticated self-only consent/appearance preferences (default off).
- [x] Add ephemeral company-bound presence with opt-out/expiry/revocation removal.
- [x] Integrate inspected development human/AI assets without changing A's source;
  retain their open named human review rather than implying final art approval.
- [x] Add bounded server historical business replay, authorized at query time.
- [x] Verify replay contains no presence tracking, financial leak or export path.
- [x] Verify human avatars have no XP/levels or leaderboard.

## 5. H, after approval

- [x] Verify installed hook contracts; map only safe structured task metadata.
- [x] Implement separately bound loopback token endpoint with strict size/schema,
  duplicate/order checks and trusted company/identity configuration.
- [x] Add Go-owned dev events/outbox and IT projection/rendering.
- [x] Verify no prompt/code/path disclosure and no ERP access; run depguard.

## 6. Final acceptance and separate operational gates

- [x] Full relevant local unit/integration/race/leak/lint/build/browser checks;
  preserve the existing Chromium PDF skip and separate operational/art gates.
- [x] Re-run SO -> DN -> SI -> PE and E/G/H local acceptance; restore test.localhost.
- [ ] Reconcile A's G-A3/G-A4 with actual named human design/comprehension reviews.
- [x] Obtain a named desktop hardware frame-pacing/draw-call observation.
- [x] Remove the unnecessary glass transmission pass and measure town plus
  two humans/one AI against the unchanged 300-draw/750k-triangle budget.
- [ ] Measure mobile/real connected load and texture/memory/postprocessing
  before certifying D performance acceptance.
- [x] Prepare the concrete local host/reader/routing/pilot proposal in
  `../../local-runtime-operations.md` without applying operations.
- [ ] Obtain separate authorization for the actual configured rollout target.
- [ ] Apply only separately authorized operations and observe their acceptance.

Until all required evidence exists, report the program as incomplete. Do not
convert implementation/fixture success into art, hardware or pilot acceptance.

## Latest local implementation evidence (2026-10-11)

The five approved direct Go modules are pinned exactly. `go mod tidy` adds their
required SDK/River transitives; the only existing version increase is
`golang.org/x/net v0.59.0 -> v0.60.0`, required by the new graph. No unrelated
upgrade was made. Default roles/production credentials remain unchanged.

One registry supplies native tools and MCP, with actual dual-credential company,
doctype/master-reference/source-document checks before claim and before effect.
PostgreSQL journals/rate windows and River enforce transactional ownership;
denied or unresolved calls cannot award AI progress. A real local River worker
poller, competing claims, lost-worker restart, rollback and graceful shutdown
passed. Real model services remain disabled; native contracts use loopback
fixtures. Codex remains rejected because its restricted capability boundary
cannot yet be demonstrated with installed 0.153.4.

Presence uses separate ephemeral HTTP DTOs instead of proposed durable
`presence.avatar.*` schemas, so human presence cannot enter replay/outbox.
Current account/company denial removes observed preference/live presence;
offline dormant preferences are revalidated on next authenticated access.
The host does not have an offline account-removal feed. History is bounded
retained business metadata, not a historical balance recovery service.

Observed final checkpoints: 227 workspace tests; client build/typecheck; 12
fixture browser cases in 1.0m; latest root lint (214 Biome files, 0 Go issues). The earlier full
Go race suite passed 584 tests in 130.369s. Later targeted race regressions passed:
avatar account/transport distinction (16.891s), Codex rejection (1.193s), complete
devbridge including real HTTP/concurrent hooks/shutdown (16.060s), and history
expiry/private visibility/result caps (2.587s). Goleak runs in applicable packages.

The rebuilt real browser host passed its session/avatar/history case (47.6s)
and complete departmental SO -> DN -> SI -> PE case (20.2s); TestClientLive passed
with race/leak checks in 79.263s. TestAgentLive passed in 9.546s. Both suites
restored test.localhost. Initial browser failures exposed asynchronous consent
test timing and unrestricted seed-company access; the tests now await actual
acknowledgments and use temporary real Company User Permissions, never fake
permission denials.

The first full Python run found one producer fixture-ownership assertion that
pre-dated Go-owned schemas. The updated test still requires every ERP fixture,
classifies each Go fixture's actual owner and asserts Frappe did not produce it.
Its focused 2 unit/23 integration tests passed. The full final Python rerun then
passed 48 unit tests in 1.678s and 199 integration tests in 205.925s, with one
existing Chromium PDF skip and the existing `limit_page_length` warning.

Final source-policy review added four failing post-claim regressions (read
visibility, quantity, cancellation and customer). Both credentials now re-read
the source before effect and compare policy-relevant facts; changed sources stop
with fixed `SOURCE_CHANGED`, no effect/XP. The full agents race package passed
in 41.084s. The all-eight-tool/human-confirmation and post-claim cases passed in
1.173s; PostgreSQL denial/source-change progress protection passed in 9.431s.
The final TestAgentLive after this hardening passed in 8.789s and restored the
test site. The final full-Go race run passed **597 tests in 131.029s**, including
the source-policy and durable outcome changes. The final root lint passed with
214 Biome files and 0 Go issues; `git diff --check` passed. No commits were made.

Hardware: RTX 3080 Ti/D3D11, 275 peak draws/413,228 triangles disconnected;
294/422,208 with two actual human GLBs and one AI GLB on local fixtures.
p95 active frame interval <=16.8ms across six transitions. See the two raw
hardware reports; real ERP load/mobile/GPU timer/texture memory remain unobserved.
No canonical Blender source was edited; named art/comprehension gates remain open.

## Local connection continuation (2026-10-11)

- [x] Diagnose the ordinary workspace: Vite returned SPA HTML for API requests;
  no correct Town gateway was receiving them. ERP itself was running.
- [x] Add server-only loopback development proxy routing for session cookies,
  ERP site selection, login/Desk/assets and binary WebSocket upgrades; preserve
  Host/Origin, strip browser-supplied site selection at the gateway.
- [x] Add bilingual login/return connection and distinguish an authenticated
  ERP session without an enabled world host; disconnected counts stay `—`.
- [x] Wire opt-in avatar/history and its presence lifecycle into ordinary
  `town serve`, checking the provisioned schema without migrating it.
- [x] Add a connection-only launcher that selects free ports, keeps secrets out
  of Vite and cannot start database roles or provision an account.
- [x] Run 75 client unit tests, 2 launcher tests, 14 fixture browser cases,
  build/typecheck, root lint (218 Biome files, 0 Go issues), and focused Go
  command/gateway/platform race/leak checks. No full Go/Python rerun is claimed.
- [x] Run actual ERP browser acceptance **through Vite**: session, binary
  snapshot/patch, avatar/history/replay/expiry and SO -> DN -> SI -> PE; final
  TestClientLive passed in 76.10s (Go package 77.270s), 2 browser cases passed in
  1.1m; test.localhost was restored. Isolate Vite dependency caches and observe
  only `/town/ws`, not the unrelated text HMR protocol. Earlier failed attempts
  also restored the test site; editing a live Vite config during acceptance
  caused a reload, so the final run used stable source/configuration.
- [x] Observe actual ordinary-site login route via the running local gateway;
  leave a reviewable workspace at http://127.0.0.1:5174/?lang=th. This is a
  gateway-only workspace, not an activated world on the ordinary ERP site.
- [x] Approve/provision the dedicated ACME-TH projection reader and local keys.
- [x] Approve backup/restore and migrations 00003–00005 for the existing `town`
  PostgreSQL database, then activate and observe local world/experience.

The approved local activation now records migrations through 00005 and uses its
dedicated read-only credential. The AI run launch UI,
real provider binding, uncertain-write reconciliation and actual hook-host
integration remain separate development work; this connection continuation does
not mark them complete. Details and the concrete local activation proposal are
in `../../local-runtime-operations.md`. No Git commit/push was made.

## Local world activation evidence (2026-10-11)

- [x] Restore the custom-format pre-activation backup into isolated PostgreSQL 18;
  verify all five original public-table row counts and save a local checksum.
- [x] Provision only the dedicated company-restricted System User; no welcome
  email, no credential rotation or human/agent permission changes. Regression
  covers Frappe permission defaults, repeat provisioning and broader-role denial.
- [x] Apply approved Go migrations 00003–00005; keep River and paid workers off.
- [x] Add explicit launcher world mode with schema-only startup checks and no
  browser secrets. Keep gateway-only mode unchanged.
- [x] Restrict world recovery/live rooms to actual reader company codes; preserve
  old stream entries instead of rewriting company names or widening reader scope.
- [x] Observe ordinary ERP login, real ACME-TH snapshot/counts, bilingual UI and
  matching ERP/Town document permission denial. No transactions were generated.
- [x] Fix narrow-window framing and verify a full-width town above its controls.
- [x] Verify 3 launcher checks; 3 reader unit/1 database regression on test.localhost;
  focused Go race suites for store (26.318s), world (50.074s), cmd/town (19.228s),
  platform (1.515s); 15 isolated fixture browser cases (1.1m); client build/typecheck;
  root lint (218 Biome files, 0 Go issues). No full Go/Python rerun is claimed here.

The observed running workspace is http://127.0.0.1:5173/?lang=th. Start it with
`node scripts/dev-workspace.ts --world`. Evidence screenshot is
`../../town-client-dev-connected-2026-10-11.png`. Canonical Blender/art sources were
not edited; mobile hardware and named art/comprehension acceptance remain open.
This closes the local ERP connection gate, not the remaining AI host/management,
uncertain-write reconciliation, hook installation or pilot acceptance gates.

## Read-only AI work tracking (2026-10-11)

- [x] Expose bounded, company-scoped agent run metadata with fresh human System
  Manager authority, consistent with existing event visibility. No credential,
  prompt, delegator, raw error or tool-result disclosure; no enqueue/retry or
  reconciliation write endpoint.
- [x] Add a natural Thai/English status panel at IT; show uncertain-outcome review
  guidance, empty/access/error states, truncate at 25, and clear on live-scope exit.
- [x] Generate SQLc from the source query; no schema or dependency changes.
- [x] Verify read-only/company/role/revocation/privacy regressions, 77 client unit
  tests, 16 isolated fixture browser cases (1.1m), client build/typecheck and
  complete agents/gateway/cmd race suites (46.277s/1.866s/18.474s).
- [x] Observe the ordinary dev host: the sales identity receives 403 for AI
  metadata; a temporary existing Administrator session receives 200 with zero
  real runs, then logs out. No synthetic runs or ERP transactions were created.
- [x] Final focused authentication/transport error regression (7.002s), client
  metadata privacy regressions, root lint (222 Biome files, 0 Go issues) and
  `git diff --check`. No Git commit/push or new dependency was made.

Agent work can now be inspected from the client, but model selection/budget and
the real worker's server-owned credentials are still absent. Automatic financial
confirmation and redispatch after an uncertain write remain intentionally absent.
Actual hook installation and named art/mobile hardware acceptance remain open.
