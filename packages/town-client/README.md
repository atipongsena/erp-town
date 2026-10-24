# Town client

Development 3D client for the existing Sunny Office Town art and C's company-filtered
world protocol. F (PromptPay/LINE) is deferred; ordinary Payment Entry remains.

Run `pnpm dev:client` from the repository root and open `http://localhost:5173/?lang=th`.
Use `?lang=en` or the language switch for English. `pnpm build:client` creates `dist/`.
The client uses relative art URLs and local fonts/Draco decoders; no CDN is required.

## People and AI mockup

Open `/?mode=demo&lang=th` (or `lang=en`) for the isolated working-day mockup.
The live workspace also links to it. It mounts a separate host without ERP sessions,
API calls, world sockets, credential access or provider calls, even with `connect=1`.
All records, people, amounts and actions are local sample data, discarded on reload.

The header switches between **3D town** and **ERP workspace**, sharing the same
story and role. `/?mode=demo&view=erp&scene=7&lang=en` opens the completed document
example; `scene=1` through `scene=7` select a story scene, with invalid values
starting at scene 1. The ERP view lists the visible SO/DN/SI/PE records and shows
customer, items, status, sample VAT, invoice balance and payment reference. It does
not mount the 3D renderer and works without WebGL. Role changes derive selection
from the newly filtered records immediately; Warehouse sees neither prices nor
accounting documents. **View completed example** is a playback shortcut, with no
transaction permissions or ERP writes. Actual records remain in the connected
document desk and Classic ERP through the full-stack local demo.

Use **Play story**, **Next scene** or **Back** to preview the seven scenes of an
SO -> DN -> SI -> PE handoff. For interactive work, change roles to AI -> Manager ->
Warehouse -> AI -> Accounts, then use the next-task action. AI prepares drafts;
people review and confirm documents, dispatch stock and record the payment.
Playback is a presentation control, independent of the selected role's actions.

Observer is the default and can view all six departments without making changes.
The other five roles illustrate scoped access. **Access & permissions** lists
view/draft/confirm/payment access per department. Changing roles filters document
facts, people, timelines and amounts immediately; warehouse roles cannot see
prices or accounting documents. This illustrative policy is not the live ERP
permission model and never changes real account rights. The renderer retains its
existing maximum of two human characters and one AI; the roster lists all visible
sample participants, with the current human actor prioritized in the town.

## Scene motion

The actual workspace renders people from consented presence and the current
document session's read/edit/create/submit activity. Clicking a department alone
does not report work or move the signed-in person. AI department/activity comes
from a trusted typed ERP tool claimed by its ledger; a generic running status or
document backlog does not invent active workers. Terminal jobs clear work.

Stable actor identities follow a sidewalk/crossing graph to the work department,
then use read/carry/type clips. Typing has an active desk, chair and terminal.
Routes exist only during travel and disappear on arrival, pause or task cancellation.
The city displays work locations, not tracked physical employee coordinates.
Consented presence remains anonymous, coarse, ephemeral and outside replay.

A newly received `erp.delivery_note.submitted` event creates one finite truck
journey. Initial snapshots, history, reconnects, billing updates and cancellation
cannot replay an old dispatch. Trucks follow left-hand lanes around the outer
road and leave through the bridge; independent swept-footprint tests include
the source truck's wheels. There is no endless road traffic or animated water.
Clicking the parked or travelling truck opens authorized Delivery Note status,
without claiming assigned vehicle, GPS tracking or customer arrival.

**Town animation** pauses movement; replay freezes it. Reduced-motion devices
start paused. The demand renderer wakes only for active work/transport or camera
transitions. Foreground elapsed time keeps travel independent of GPU frame rate;
pauses and hidden tabs do not accumulate catch-up time. Hardware FPS remains
unmeasured. Rendering never creates business documents or silently enables consent.

## Self-starting live ERP demo

With the existing Docker ERP stack running, run from the repository root:

```sh
node scripts/live-demo.ts
```

Open `http://live-demo.localhost:5196/?lang=th&liveDemo=1` (or `lang=en`). Login,
connection, staff assignment and the workflow start automatically. The dedicated
hostname keeps the demo session cookie separate from ordinary `localhost` work.
This desktop-first demonstration records actual SO -> DN -> SI -> PE documents
on **test.localhost only**, with role-scoped Sales/Stock/Accounts staff accounts.
The AI uses a deterministic local provider through the real policy executor,
durable ledger and event publisher. Staff are automated demo participants;
no external model is called and no physical delivery tracking is implied.

