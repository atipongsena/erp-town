# World State Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans, strictly sequentially.
> The user's AGENTS.md overrides optional approval/commit/worktree/model workflows.

**Goal:** Complete C as the tested company-scoped state service used by D, E, G and H.

**Architecture:** Pure event reduction produces immutable company state. Durable
checkpoints plus raw stream replay restore state; read-only ERP reconciliation repairs
missing history. A room actor filters snapshots/patches for authenticated viewers.

**Tech Stack:** Go 1.27, PostgreSQL 18, Redis Streams, coder/websocket, protobuf, Buf.

**Spec:** `docs/superpowers/specs/2026-10-10-world-state-design.md`

## Global Constraints

- Follow the existing Foundation and Go Platform contracts and clean-room rules.
- Test only testcontainers and `test.localhost`; never `town.localhost`.
- Execute one implementation task at a time. No parallel agents or model changes.
- Do not edit generated files by hand; use pinned local generators.
- No unauthorized dependency changes beyond the approved C versions.
- No commits, pushes, deployment changes or real credential/role provisioning.
- Keep English code/docs, Thai user communication and LF endings.

## Review Focus

- An older pending entry must prevent a snapshot cursor from skipping its work.
- Restart replay must restore events already recorded in `consumed_events`.
- Company/role/private checks must apply to both snapshots and patches/removals.
- Reconnect and shutdown must not leave writers, timers or sessions leaking.
- ERP reconciliation must not reintroduce amounts or stale resolved signals.

### Task 1: Projection and durable checkpoints

**Files:** `go/internal/world/{state,projection,snapshot,snapshot_store}.go`, focused
tests and `go/internal/store/migrations/00003_world_snapshots.sql`.

**Interfaces:** `Projector.Apply(State, events.Envelope) (State,error)`;
`EncodeSnapshot(State) ([]byte,error)`; `DecodeSnapshot([]byte) (State,error)`;
`SnapshotStore.Load/List/Save`, `Snapshot{State,StreamID,TakenAt}`.

- [x] Observe failing event-reduction tests and implement immutable reduction.
- [x] Preserve independent visibility and sequence tombstones through cancellation.
- [x] Observe failing snapshot API tests, implement strict versioned codec and store.
- [x] Verify corruption, replay tombstones, concurrent save and cursor/version monotonicity.
- [x] Focused `-race` and world lint pass; see local snapshot report.

### Task 2: Shared wire protocol and pinned generators

**Files:** `packages/world-proto/{proto,src,test,scripts}`, Buf config, generated
`go/internal/worldpb`, approved Go/pnpm manifests and lockfiles.

**Interfaces:** binary `ClientFrame` with JOIN or display intent; `ServerFrame` with
snapshot/patch, room/base versions, upserts/removals and reconnect token.

- [x] Write cross-language golden binary encode/decode tests; observe missing protocol failure.
- [x] Add the approved pinned runtime/plugins, generate locally and document one command.
- [x] Verify Go/TS golden compatibility, malformed frames and deterministic regeneration.

### Task 3: Room actor and WebSocket transport

**Files:** `go/internal/world/{room,sync,websocket}.go` and focused tests.

**Interfaces:** a service publishes immutable State, serves `/town/ws`, and owns room
actors; it accepts an existing JWT verifier and no ERP writer.

- [x] Observe failing company/role/private filtering and removal tests.
- [x] Implement 10 Hz changed-entity patches, per-client version tracking, queue 256.
- [x] Verify five-second JOIN, same origin, expiry, wrong company, unsupported intents.
- [x] Verify reconnect scope/version fallback, slow writer resync and leak-free shutdown.

### Task 4: Read-only ERP projection and reconciliation

**Files:** `go/internal/erp/projection.go`, fake/ERPNext projection adapters,
`frappe-apps/erp_town/erp_town/projection_api.py`, shared contract and focused tests;
`go/internal/world/reconcile.go`.

