# Town client implementation plan

**Goal:** Connect the existing Sunny Office Town assets to C's filtered state and
the ERP gateway, with accessible Thai/English controls and measured rendering.

**Architecture:** C owns authoritative room facts. A pure client protocol state
consumer handles snapshots, patches and resync, followed by an in-memory session
transport. The R3F client maps approved facts to A's existing assets;
document commands remain in the gateway. Implement sequentially; no subagents.

**Tech Stack:** protobuf-es/TypeScript/Vitest and the 13 renderer dependencies
approved on 2026-10-10; React/R3F, Zustand, Motion and local Vite builds.

**Spec:** `docs/superpowers/specs/2026-10-10-town-client-design.md`

## Global constraints

- F is deferred; ordinary Payment Entry stays in the ERP slice.
- Preserve A's existing work and both checkouts' unrelated local changes.
- No generated proto edits, dependency/manifest changes, credentials, migrations,
  external writes or deployment without the applicable explicit authorization.
- No live ERP amount inferred from world facts or offline fixtures.
- Keep `uint64` as `bigint`; company/visibility changes require fresh state.
- TDD for meaningful behavior; appropriate local checks before progress claims.
- No commits in this increment without authorization in this chat.

## Review focus

- Coalesced version jumps and equal-version resume must not be mistaken for gaps.
- A changed audience must remove old private data on snapshot or explicit removal.
- Late frames, mutation aliases and same Item in different warehouses cannot leak
  or corrupt the selected company's state.
- Disconnect/resync must never silently display stale state as current ERP facts.
- Keyboard/mobile fallback and reduced motion cannot depend on a WebGL click.

### Task 1: Client-side protocol state

**Files:** Create `packages/world-proto/src/client-state.ts` and
`packages/world-proto/test/client-state.test.ts`; update the package's existing
`src/index.ts` exports and README. No manifest or wire format changes.

**Interfaces:** `WorldClientState(company)` accepts `ServerFrame` or binary bytes,
returns cloned entities and a memory-only reconnect cursor, and reports resync
on invalid input. `reset()` clears all data and any cursor.

- [x] Observe failing snapshot/patch, visibility-removal, bigint, coalesced/resume,
  invalid/company-mismatch and mutation-alias tests.
- [x] Implement atomic validation/application with all four key dimensions.
- [x] Run focused tests, existing protocol goldens, package typecheck and lint.

### Task 2: Session transport

Implement an injectable transport around the browser WebSocket, with in-memory
JOIN, safe company/session resets, stale-connection guards and bounded reconnect.
Test against the actual C wire contract before a live reader is provisioned.
The original increment did not authorize authentication or credential changes;
the test-only scope approved on 2026-10-11 is recorded under Task 4.

- [x] Observe failing session, binary JOIN, resume/gap, policy-close, timeout and
  display-intent tests before implementing the corresponding behavior.
- [x] Implement `WorldConnection` and a token-free owned view, hide stale facts,
  ignore late callbacks and clear all timers on explicit disconnect.
- [x] Bound opening to 15 s, JOIN acknowledgement to 5 s and reconnect delay to
  250 ms through a 5 s cap. Mirror C's two display intents and UTF-8 target bound.
- [x] Verify the native WebSocket factory's TypeScript compatibility.
- [x] Observe a browser/Go network handshake and live, permitted ERP state on
  `test.localhost` through the approved snapshot/restore acceptance harness.

Observed 2026-10-10: all 42 world-proto tests passed (24 state, 16 lifecycle, two
existing protocol goldens), package typecheck passed, and full `pnpm lint` passed
including Go lint. No generated files, manifests, lockfile or credentials changed.

### Task 3: First real 3D scene

Obtain approval for the exact dependency proposal in the spec. Reconcile the A
asset integration point; build the client package, orthographic slice, roof
selection, object-list fallback, localization and reduced motion. Do not claim
the open A gates passed. Test loading/missing assets and actual renderer behavior.

The user explicitly approved the 13-package proposal and related manifest/lockfile
changes on 2026-10-10. All exact versions were installed; the existing world-proto
package is linked through the workspace. That approval does not provision credentials.

- [x] Build `packages/town-client` using React/R3F, Zustand and Motion.
- [x] Integrate a hash-recorded development snapshot of A's eight buildings, actual
  static environment, transforms, tokens and local licensed fonts/decoders.
- [x] Render orthographic whole-town/department views, four existing roof clips,
  bilingual copy, keyboard/mobile object selection and reduced motion.
- [x] Handle missing models and devices without WebGL while preserving the list.
- [x] Wire the same-origin session controller and strict display-only fact mapper;
  verify browser binary snapshot/expiry through fixture routes without real credentials.
- [x] Observe 138 workspace unit tests, both package typechecks, full lint and build
  passing; all six browser cases passed against development and production output.
- [x] Observe actual browser/Go/ERP snapshot, event update and expiry acceptance.
- [ ] Observe hardware performance; SwiftShader measurements do not satisfy it.

Environment compressed from 7,672,772 to 764,804 bytes; original saved Blender source
remained byte-identical. Initial render: 219,650 triangles/241 calls. Largest built
chunk about 436 kB. G-A3/G-A4 remain open; no full D/program/pilot completion claim.