The launcher snapshots/seeds the test site, creates a separate temporary PostgreSQL
container and periodically drains due test outbox rows through the existing publisher.
The pump is bounded and serialized. It stops on failure without retrying business
writes. The viewer has standard human ERP roles restricted to the seeded company.
Default playback runs three rounds then becomes idle; `--rounds=1` or `--rounds=2`
shortens it. Use an interactive terminal and type `stop` or Ctrl+C to stop the host,
wait for its outbox pump and restore the test-site snapshot. Do not run integration
tests against test.localhost while this demo holds its snapshot. Credentials remain
in process memory and never enter URLs, browser payloads or logs.

## Boundaries

The initial town is an explicitly disconnected art preview, without fixture business
counts, financial values or real employee presence. Selecting a building opens its
inspection roof. The object list provides the same selection with keyboard/touch;
Escape returns to overview. Reduced motion sets roof/camera poses directly. Economy
quality lowers pixel density and disables shadows.

Connect is an explicit user action. It reads same-origin `/town/api/me`, validates
company membership, obtains a JWT from `/town/api/auth/token` with the existing
`X-Town-Request: 1` header and joins `/town/ws` in a binary frame. JWT/resume tokens
are never rendered or persisted. Auth requests are bounded to 45 seconds and
company changes cancel old requests/connections. Disconnect, expiry and invalid
frames hide old facts. Counts describe visible signals, not an ERP backlog.

The renderer only maps known facets and the whitelisted document status field.
It does not expose amounts, parties, actor ids or arbitrary payload text. Classic
links use the existing same-origin `/desk` ERP document routes. Document inspection
uses separate permission-checked gateway reads; amounts never come from world facts.
Running standalone Vite does not provision, proxy or host the
ERP gateway: its Connect/Classic routes require an approved same-origin local
integration. Real reader credentials and pilot/deployment configuration remain
separate protected operations.

## Document desk

While connected, Shop, Warehouse and Bank provide a bilingual document desk.
Open a visible fact or enter an existing document number. Shop can create an order
from existing customer/item codes with multiple lines and optional ERP price-list
rates. Saving always creates a draft; confirmation is a separate action after
reading the actual ERP document. A submitted order can produce a delivery draft,
a submitted delivery an invoice draft, and a submitted invoice a full-balance
payment draft. Partial delivery, partial payment, returns and cancellation use
Classic ERP in this slice. Department names do not define permissions: ERP remains
the authority for the caller's actions.

The gateway exposes GET/POST pairs under `/town/api/delivery-notes`,
`/town/api/sales-invoices` and `/town/api/payment-entries`, alongside existing orders
and document submission. Each create takes the selected company, its submitted
source document and an optional posting date; payment also takes an existing
mode of payment. Source company/status checks precede writes. OpenAPI remains
available at `/town/api/openapi.json`.

Amounts/quantities stay decimal strings, without browser rounding or an assumed
currency. A pending approval is distinct from submission. In-flight actions are
serialized and never automatically retried. A lost/invalid write acknowledgement
blocks further writes until the user checks Classic ERP and explicitly acknowledges
the warning. A known created name remains available if only its subsequent read
fails. Company/connection changes clear document details, cancel pending requests
and ignore late responses; an interrupted write retains its uncertainty warning
in memory across reconnects. Nothing is persisted in browser storage.

## Art snapshot

`public/art/integration.json` records SHA-256 provenance for eight byte-identical A
building exports and the separately exported environment. `src/layout.json` keeps
the source building transforms in glTF coordinates. Art gates G-A3/G-A4 remain open.
Human/agent scene roots are excluded from the static environment; it does not
represent opt-in presence or an agent runtime. The `ERP_Flow_Preview` collection
is excluded so historical art routes cannot remain visible when nobody is moving.

The current environment's 163,490 triangles merge to 25 material groups and occupy
718,416 bytes with Blender Draco compression. Source cars and trucks are exported
separately and recorded in `integration.json`; refreshing art preserves the
separately exported character provenance. The scripts leave the saved source
unchanged. Its procedural water cannot be represented by glTF, so `adaptMaterial`
replaces only the water role with the shared palette color and owns that material.
Other cached GLTF materials are preserved.