**Interfaces:** `erp.Projection.ReadProjection(ctx,as,company)` returns only whitelisted
facts plus subject sequence fences; synthetic facts follow the C spec.

- [x] Observe failing read-only API tests; implement company-bound metadata reads and shared adapter contract.
- [x] Verify no money/unknown fields, pagination and permission failures.
- [x] Reconcile cleared overdue/stock/backlog without lowering per-facet sequence fences.
- [x] Run the focused Go and Python tests against isolated fixtures/test site.

### Task 5: Recovery and process wiring

**Files:** world recovery/runtime tests, events stream-context helper, town main,
platform config tests and local operation documentation.

**Interfaces:** recovery publishes State only after replay/rebuild completes; stream
entry ID travels in handler context without changing the existing Handler signature.

- [x] Observe failing restart/trim/live-boundary API tests and actual trim-during-replay defect.
- [x] Create group before replay boundary, gate live application, preserve pending prefix.
- [x] Persist checkpoints every 30 seconds and reconcile with read-only projection.
- [x] Wire `world` role and `/town/ws`; validate configuration without logging secrets.
- [x] Verify recovery/shutdown with real Redis/PostgreSQL testcontainers.

### Task 6: Acceptance and next program step

**Files:** `go/tools/worldload`, world acceptance tests and this progress record.

- [x] Measure 200 clients/room, stream-entry-to-client-patch p95 <250 ms.
- [x] Run required package typechecks, Go race suite, relevant integration checks and lint.
- [x] Review the final C diff, correct material findings and record observed evidence.
- [ ] Continue A–H in dependency order. Reuse A's existing work in
  `D:/ERP_Town-art-direction/` before D scene work; see `2026-10-10-art-foundation.md`.
  Prepare consequential dependency/data/security decisions for approval before executing.

## Progress

- 2026-10-10: existing readiness fixes and projection are uncommitted, verified in prior
  increments. Snapshot step complete; focused snapshot race run 13.494 s, lint 0 issues.
- User scope is all ERP Town A–H, with strictly sequential execution. C implementation
  and local acceptance are complete; real reader provisioning and pilot checks remain
  outside authorization. The entire A–H program is not complete.
- Wire tests and typecheck pass; regeneration is byte-identical; full lint passed.
  Buf's necessary transitive Go upgrades are recorded in go.mod/go.sum. Its internal
  TypeScript 5 compiler is not used for workspace typechecking; the existing root
  TypeScript 7 compiler is selected explicitly.
- Room/WS focused race suite passes (7.837 s), including the real five-second JOIN
  deadline and goleak shutdown check; world lint reports 0 issues.
- Snapshot format 2 preserves per-facet ERP fences, audience and event tombstones.
  Recovery tests reproduce prefix trimming with a real Redis hook, then verify rebuild
  before publication, replay despite durable dedupe, duplicate ID handling and pending
  checkpoint protection. Reconciliation cannot promote private/role facts.
- Local 200-client acceptance: 20 rounds, 4,000 samples, p95 89.5308 ms, max 95.7054 ms,
  race detector enabled. The test timestamps Redis entries, not business events.
- Final sequential checks: TypeScript typecheck and 73 tests pass; complete Go race
  suite with `-p 1 -count=1` passes 412 tests in 215.454 s; Python 45 unit and 189
  integration tests pass (one existing Chromium PDF skip), using `test.localhost` only.
  Existing session API emits its `limit_page_length` deprecation warning. Ruff is not
  installed in the bench Python environment; no dependency was added to run it.
- Review found process-context shutdown reported normal closure. A focused failing
  regression test observed code 1000, fixed to 1001; all WebSocket tests then pass
  with race detector (7.624 s). Full lint passed before and after the final correction.
- The user subsequently authorized local Git commits to record progress for other
  agents. See `2026-10-10-program-handoff.md` for current A–H ownership, verification
  limits and resume actions. Earlier uncommitted notes describe historical increments.