### Task 4: Authorized ERP actions and acceptance

Connect document inspection and commands to the existing gateway, preserving
RBAC/audit and Classic alternatives. Test the real sales-to-cash slice on the
isolated test site once its operational prerequisites are authorized. Record
actual render/connection measurements, remaining art evidence and pilot limits.

Approved local acceptance scope, 2026-10-11: prepare a dedicated projection-reader user
and test-only keys on `test.localhost`, an ephemeral JWT issuer for the Go test
instance, and same-origin local client/gateway routing. Keep `town.localhost`,
pilot credentials/config and production data unchanged. Site seed/restore must use
the existing isolated integration harness. The user's continuation authorized
this bounded proposal; development/pilot provisioning remains a separate approval.

- [x] Add a repeatable `node tests/integration/client-acceptance.ts` runner using
  the existing `prepareTestSite`/`restoreTestSite` lifecycle.
- [x] Verify the dedicated reader against real ERP permissions, with no write
  access; run the actual Go runtime and browser against an isolated PostgreSQL
  schema, test Redis stream and ephemeral 30-second JWT issuer.
- [x] Observe the native binary snapshot, real gateway Sales Order creation and
  submission, subsequent ERP/outbox/Redis/world update, Classic document route,
  Thai/English status, explicit disconnect and real JWT expiry clearing facts.
- [x] Correct ERP work statuses (`To Deliver and Bill`, `To Deliver`, `To Bill`)
  without inferring invoice settlement; use ERP v16's canonical `/desk` links.
- [x] Implement bilingual document inspection/action UI and complete the
  real browser sales-to-cash workflow with Sales, Stock and Accounts sessions.
- [x] Add typed delivery/invoice/payment gateway reads and draft creation,
  with company/source/status/date validation and existing session/CSRF guards.
- [x] Keep exact server decimal strings, distinguish approval from submission,
  serialize commands, preserve uncertain writes across reconnects, and clear
  document details/ignore late responses on connection/company changes.
- [x] Verify manual document opening, keyboard/mobile/no-WebGL use and explicit
  draft review/confirmation, plus ambiguous-write UI and controller regressions.

Observed 2026-10-11: the live acceptance and Go race/leak checks passed and the
test site's database was restored. The six existing browser cases and 139 workspace
unit tests passed. Test helper guards passed (three unit tests), and the five existing
testing-helper integration tests passed. The built browser runs with SwiftShader;
it does not satisfy hardware/pilot or remaining art acceptance. JWT/reader/admin
keys are kept out of persisted traces, screenshots and browser subprocess secrets.
Normal preview on port 5173 stays disconnected; only the temporary test server
hosts the same-origin ERP routes. These changes are uncommitted.

Final rerun: live browser case passed in 40.4 s; Go `-race` acceptance and leak
checks passed in 51.920 s, then site restoration completed. The reader was denied
access to the actual ETT test company, not just an unknown company code. Full
root lint, integration-tag Go lint, browser-test TypeScript checking and production
build passed. Git remains on `feat/go-platform` at `b719ca5`; original `.gitignore`
and `.ignore` work is preserved. The source `town.blend` hash remains unchanged.

Document desk continuation, 2026-10-11: the UI and gateway now support draft/review/
confirm for SO -> DN -> SI -> PE. All ERP amounts are read separately with caller
permissions; world sync still contains no monetary fields. Fulfillment creates
reject an unreadable selected company, a foreign source company or an unsubmitted
source before writing. ERP, rather than department labels, defines role permissions:
the real standard Sales User can create a Delivery Note, while the dedicated
projection reader cannot create any of the four document types. An anonymous
delivery request is denied.

Observed: both real browser cases passed (40.2 s connection/event/expiry and
28.8 s document workflow); Go race/leak acceptance passed in 81.221 s and the test
database was restored. The full UI workflow verified referenced order/invoice,
server amounts, no invoice settlement from a payment draft, and exact decimal
zero outstanding after payment submission. ERP returns zero as `0.0`, which
remains a string and is compared directly with the displayed value.

Eight fixture browser cases passed, including the two document cases. All ten
document controller regressions passed; the client has 35 unit tests (149 across
the workspace). The approval race regression was observed failing, then fixed:
a fresh Submitted ERP document supersedes an earlier pending-workflow acknowledgement.
Production build/typecheck, root lint and integration-tag Go lint passed; gateway
tests passed with `-race`. No dependency changes, generated edits, production/
pilot changes, commits or external writes were made in this document increment.
Manual code/number entry remains the initial UI; partial operations, returns and
cancellation use Classic ERP. Hardware performance and G-A3/G-A4 remain open.

Final post-fix acceptance: both live cases passed again (40.5 s and 28.6 s),
Go race/leak acceptance passed in 81.245 s, and test-site restoration completed.
All 149 workspace unit tests and full root lint passed. The final payment
screenshot shows the exact ERP outstanding amount `0.0` beside the selected Bank.
Only the footer permission copy changed afterward; it was rebuilt/typechecked.

### Task 5: Remaining program

Continue E, G and H sequentially from their program contracts after D's needed
interfaces are concrete. Prepare their consequential policy/data/dependency
decisions for approval. Keep F deferred and do not imply pilot deployment.