To refresh the snapshot, run Blender **in a separate background process** with
`scripts/export_environment.py -- <source town.blend> <public/art/town-environment.glb>`,
then `node packages/town-client/scripts/sync-art.mjs <art checkout>` from the repo root.
Run Biome formatting on copied JSON afterward. The scripts never save the source.
Draco files are vendored from the approved Three installation; its Apache 2.0 notice
and the fonts' OFL notice are included. A's source manifest records model credits.

## Observed checks, 2026-10-10

- All workspace unit tests: 138 passed, including 24 client tests and 43 wire/state
  tests. Both affected packages passed typecheck; root lint including Go lint passed.
- Six Playwright browser cases passed against development and the production build:
  actual GLB rendering, TH/EN keyboard selection, mobile/reduced motion, unauthorized
  gateway, failed model, binary snapshot/expiry clearing and no-WebGL fallback.
  HTTP/WebSocket routes in these tests are fixtures, not live ERP or Go connections.
- Production chunks are lazy/split, with the largest minified chunk approximately
  436 kB. The local rendered scene reported 219,650 triangles and 241 draw calls.
  Chromium used SwiftShader; no hardware FPS or pilot performance claim is made.
- Development emits an upstream Three Clock deprecation warning through R3F.
  The deliberately missing-model browser case emits the expected loader error.

Browser checks are isolated from the ERP e2e setup and never seed a live site:
`pnpm --filter @erp-town/town-client test:browser`. Set `TOWN_CLIENT_PREVIEW=1` to
run the same cases against the built output. Screenshots/traces stay in ignored
`test-results/`. Full sales-to-cash actions and E/G/H remain in the program plan.

## Live acceptance, 2026-10-11

After `pnpm build:client`, run `node tests/integration/client-acceptance.ts` from
the root with Docker running. It snapshots/seeds **test.localhost only**, creates
a dedicated company-restricted read-only reader, and restores the database in
`finally`. A Go race test serves the built client, gateway, world WebSocket and
Classic ERP from one temporary loopback origin. PostgreSQL is provided by the
existing testcontainers helper; Redis uses only `town:test:events` and a unique
consumer group. The normal Vite preview and dev/pilot configuration are unaffected.

Observed: real seeded-user login, binary snapshot, Sales Order creation/submission
through the gateway, an ERP event reaching the browser, correct bilingual work
status, Classic document link, disconnect and JWT expiry clearing old facts passed.
The reader cannot create documents or read the other test company. JWT seed and
reader keys are process-only; browser network traces/videos are disabled and
reader/admin keys are removed from the browser subprocess environment. The screenshot
contains test document metadata only. The six fixture browser cases also passed,
and all 139 workspace unit tests passed at that first checkpoint.

The document continuation completed the real SO -> DN -> SI -> PE browser flow
with the seeded Sales, Stock and Accounts sessions. Actual invoice amounts matched
the UI, payment drafts did not settle the invoice, and submitting the receipt
reduced the ERP outstanding amount to decimal zero. The reader was denied writes
to all four document types and an anonymous delivery create was rejected. Both
live cases and the Go race/leak test passed; test-site restoration completed.

Eight fixture cases passed, including mobile/no-WebGL manual document opening,
draft review before explicit confirmation, bilingual controls and ambiguous-write
blocking. There are 35 client unit tests (149 workspace total); the approval race
regression ensures the latest ERP status supersedes an earlier workflow
acknowledgement. Build/typecheck, root lint, integration-tag lint and gateway race
tests passed. Hardware performance and G-A3/G-A4 acceptance remain open.

## Local operator access and reader permissions

The signed-in identity and roles come from the existing gateway session. For this
development workspace, `sales@acme-th.test` has the installed standard human ERP
and HRMS roles, including System Manager. This is a local account configuration,
not a client-side permission bypass; ERP still checks every action. No password,
API key, reader authority or company restriction is changed by the operator helper.
`erp_town.setup.local_operator.provision` is a non-whitelisted, Administrator-only
bench command restricted to `town.localhost` in developer mode. Dry run is the default.

