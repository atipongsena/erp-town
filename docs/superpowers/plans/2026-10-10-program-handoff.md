# ERP Town A–H — cross-agent handoff

Recorded 2026-10-10 on `feat/go-platform` by Codex. The user authorized local Git
commits to make progress discoverable by other agents. The user wants the entire
A–H program completed, with strictly sequential work and no parallel agents.
The program is not complete. Read this record alongside the implementation plans
and Git history; do not infer additional operational authorization from it.

Code checkpoint: `9666837` (`feat(world): implement recoverable world state and websocket sync`).

## Current scope and ownership

| Part | State in this checkout | Next action |
| --- | --- | --- |
| A: art | Existing work is owned by another chat in `D:/ERP_Town-art-direction/`, branch `feat/art-direction`; G-A3/G-A4 remain open in the inspected scorecard. | Reuse its reviewed tokens, assets and references; do not recreate art or overwrite its uncommitted work. |
| B: foundation | Prior foundation and Go platform work is recorded in Git. This change adds readiness fixes on top. | Retain existing contracts and deferred pilot follow-ups. |
| C: world state | Implementation and local acceptance complete; live reader provisioning and pilot verification remain outstanding. | Follow `docs/world-state-operations.md` when authorized to provision a reader and run live checks. |
| D: town client | No live 3D client implemented by this work. | Integrate reviewed A exports with C's wire/visibility contracts; obtain approval for concrete new dependencies before installation. |
| E: agents | Runtime/provider implementation remains. Go outbox subject ordering is fixed. | Resolve Go-owned subject allowlist; implement bounded publishing/retention and the approved agent/policy design. |
| F: PromptPay/LINE | Not implemented by this work. | Continue from the program overview with explicit external/credential boundaries. |
| G: avatar/replay | Not implemented by this work; depends on C/D. | Preserve opt-in and authorization requirements. |
| H: dev-agent bridge | Not implemented by this work; depends on C. | Keep the bridge without ERP permissions. |

## Implementation evidence

The world implementation includes immutable company projections, strict format-2
PostgreSQL snapshots, bounded raw Redis recovery independent of durable dedupe,
read-only ERP reconciliation, per-company rooms and protobuf WebSocket sync.
JWT company/role/private visibility is enforced server-side. Resume tokens are
short-lived, single-use and bound to the same identity/company/roles/version.

Recovery guards pending entries and trim races; one advisory-lock owner runs each
stream/group. Room/WebSocket cancellation is checked with leak tests. The Go world
reader uses a separate read-only credential; no real reader role/user/key has been
created. Retained snapshots cannot recover old approval/error history once both
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

## Art coordination

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

Outstanding backend items include per-XADD timeout/retry bounds and published
outbox retention. E's subject ownership design remains a separate decision; the
ordering fix does not authorize Go emission on ERP-owned subjects.

No push, merge, deployment, production/pilot migration, reader provisioning,
provider purchase/call or external publication is authorized by the local commit
request. Existing user-owned `.gitignore` and `.ignore` changes are excluded from
these commits and retained locally.
