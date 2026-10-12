# Local runtime integration and operational proposal

Date: 2026-10-11. Scope: approved local E/G/H implementation and the separately
approved ACME-TH local world activation. PromptPay and LINE (F) remain deferred.
Pilot deployment, paid models and hook installation remain outside this activation.

## Current entry points

The default `town serve` roles remain `gateway,outbox,world`. Agent workers and
the dev bridge remain opt-in Go host entry points, exercised by isolated-container and
`test.localhost` acceptance hosts; there is no public enqueue/retry/reconciliation endpoint or
automatic credential/model selection. Starting the normal client does not start
a paid AI run or enable the dev bridge.

| Capability | Explicit host integration | Required scope |
| --- | --- | --- |
| Agent queue | `agents.NewWorkerQueue(pool, factory)`, `Start(ctx)`, bounded `Stop(ctx)` | Server-owned run factory and provisioned Go/River schema |
| Native provider | `agents.NewLocalProvider(kind, config)` | `anthropic` or `openai`, literal loopback HTTP, fixtures only |
| Internal MCP | `agents.NewMCPHandler(resolver)` on an explicitly chosen internal route | Fresh host authentication resolving one server-bound executor/run |
| Human avatars/history | `town serve` with `TOWN_EXPERIENCE_ENABLED=true`, or `gateway.RegisterExperience(...)` on a custom host | Provisioned avatar schema, fresh human ERP session and retained business stream |
| Dev bridge | `devbridge.New(pool, token, mapping)`, `devbridge.Serve(ctx, "127.0.0.1:<port>", bridge)` | Separate listener, configured token and trusted company/agent mapping |

Application constructors assume explicitly provisioned schemas. Migrations
00003–00005 now also exist in the approved local `town` database, after verified
backup/restore. River's pinned migrations are
also invoked explicitly in isolated queue tests, never by ordinary startup.

### Local browser connection

The Vite development server now proxies `/town/api/*` and `/town/ws` to the Go
gateway and ERP login/Desk/assets to the selected ERP site. Gateway upgrades
preserve Host and Origin together; ERP site selection comes from the server's
configuration, not a browser header. No CORS bypass or browser API key is used.
Server-only variables are `TOWN_DEV_GATEWAY_URL`, `TOWN_DEV_ERP_URL`,
`TOWN_DEV_ERP_SITE` and optional `TOWN_DEV_CACHE_DIR`. Proxy targets must be HTTP
loopback origins without credentials. Defaults remain ports 8090/8080 and
`town.localhost`. Built files still require the operator's same-origin routing;
these Vite proxies are for development, not deployment configuration.

The client provides an ERP login link with a local return URL. Returning with
`connect=1` requests a connection once and immediately removes the marker from
the address. An authenticated gateway reports its configured `world_enabled`
capability, allowing the client to distinguish a missing world service from a
missing ERP session. Disconnected departments continue to show `—`, never a
misleading count of zero.

For a connection-only workspace, run `node scripts/dev-workspace.ts`. It checks
the running ERP, builds the Go binary, chooses available loopback ports, and
launches gateway plus Vite with matching routes. It uses the already configured
JWT seed, sends no secrets to Vite, provisions no accounts, and starts only
`gateway`: no database is opened or migrated. World data remains unavailable
until separately approved provisioning. Existing programs on occupied ports are
left running. Stop with Ctrl+C.

For the provisioned local world, run `node scripts/dev-workspace.ts --world`.
This enables gateway/outbox/world plus experience, using the dedicated reader
from the ignored local environment. It passes `--require-provisioned-schema`:
world/outbox check every embedded migration version without applying one. A
missing schema stops startup; newly added migrations require explicit operation.
JWT, database and reader secrets go only to Go, never to the frontend process.
Current workspace: http://127.0.0.1:5173/?lang=th; gateway: port 8090.

`TOWN_EXPERIENCE_ENABLED=true` now wires authenticated avatar/history routes and
the presence sweeper into ordinary `town serve`; it requires `gateway` and a
previously provisioned `avatar_preferences` schema. The experience wiring checks
the schema without applying migrations, checks Redis availability and releases
its resources on shutdown. Consent still defaults off. Agent workers and the
dev bridge are not enabled by this flag.

The same explicit experience host also registers `GET /town/api/agent-runs`.
It uses the existing fresh human resolver, actual System Manager role and actual
company access, matching existing agent-event visibility. The IT building offers
Thai/English read-only run status, capped at 25 with review-needed work first.
Only id, agent key, status, call count, times and an unresolved flag are returned.
Delegator identities, prompts, raw errors, tool arguments/results, intent hashes
and credentials stay private. Scope is checked on every request; responses use
`no-store`. Unknown/duplicate query fields are rejected. The panel is destroyed
on company change, replay or disconnect, cancelling pending reads. There is no
retry/force-complete action: an uncertain ERP write still requires actual record
inspection before anyone repeats it. This does not activate a model or worker.
Observed on the ordinary dev host: the sales identity received 403; an existing
Administrator session received 200 with zero real runs and was logged out after
the read. No demo runs were inserted to make the screen appear active.

