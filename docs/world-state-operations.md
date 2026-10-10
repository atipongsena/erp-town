# World State local operations

`town serve` enables `gateway,outbox,world` when roles are omitted. Until a reader
has been provisioned, run the existing capabilities with `--roles=gateway,outbox`.
No role or credential is created automatically.

World requires the existing `TOWN_JWT_PRIVATE_KEY` and **separate**
`TOWN_WORLD_API_KEY` / `TOWN_WORLD_API_SECRET`, belonging to a dedicated ERP user
with `Town Projection Reader`, read permission for Company, Sales Order, Delivery
Note, Sales Invoice, Warehouse, Item and Bin, and company user permissions. Keep
API keys in local environment variables, never source control. The endpoint checks
the credential's real role and permissions; claims supplied by a caller cannot
grant access. It returns metadata and sequence fences, without amounts or parties.

`GET /town/ws` upgrades a same-origin connection. Send binary protobuf JOIN within
five seconds with a town JWT and authorized company. JWTs are never URL parameters.
The `@erp-town/world-proto` package defines the protocol and generation command.
Expiry closes the connection; exchange the ERP session for a fresh JWT through the
existing gateway and reconnect. Resume tokens live for 30 seconds, are single-use,
and require the last successfully received version and the same user/roles/company.
A failed resume returns a freshly filtered snapshot.

Only one world owner can run per stream/group; PostgreSQL holds an ownership lock.
Snapshots and ERP reconciliation run every 30 seconds. Any pending stream entry
postpones checkpoint advancement. Restart replays raw retained history, including
events already in durable dedupe; it does not republish ERP events. Missing/trimmed
history rebuilds the bounded metadata projection, not historic approvals or errors.
Trimming during replay restarts the unpublished attempt, up to three attempts; an
unstable stream or regressed cursor fails startup instead of presenting partial state.
After trimming, historic non-reconstructible facets can be absent. Existing business
documents remain authoritative in ERP Desk. Internal snapshot format 2 preserves
per-facet reconciliation fences without advancing unrelated event facets.

Run these **one command at a time** from the repository root:

```powershell
pnpm world:gen
go -C go test -race ./internal/world -run World200Clients -count=1 -v
```

The acceptance test uses isolated PostgreSQL/Redis containers and 200 real sockets.
It publishes no ERP transactions. It collects every client's patch across 20 stream
entries (4,000 samples), using Redis entry timestamps and a p95 limit of 250 ms.
This is local acceptance evidence, not a pilot capacity guarantee.

For observation of an already running local service, provide a short-lived token
via `TOWN_LOAD_JWT`, then run:

```powershell
go -C go run ./tools/worldload --company ACME-TH --clients 200 --duration 30s
```

The observer only JOINs and reads frames, prints aggregate counts, and does not
publish synthetic events, expose token/data contents, or measure stream latency.
Real credential provisioning, deployment routing, pilot migrations and pilot load
tests require separate approval.
