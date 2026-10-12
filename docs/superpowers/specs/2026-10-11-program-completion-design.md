# Remaining local program completion

Date: 2026-10-11. Requested scope: A/B/C/D/E/G/H; F stays deferred.
The user explicitly approved the protected local scope below on 2026-10-11.
Implementation may proceed within that scope. The exclusions below remain in
force, including production/pilot operations, external provider calls and commits.

## Existing evidence and acceptance boundary

D's real test-site browser flow SO -> DN -> SI -> PE and C's local 200-client
acceptance passed at their recorded checkpoints. E's local run kernel has 40
race/leak tests. G's session replay is implemented in this continuation; it is
not durable historical replay or human presence. A remains owned by the art
checkout. Its scorecard still records 2.22/3 and open G-A3/G-A4 gates.

Completion means verified local implementations and test-site acceptance for
E/G/H, plus separate observed art/hardware acceptance. A pilot deployment is a
distinct operational action. Never mark those gates complete from fixture tests.

## G: session replay (authorized independent implementation)

Record only `DisplayFact` fields already shown by D: identity, document type/name,
facet/scope, department and status. Do not record raw protobuf payloads, financial
fields, credentials, actor identities or movement. Capture at most 64 distinct
versions, 1 MiB of encoded display facts and 15 minutes of local receive time.
No localStorage, IndexedDB, download, backend history or automatic playback.

Copy inputs/outputs. Prune removed identities from all prior points, including
same-version visibility refreshes. A reappearing identity does not restore its
pruned history. Clear on company change, snapshot version regression, reconnect,
logout, expiry, malformed sync or transport loss. Evicted selections return live.
An expiry timer removes history even when no new frames arrive. Failures must not
alter the independently owned current world state.

Use a native bilingual selection control and a clear historical banner. While
selected, show earlier department statuses and hide document controls/links;
deactivate the document controller before paint, cancelling pending operations
with its existing uncertain-write protection. Return live explicitly before
performing a new document action. Retained points are received states, not every
ERP event, an audit trail, evidence of settlement or a reconstruction of the past.

## E: runtime integration proposal

Use the approved Go architecture: one registry for native tools and MCP, one
policy executor, erp.Port as the only business interface and River for durable
jobs. Start with L0 read and L1 draft assistance. The initial allowlist supports
the four existing document reads and draft creation paths. No direct agent
submit/cancel/payment settlement or free-form workflow action is enabled.
Financial confirmation remains an explicit human action with ERP RBAC/workflow.
AI XP/levels are derived only from successful, audited AI tasks; never rank humans.

Bind the acting identity, API credential, company and on_behalf_of server-side.
Resolve an enabled Town Agent Identity and the current principal's actual ERP
companies; never trust those fields from a model, browser or hook. Revalidate
delegation before a run and each tool. The on_behalf_of user must be an existing
authorized user, and revocation fails closed. Credentials must never enter model
requests, tool arguments, event data, journals, traces or response payloads.

The first policy has no automatic financial confirmation allowance. Bound drafts
to 100 total positive line units, exact decimal strings, 16 tool executions per
run and 30 attempts per agent/company per minute (atomically enforced). A rate
limit or missing/unknown financial fact stops for human review. Known financial
values come from the ERP, not model-calculated or estimated totals. Keep kernel
ceilings (8 turns, 12,000 reported tokens, 64 KiB per payload, 2-minute deadline).
These limits are not approval to spend money on a provider.

Proposed direct dependencies, verified against official release pages on
2026-10-11 (pin exact versions; review all resulting transitive changes):

| Module | Version | Primary source |
| --- | --- | --- |
| github.com/anthropics/anthropic-sdk-go | v1.80.0 | https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.80.0 |
| github.com/openai/openai-go/v3 | v3.76.0 | https://github.com/openai/openai-go/releases/tag/v3.76.0 |
| github.com/modelcontextprotocol/go-sdk | v1.8.0 | https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0 |
| github.com/riverqueue/river and riverdriver/riverpgxv5 | v0.49.0 | https://github.com/riverqueue/river/releases/tag/v0.49.0 |

Provider tests use local HTTP/process fixtures, including structured tool calls,
token accounting, deadlines, no hidden tool-write retries, malformed/panicking
adapters and bounded output. Real provider calls and private-data disclosure
remain disabled pending credentials/model/budget approval. Codex uses an isolated
app-server stdio adapter with no general shell/workspace/ERP access; validate the
installed version/protocol before selecting it. Native ERP tool calls still pass
the same registry/policy. If the app-server cannot enforce this boundary, stop
that adapter rather than silently exposing unrestricted tools.

### Durable data and restart behavior

Propose additive Go-owned PostgreSQL migrations only, applied to isolated test
containers initially: agent_runs, agent_tool_calls, agent_policy_windows,
agent_progress, avatar_preferences and River's pinned PostgreSQL queue schema.
No MariaDB ERP schema or production/pilot migration is included.