Before activation on 2026-10-11: ERP was reachable, company `ACME-TH`
exists, port 8090 is occupied by an unrelated program, no world reader variables
are configured, and the existing `town` database records migrations through
00002. Do not start world/outbox against this database to implicitly apply
00003–00005 before backup/restore and migration approval.

The next protected local operation proposed for review is: create a dedicated
`world-reader@erp-town.local` System User on **town.localhost**, without a welcome
email; grant only Town Projection Reader and read permission for Company, Sales
Order, Delivery Note, Sales Invoice, Warehouse, Item and Bin; restrict Company
User Permission to the existing `ACME-TH` company. Generate its API credential
and save it only as the local `TOWN_WORLD_API_KEY` / `TOWN_WORLD_API_SECRET`.
Do not repurpose Administrator or an existing human/agent credential.

Before opening the world service against the existing PostgreSQL **town**
database, take a local custom-format backup and prove restore in an isolated
PostgreSQL instance. Then apply embedded migrations **00003–00005** and start
the local world/outbox/gateway with experience enabled. River, paid providers,
hooks, pilot deployment and ERP business transactions are excluded from this
activation. The subsequent continuation authorized and completed this specific
local operation, as recorded below. No business transactions were created.

### Observed local activation

- PostgreSQL custom-format backup at ignored
  `go/tmp/backups/town-before-world-20261011.dump`; restored with
  `pg_restore --exit-on-error --no-owner --no-acl` into a separate PostgreSQL 18
  container. All five original public-table row counts matched. SHA256 and
  restore counts remain in the adjacent local restore report.
- Embedded migrations 00003–00005 applied to `town`; recorded versions 0–5 are
  applied. River tables were not installed.
- `erp_town.setup.local_reader.provision` explicitly provisions only the approved
  dev reader via the Administrator console. It is not whitelisted or automatic.
  Exact seven-doctype read profile, no export/import/print/email/report/share or
  write privileges, Company permission restricted to ACME-TH, no welcome email.
  Frappe needs `desk_access=1` on this role to retain the specified System User;
  this adds no business write grants. Repeated provisioning retains existing
  credentials and rejects broader roles/permissions rather than repairing them.
- Projection GET: ACME-TH returned 200 and 15 bounded metadata facts; an unknown
  company returned 403. World scope is obtained from the reader's actual ERP
  identity at startup. Out-of-scope snapshots/events, including legacy company
  names, cannot create rooms or prevent allowed-company startup. They remain
  untouched in storage; no old company event is silently rewritten. ERP rebuild
  supplies current business metadata, not missing historical approvals.
- Ordinary-site browser login using existing seeded sales identity returned to
  the town and established a real binary snapshot: shop 7, warehouse 4, bank 4.
  Thai/English switching preserved connection. Avatar consent stayed off.
  A sample document read was denied by both ERP resource API and Town with 403;
  no user permissions were expanded. History/avatars routes are enabled, while
  destructive or business-write behavior remains covered only on test.localhost.
- Narrow workspaces now place the town above the sidebar, avoiding a tiny island
  in a tall, narrow column. Evidence:
  `docs/town-client-dev-connected-2026-10-11.png`.

## Agent authority and reconciliation

`Binding` contains a run id, agent key, company and two actual ERP credentials:
the enabled Town Agent Identity and its human delegator. The factory must retrieve
these from trusted host configuration/current authentication, never model JSON,
job arguments, a browser-selected user id or an MCP tool argument. Persistent
jobs contain only the run id; prompts and credentials remain ephemeral.

Use `NewPostgresLedger(pool)` and `NewPolicyExecutor(port, ledger, binding)`;
construct the run with `executor.Tools()` and the same durable audit ledger.
`Queue.Enqueue` atomically records the run, River job and Go outbox event. Workers
verify their reconstructed binding against the stored run before invoking the
provider. Every tool rechecks both ERP identities, company/doctype authority,
master reference visibility and source document visibility.
Reads recheck document access after claiming. Source drafts compare both fresh
views against prepared policy-relevant facts immediately before effect; a
changed customer, line quantity/status or settlement fact stops with
`SOURCE_CHANGED` and awards no progress.

The registry exposes four document reads and four draft-creation tools. It
contains no submit/cancel/settlement tool. ERP calculates prices and amounts;
quantities are positive decimal strings, bounded to 100 total units. Runs allow
8 turns, 16 tools, 12,000 reported tokens, 64 KiB payloads and 2 minutes. The
sliding limit is 30 attempts per agent/company per 60 seconds. Native SDK retries,
redirects and proxy forwarding are disabled.

MCP callers must supply a stable `_meta["io.erp-town/call-id"]` for each intent.
Reusing a call id with a different canonical intent fails closed. A new durable
claim and its audit precede any ERP effect. Lost acknowledgment, cancellation,
expired worker lease and unresolved intent enter `needs_human`; do not retry the
write, replace its id or edit the ledger to force completion. A human must inspect
the authoritative ERP document and reconcile the unresolved operation. A future
administrative reconciliation UI/API needs its own reviewed design.

