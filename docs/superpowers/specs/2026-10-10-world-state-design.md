# World State (C)

Approved scope: the 2026-10-10 C proposal in the Go Platform follow-ups. The user
also authorized continuing the entire ERP Town A–H program, then requested strictly
sequential execution to conserve usage. This document records the approved C contract.

## Contract

- One immutable event projection and one room actor per company. ERPNext remains the
  system of record. World accepts only `erp.Projection`, never ERP write interfaces.
- Existing validated event envelopes reduce independently by subject sequence;
  removed signals retain sequence tombstones. Public facts never contain money.
- Local PostgreSQL `world_snapshots(company text PRIMARY KEY, stream_id text NOT NULL,
  state bytea NOT NULL, taken_at timestamptz NOT NULL)` stores one versioned internal
  checkpoint per company every 30 seconds. A checkpoint covers a fully applied stream
  prefix, including entries that have been deliberately rejected under event rules.
- Recovery replays raw Redis entries after the checkpoint without consulting durable
  consumer dedupe. Missing or trimmed checkpoints rebuild through read-only ERP
  projection. Establish the live group before capturing the replay boundary; hold
  live application until recovery completes. Never skip unresolved pending entries.
- ERP reconciliation reads open SO/DN, overdue unpaid SI and below-reorder stock.
  Its explicit metadata whitelist excludes amounts and personal display names.
  Per-facet sequence fences prevent stale events from resurrecting resolved facts
  without consuming unrelated approval/error sequence numbers. Internal snapshot
  format 2 persists these fences separately from event-delivery tombstones.
- `/town/ws` uses `github.com/coder/websocket` v1.8.15 and existing EdDSA JWTs.
  Same-origin browser connections, a five-second authenticated JOIN deadline,
  JWT company membership and token expiry are enforced before any room data.
- Public is company-scoped. Roles require an intersection with JWT roles. Private
  requires actor kind `user` and actor id `user.` plus the JWT subject;
  `on_behalf_of` does not grant visibility. Unknown audiences fail closed.
- Rooms publish at 10 Hz, only changed entity upserts and removals. Client state
  tracks visible entity versions. Permission loss removes previously visible data.
  Each connection has a dedicated writer and a queue of 256 frames; overflow closes
  with a resync reason. Client messages contain display intents only, never ERP writes.
- Random reconnect tokens expire after 30 seconds and bind the authenticated user,
  company and visibility scope. Resume only from a proven client version; otherwise
  send a freshly filtered snapshot. Role changes require a new filtered snapshot.
- Protobuf source lives in `packages/world-proto`; local Buf generation produces Go
  and TypeScript without uploading schemas. Runtime/protoc-gen-go v1.36.12,
  Buf CLI v1.73.0, protobuf-es runtime/plugin v2.16.0 are pinned.
- Acceptance: race-free shutdown, recovery/trim/live-boundary/visibility tests and
  200 clients in one room, with stream-entry-to-client-patch p95 below 250 ms.

## Recovery and local operation

Raw replay dedupes event IDs in memory, independently from the durable consumer
ledger. A retained first entry and checkpoint cursors anchor each unpublished scan;
prefix trimming invalidates it and forces an ERP rebuild, with three bounded attempts.
A regressed Redis cursor fails startup. A trimmed rebuild restores current whitelisted
metadata, not historic approvals/errors that no longer exist in retained history.

The `world` process role uses `TOWN_WORLD_API_KEY` / `TOWN_WORLD_API_SECRET` for a
dedicated `Town Projection Reader` user, plus the existing JWT signing key for
verification. Configuration validates presence and never prints key values. It
creates no ERP roles/users/credentials. Only one owner runs per stream/group, held
by a PostgreSQL session advisory lock. See `docs/world-state-operations.md`.

## Synthetic reconciliation facts

Reconciliation uses internal `world.projection.document`, `.overdue` and `.stock`
facts, not new event-bus types. Metadata: actor `system/system.town`, public visibility,
empty event/correlation IDs, RFC3339 time, nonnegative sequence covered by its subject
fence. Data is strictly one of `{status}`, `{due_date,days_overdue}` or
`{warehouse,fill_ratio}`. Stock ratio is between 0 and 1 inclusive because rounding
can produce 1 for a quantity just below the threshold. Snapshot validation checks
these separately from event-derived entities.

## Authorization boundary

The approved dependencies, local C migration and WS authorization design may be
implemented and tested locally. No real credentials or role grants, pilot/production
migrations, deployment changes, external writes, commits or pushes are authorized.
Preserve existing user-owned `.gitignore` and `.ignore` changes.