Use a unique run id and unique run/call id with a canonical intent hash. Persist
claim, validated policy decision and pre-call intent before any ERP write. After
a successful acknowledgment, persist a bounded typed result (document references,
status and safe outcome codes only; no copied ERP amounts/parties) and outcome. Never
automatically repeat a write after cancellation, lost acknowledgment, crash or
lease expiry. Recovery with an unresolved intent enters needs_human; restarting
a River worker cannot redispatch it. Queue enqueue, run state and Go-owned outbox
entries share a transaction. Separate failed read/provider retries from possible
business side effects; test transaction rollback and competing workers.

Journal entries expose metadata only to authorized company users. Results and
intent hashes are protected separately, with no raw prompt/provider-error log.
Retain completed metadata at the existing 400-day Foundation boundary; never
purge unresolved writes. Clear ephemeral model context at termination.

### Event ownership

Extend the source schemas and regenerate all Go/TS/Python copies. Proposed types:
agent.run.started/completed/needs_human/failed, agent.progress.updated,
presence.avatar.updated/left and dev.task.started/updated/completed/failed.
Go alone owns Town Agent Run, Town Agent, Town Avatar and Town Dev Task subjects.
Their seq namespace never uses ERP document subjects or alters ERP seq fences.
Financial ERP events remain exclusively produced by ERP. AI/dev events use the
durable Go outbox; presence remains ephemeral with TTL and no durable history.
Visibility is explicit company plus roles/private ownership; no monetary fields,
prompt text, paths, source code, credentials or human productivity metrics.
Extend C's reducer and protocol through their generators with adversarial
visibility tests. Preserve existing document/approval/error facets and formats.

## G: avatar and durable replay proposal

Avatar consent defaults off. Only the authenticated user can opt in/out or change
their own appearance. Store consent and appearance, not human activity history.
Use an opaque presence id, coarse department/emote and a short TTL; validate
company membership on each update. No names, photo, job performance, XP or human
leaderboard. Revoke and remove live presence immediately on opt-out; exclude it
from replay and erase its preference/presence when account access is removed.

Durable replay covers business state transitions only. Authorize at query time
using current membership and visibility, cap the requested window/results, and
keep financial document reads outside replay. Do not advertise recovery beyond
available retained history. No export/share endpoint is proposed. A separate
later DuckDB/BI dependency would need approval; it is not required for the first
bounded replay view backed by existing snapshots/events.

## H: dev bridge proposal

Keep internal/devbridge unable to import ERP (existing depguard). A separate
loopback-only listener accepts a configured local bearer token and bounded
structured task events. Reject non-loopback requests, CORS/browser origins,
oversized/unknown fields and arbitrary company/identity claims. Token and mapped
company come from trusted local configuration, not the incoming JSON. Do not
install hooks or write secrets automatically.

Hook adapters accept only coarse states, opaque task id, monotonic sequence and
provider kind. Do not send prompts, outputs, filenames, repository paths, git
diffs or code. Idempotency prevents replay/sequence regression from changing a
finished task. The outbox uses Go-owned subjects. Render AI dev activity at IT,
without ERP tools or credentials. Verify Claude/Codex hook protocols from their
installed binaries/current official docs; do not invent supported hook names.

## Exact protected local scope requested

1. Add the five direct Go module entries above and update go.mod/go.sum, reviewing
   transitive changes without unrelated dependency upgrades.
2. Add the additive Go/River migrations, generated queries/protocol/event schemas,
   and test them only in isolated containers. Do not apply to existing dev/pilot
   databases until a separate operational request is approved.
3. Implement the described agent delegation/policy and authenticated avatar/
   replay endpoints, disabled unless explicitly configured. Add the separate
   loopback dev-bridge token endpoint; no secret provisioning or hook installation.
4. Extend the previously approved test.localhost snapshot/seed/restore acceptance
   with temporary test-only identities and the new local routes. Provider I/O uses
   only loopback fixtures; external calls remain off.

Approval does not include production/pilot deployment, permanent credentials,
paid provider calls, private uploads, commits, pushes or changes to F.

## Sequential acceptance after approval

- Finish E's registry/policy, durable claim/audit/River and provider/MCP adapters;
  verify known/uncertain writes, disabled identities, company isolation, limits,
  deadlines, shutdown and restart against local fixtures and test ERP.
- Finish G's consent/presence rendering and bounded authorized business replay;
  verify opt-out/expiry/revocation and prohibit human tracking/financial leaks.
- Finish H's loopback ingestion and IT rendering with duplicate/order/privacy
  tests, plus a depguard proof that it cannot call ERP.
- Re-run the real SO -> DN -> SI -> PE browser slice with the changes and restore
  test.localhost. Run required unit/integration/race/leak/lint/build checks.
- Inspect A's reviewed exports and obtain the actual human design/comprehension
  review; measure D on named hardware. These remain observed acceptance gates,
  not metrics fabricated from SwiftShader or still images.
- Prepare the local reader/routing and pilot rollout proposal only after code
  acceptance; provision/deploy only with its separate authorization.