Reader provisioning preserves existing custom permission tables and copies standard
permissions before adding the first Custom DocPerm. A reader-only custom table
otherwise shadows all standard roles. The guarded, non-whitelisted
`erp_town.setup.local_reader.repair_existing_permissions` repairs only that
recognizable old state; other operator custom tables remain unchanged. The local
repair restored Sales Order, Delivery Note, Item and Bin staff grants. Actual
browser inspection of submitted `DN6910-0003` succeeded after the repair, including
its line items and linked order, without creating a business transaction.

Observed for this continuation: 92 client unit tests passed; build/typecheck and
root lint passed. The full isolated browser run passed 21 cases and exposed a
dock label covering the clickable cab. After repositioning that label, the
unchanged truck-mesh regression passed three consecutive runs. Eight reader
permission tests and four operator guard tests passed on `test.localhost`.
Actual connected screenshots show the dock worker and authorized delivery work
in `docs/town-truck-work-2026-10-11.png`. Continuous-motion hardware FPS remains
unmeasured.

## Live work and automatic demo acceptance, 2026-10-12

The state-driven renderer keeps stable actors, uses separate human/AI workstations,
and follows collision-checked sidewalks and crossings. Foreground elapsed time
drives walking and work clips even with slow software rendering. Idle actors and
completed trips stop drawing; a polling status endpoint does not restart movement.
Camera/roof transitions settle exactly after work ends. Static art excludes the old
permanent `ERP_Flow_Preview` route objects; the canonical Blender source is unchanged.

Observed: 108 client unit tests, all 23 isolated browser cases, client typecheck/build,
root Biome/Go lint, three
launcher lifecycle tests and race tests for agents, avatars, world, events, the demo
host and scenario passed. Seven Python demo guard/publication tests passed on the
test site. The launcher was stopped gracefully and its original test-site database
was restored before starting the current three-round demonstration.

A real ERP round produced submitted SO, DN, SI and PE documents. Reading the actual
invoice confirmed outstanding `0.0`; all participants became idle at completion.
`docs/town-live-work-evidence-2026-10-12.json` records the test-only document names
and result. Desktop screenshots are `docs/town-live-work-2026-10-12.png` and
`docs/town-live-dispatch-2026-10-12.png`; the latter shows a newly submitted DN
triggering one moving truck in the live browser.
The AI provider is local and staff accounts are automated demonstration participants.
This does not represent physical employee or vehicle tracking, production readiness,
hardware FPS, or acceptance of the remaining pilot/art program gates.

## NuriTrack-inspired operations view, 2026-10-12