Safe durable results contain document reference/status/fixed outcome codes only.
Raw provider errors, ERP prices/parties, prompts and credentials are excluded.
Completed metadata has the 400-day boundary; unresolved writes are retained.
AI-only progress counts completed runs with audited successful tools and no
denied/unresolved call. Display XP is `successful_runs * 10`; the display level is
`1 + floor(sqrt(successful_runs / 5))`. No human XP/level/leaderboard exists.

## Installed Codex boundary

Codex CLI 0.153.4 was inspected locally through its generated app-server protocol.
`ThreadStartParams` supports dynamic tools, sandbox/approval configuration and
empty environment mappings. That does not establish that all built-in shell,
workspace and configured MCP capabilities are disabled. The approved isolated
ERP-only boundary therefore remains unproven. `NewLocalProvider("codex", ...)`
rejects selection; there is no unrestricted fallback. No app-server model thread
or external provider request was started. Enablement needs a verified capability
boundary plus separately approved model, credentials and budget.

## Avatar and historical views

The explicit experience host registers `GET/PUT /town/api/avatar/preference`,
`GET/POST /town/api/avatar/presence` and, only with a history source,
`GET /town/api/history`. Writes require the same-origin authenticated session and
`X-Town-Request: 1`; request identity/company fields cannot grant authority.

Consent defaults off. PostgreSQL stores only self consent and one of three
appearance choices. Presence is a separate memory-only channel with an opaque
id, coarse department/emote and a 60-second TTL. It never enters the durable
outbox, world snapshots or replay. Opt-out immediately removes live presence.
Fresh company/account denial clears the observed preference/presence; temporary
transport failure drops live presence without erasing consent. Idle live entries
are rechecked every 15 seconds. Dormant preferences without a live credential are
checked on the next authenticated access; an offline ERP account-removal feed is
not implemented. Shutdown releases in-memory credentials.

The scene renders at most two humans and one active AI indicator, using three
existing development GLB exports with recorded SHA256/licensing provenance.
Skeletons and tinted materials are independently owned/disposed; cached geometry
is shared. Poses are static on the demand-rendered scene, without movement paths.
These exports do not substitute for A's named human art review.

Retained history uses the existing Redis business stream, with current ERP
company/roles/private ownership checked at query time. Requests allow at most
24 hours and 1,000 transitions, scanning at most 1,001 retained entries. The UI
requests 15 minutes/100 transitions and shows partial retention explicitly.
Output has only time, document type/name, kind and coarse status. Presence, dev
activity, AI progress, monetary data and actor identities are excluded. This is
not a historic balance reconstruction, guaranteed audit recovery or export API.

## Dev bridge and hook normalization

The listener accepts exactly `provider`, opaque `task_id`, monotonic `sequence`
and `state`. Company and actor come from the host mapping. It rejects remote
peers, browser origins, unknown/duplicate/null fields, payloads over 4 KiB and
invalid tokens. Sequence/terminal checks and event insertion are transactional;
duplicate concurrent hooks commit only one event. A finished task cannot restart.
Go-owned `Town Dev Task` events display at IT with System Manager visibility.
Depguard forbids this package from importing ERP.

Installed versions: Claude Code 2.1.263, Codex CLI 0.153.4. The local normalizers
accept Claude `UserPromptSubmit`, `PostToolUse`, `Stop` and `StopFailure`; recursive
`stop_hook_active` is ignored. Codex's documented notification supports
`agent-turn-complete` only. A trusted host must supply its opaque task id and
sequence/start event; no synthetic Codex start hook is invented. A completed
event means a development turn finished, not that the whole project is complete.
Only four safe fields leave normalization; raw prompts, transcripts, code, cwd,
paths and credentials are discarded. No hook has been installed.

Protocol references: [Claude hooks](https://code.claude.com/docs/en/hooks),
[Codex advanced configuration](https://learn.chatgpt.com/docs/config-file/config-advanced).

## Separately approved rollout steps

1. Choose the dev or pilot site, company, service host and route exposure. Keep the
   dev bridge on its separate loopback listener and internal MCP authenticated;
   do not publish either as browser/ERP routes by default.
2. Approve the concrete database backup and migration operation for that target,
   including Go migrations 00004/00005 and River v0.49.0 schema. Verify an actual
   restore before applying them to an existing database.
3. Provision a dedicated company-restricted Town Projection Reader using the
   already documented read-only role/doctype permissions in
   [world-state-operations](world-state-operations.md). Keep user sessions,
   projection-reader credentials, delegated agent credentials and dev-bridge
   token separate. Do not repurpose Administrator or log returned secrets.
4. Supply the explicit host bindings above, approved localhost ports and graceful
   worker/presence shutdown. A production launcher/credential-rotation workflow
   remains separate from these local test hosts.
5. Observe snapshot/reconnect/revocation, SO -> DN -> SI -> PE, draft-only AI,
   uncertain-write reconciliation and bridge privacy on the configured target.
   Review mobile/connected GPU load and named human art/comprehension feedback
   before certifying program/pilot acceptance.

Only the approved local reader and `town` migrations were activated. No production
credentials, paid calls, hook installation, push/merge or deployment were performed.