Reference: [NuriTrack live demonstration](https://nuri-track.vercel.app/), inspected
in desktop Chrome. The visible application provides selectable site process steps,
parcel journeys, fleet/dock panels, replay and day/sunset/night environment controls.
Its Jev settings describe structured reading with default rules when AI is off.
The supplied video description is supporting context; its source code, license and
internal architecture were not independently verified. No NuriTrack code or assets
were copied. This increment implements the useful interaction patterns with the
existing ERP Town stack and art, without dependency changes.

| Reference pattern | ERP Town adaptation |
| --- | --- |
| Site flow with camera destinations | Four keyboard-accessible stages: orders, deliveries, invoices and receipts. A stage focuses its existing department and document inspection. |
| Operational status panels | Count only authorized `document` facts, with explicit draft/submitted/cancelled/unknown and existing awaiting-action statuses. Approval, stock and overdue signals do not inflate document totals. |
| Environment controls | Day, sunset and night lighting presets in both the connected client and offline demo. Presets reuse the existing two lights, add no draw calls, and do not advance ERP or replay time. Day remains the default. |
| Replay | Existing session replay feeds the same stages and explicitly labels historical counts. Disconnect clears counts to unavailable rather than zero. |
| AI interprets, code decides | Retain the existing typed agent policy and ERP command boundaries. No Jev integration or external model calls were added. |

These stages are independent document queues, not a linked shipment or sales order
journey. The current display contract provides no document lineage, delivery GPS,
arrival proof or invoice settlement evidence. Missing records mean no visible
documents, not that a business step has completed. End-to-end tracking would need
an explicitly designed server contract before implementation. Large hub machinery,
air/sea transport, accelerated ERP time and weather are outside this increment.

Verification includes regression cases for authorization/disconnect boundaries,
document-only counts, cancelled/unknown states, browser replay, keyboard camera
selection, Thai mobile layout and reduced motion. Lighting checks compare actual
rendered town pixels across all presets and require the restored daytime image to
match; the HTML toolbar is excluded from that comparison because native select
focus/hover changes its pixels independently. Browser checks use isolated protocol
fixtures, with no ERP business writes. Screenshots are offline previews:
`docs/town-operations-flow-2026-10-12.png` and `docs/town-night-2026-10-12.png`.
The tests write new images under `test-results`; these two review images were
copied into `docs` after verification. Observed: client build/typecheck, 112 client
unit tests, all 26 isolated browser cases and root Biome/Go lint passed.

## Shared simulation, game HUD and verified journeys, 2026-10-12

The queues-only increment above is a historical baseline. The current client
adds a shared versioned Go/TypeScript route graph, scheduled travel deadlines,
bounded work-bay/crossing reservations, finite dispatch jobs and typed shared
scene/list/camera selection. Frame poses stay outside React; presentation UI
progress publishes at 4 Hz. Human/AI work and document counts remain distinct.

The desktop HUD combines metrics, process steps, selected details, work queue,
verified business journey and replay over the town. Its empty areas pass pointer
input through to the scene. At narrow widths, HUD detail and the existing document
forms share one collapsible drawer. Existing keyboard, reduced-motion, no-WebGL,
Classic ERP and explicit document-command controls remain available.

Active workstation monitors render the same scoped dashboard using cached 512×256
textures at up to 2 Hz. Clicking their actual screen selects the department.
Replay uses saved business facts, removes current actors/trips and never reads
present-day documents or issues commands. Company/access loss clears selection
and all presentation resources; reconnect requires explicit reselection.

Invoice detail now exposes authorized `against_delivery_notes: string[]` alongside
existing DN→SO and PE→SI references. The journey preserves branches and only
loads visible/explicit candidates (32 nodes, concurrency two, five-second reads,
64-KiB response cap). It explains missing coverage and does not infer settlement
from a submitted payment. Only the actual invoice's zero outstanding supports
that label. No event/protobuf, migration or dependency change was required.

Observed final checks: 138 client unit tests, all 33 isolated browser cases,
typecheck/production build, affected Go race packages and root lint passed. The
real test-site browser acceptance passed both cases and restored its snapshot.
A bounded ERP round produced four submitted documents and outstanding `0.0`,
one finite truck trip, four idle actors at completion and zero browser business
writes/page errors. The launcher stopped gracefully and restored `test.localhost`.

On the RTX 3080 Ti, the active rendering fixture with three actors, two monitors,
one dispatch and full HUD recorded 270 peak calls / 368,028 triangles and p95
4.3 ms over 17.25 seconds. Textures reduced from 27 to nine after scope loss;
three idle actors generated only six demand frames in three seconds of polling.
These are observations for this slice, not maximum-population/mobile or production
guarantees. WebGL's total GPU byte allocation remains unavailable; monitor byte
estimates are identified separately in the report. The baked truck asset does
not expose independent wheels. A's art gates and pilot acceptance remain separate.

Full algorithms, evidence, limitations and handoff:
[`docs/town-simulation-dashboard-evidence-2026-10-12.md`](../../docs/town-simulation-dashboard-evidence-2026-10-12.md).
Run `node scripts/live-demo.ts --rounds=1` from the root for the bounded ERP demo;
enter `stop` in its terminal to shut down and restore the test site.

## Online showcase packaging

`VITE_TOWN_STATIC_DEMO=true` forces the isolated `DemoApp` for every URL, including
URLs containing `connect=1`. It never mounts the authenticated ERP application.
Build it with Vite's deployment base (for GitHub Pages, `/erp-town/`) and publish
only the generated static files, with `.nojekyll` for branch-based Pages hosting.
Keep this flag unset for the connected client and test-site live demo.

The observer view reuses `usePresentation`, `DemoMotion` and the shared truck
runtime: three staff plus one AI illustrate concurrent routes and work-bay waits,
and one labeled truck circulates indefinitely. Other role views retain the
existing role-filtered story handoffs. Neither mode calls ERP or an AI provider.
The visual loops do not advance the seven-step story or grant workflow actions.
Pause/hidden tabs stop both frame motion and recurring loop pose sampling.

Repository overview, real local demo setup and recordings: [root README](../../README.md).
