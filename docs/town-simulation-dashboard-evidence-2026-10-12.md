# Task simulation and game dashboard acceptance — 2026-10-12

The approved [implementation plan](superpowers/plans/2026-10-12-town-simulation-dashboard.md) is implemented in the existing town client and Go read boundary. ERP remains the business authority. Presentation arrival never submits a document or certifies customer delivery.

## Delivered behavior

| Area | Implementation and boundary |
| --- | --- |
| Navigation | `go/internal/livedemo/work-layout.json` is the shared versioned graph. Go and TypeScript use Dijkstra over the existing sidewalks. All 72 department/kind combinations match the previous route geometry and calculated duration. Go travel is `ceil(routeLength / 8 * 1000)` milliseconds, replacing the fixed 25-second delay. |
| Scheduled actors | UTC starts and bounded durations determine route distance directly. Background resume catches up to the server deadline. Stable, conservative whole-route windows serialize demo returns; the latest arrival controls the stage deadline. Unknown layout versions are rejected. |
| Ordinary work | A mutable 16-actor runtime uses cumulative distances, binary-search sampling, smooth heading, crossing reservations and separate human/AI work bays. Retargeting starts at the current pose. Waiting places sit beside the occupied bay. Idle, cancellation and scope reset release resources. |
| Trucks | New submitted-DN event IDs create bounded finite outbound jobs, with one moving truck and up to eight admitted jobs. Crossings cap speed and share pedestrian resources. Imminent scheduled pedestrians take priority without extending their Go deadline. Duplicate/reappearing subjects cannot redispatch a recorded event. Snapshots, replay and reconnect prime state without manufacturing dispatch. |
| Assets | Existing licensed town/character/truck assets are reused. Clip changes blend over 150 ms. The truck exposes a single baked body mesh, so independent wheel animation is unavailable. No new NuriTrack source, textures, models or Jev dependency was copied. |
| Shared dashboard | Immutable authorized facts and independent queues are projected once per source change. Presentation progress publishes every 250 ms. HUD, process flow, selected object, activity list and monitors use the same scoped view. Unavailable counts display `—`; document counts do not become people counts. |
| Interaction | Scene meshes/labels and list rows share typed selection. Camera follow ends on manual input. Closing document inspection returns keyboard focus. Desktop overlays pass pointer input through their empty areas. At narrow widths, HUD detail and existing document controls share one collapsible drawer. |
| Journey | Explicit authorized DN→SO, SI→DN and PE→SI references form a many-to-many graph. Read-only loading is limited to 32 visible/selected candidate documents, two concurrent reads, five-second request deadlines and 64-KiB streamed responses. Missing/inaccessible evidence stays partial. A submitted receipt alone cannot settle an invoice; the authorized invoice must have zero outstanding. The UI explains that the current endpoints cannot discover unseen children. |
| Read contract | Sales invoice detail now returns `against_delivery_notes: string[]`, deduplicated from authorized item references while retaining all invoice lines. Go fake/adapter/gateway and strict client decoding are covered. No migration, event or protobuf extension is involved. |
| In-world monitors | Active workstations own 512×256 `CanvasTexture` dashboards. Ordinary changes repaint at most twice a second; scope loss clears content, offscreen refresh is skipped and owned resources are disposed. The monitor plane is in front of its frame and a real raycast click selects its department. |
| Replay | The existing 64-point/1-MiB/15-minute retention remains. HUD uses the selected saved facts; actors and truck jobs reset. Historical selection cannot fetch present-day document details or execute commands. Human positions are not reconstructed. |

## Observed verification

| Check | Observed result |
| --- | --- |
| `pnpm --filter @erp-town/town-client test` | 138 tests passed across 23 files. |
| `pnpm --filter @erp-town/town-client build` | TypeScript check and Vite production build passed. |
| `pnpm --filter @erp-town/town-client test:browser` | 33 isolated cases passed. Includes both desktop sizes/languages, narrow drawer, mesh/list selection, manual follow cancellation, keyboard focus, real monitor raycasting, light restoration, no WebGL, replay/privacy and controlled hidden-tab scheduled resume. |
| `go test -race -count=1 ./internal/livedemo ./internal/gateway ./internal/erp/erpnext ./internal/erp/erpfake` | All four affected packages passed. Route parity, fake-clock deadlines, snapshot ownership, cancellation, uncertain writes, invoice-source deduplication and gateway contracts are covered. |
| `node tests/integration/client-acceptance.ts` | `TestClientLive` passed with race checking, including both real browser cases (session/world/Classic/history/expiry and departmental sales-to-cash). The harness restored `test.localhost`. |
| `pnpm lint` / `git diff --check` | Root Biome checked 278 files and Go lint reported zero issues; whitespace check passed. |
| One bounded actual ERP demonstration | Completed with four submitted documents, actual invoice outstanding `0.0`, one observed dispatch ID, zero browser business writes/page errors, no remaining trip and four idle actors. The launcher stopped gracefully and restored the original test-site database. |

The live source and UI poll at different intervals. The captured trace preserves their timestamps and transient differences; it does not claim an atomic refresh across ERP reads, world projection and presentation.

New regression cases were observed failing before fixes for route/schedule admission, retained business projection, occupied bay overlap, scheduled crossing priority, repeated dispatch on subject reappearance, journey root/stream bounds, keyboard focus and the monitor behind its frame. Initial integrated failures from hot reload and the previous persistent-selection assumption were diagnosed; final acceptance used stable code and explicitly reselected after reconnect while retaining permission/uncertain-write assertions.

### Actual ERP evidence

[Live observation](town-simulation-live-evidence-2026-10-12.json) contains server schedules/document results, timestamped HUD/actor/trip observations and the actual invoice read. Screenshots are supporting visual evidence:

- [Sales review](town-simulation-final-sales-review-2026-10-12.png)
- [Recorded dispatch](town-simulation-final-dispatch-2026-10-12.png)
- [Invoice work](town-simulation-final-invoice-review-2026-10-12.png)
- [Completed, idle town](town-simulation-final-completed-2026-10-12.png)

### Renderer evidence

[Renderer observation](town-simulation-renderer-2026-10-12.json) measures installed desktop Chrome on NVIDIA RTX 3080 Ti / ANGLE D3D11 with actual owned models, three actors, two work monitors, one dispatch and the full HUD. Authorized protocol fixtures isolate rendering load; actual ERP correctness is established separately above. The JSON records sample duration/count, active frame p95, peak draw calls/triangles, texture counts, idle frames and post-scope cleanup.

The initial 480-call finding came from transmissive vehicle glass and unnecessary shadow draws. Vehicles now reuse the building material adaptation, and glass, ground, decals and emissive panes do not cast solid shadows. The observed active slice is below the existing 300-call/750,000-triangle budgets and 33.3-ms p95 target. This is evidence for this slice on this GPU, not a production or mobile performance guarantee.

Final observation: 4,141 samples over 17.25 seconds, 270 peak calls, 368,028 peak triangles and active frame p95 4.3 ms. Texture count peaked at 27 and returned to 9 after scope loss. A separate connected idle observation with three idle actors and no truck rendered six demand frames in three seconds; status polling did not restart motion. The two owned monitor surfaces require 1,048,576 base RGBA bytes, approximately 1,398,102 bytes including mip levels. [Full HUD and working monitors](town-simulation-gpu-2026-10-12.png) is the rendering fixture screenshot.

WebGL does not provide a reliable VRAM byte total. The report distinguishes actual texture counts from a monitor-only RGBA/mipmap byte estimate. Total GPU memory, maximum-population stress and mobile hardware remain unmeasured.

## Handoff and remaining program gates

User approval on 2026-10-12 covered implementation and routine design decisions, including the planned read-only invoice-lineage extension. Work was sequential in the existing checkout. No dependencies, manifests, lockfiles, migrations, authentication, CI or deployment configuration were changed by this slice. No commit, push or deployment was requested or performed. Pre-existing work from the other chats was preserved.

The sequential/no-agent plan overrides the execution skill's optional fresh-agent review workflow. Author review checked the final source against R1–R12, the permission/command boundaries, resource cleanup, source version handling and the observed regression results. It is not an independent human review.

The owning chats' World State/Classic ERP progress remains the baseline; their completion records were not replaced. A's canonical Blender/art-direction checkout was not modified. G-A3/G-A4 art acceptance, production pilot/restore drills, native external model integration and deferred F remain separate program work.

Run the bounded test-site demo from the repository root with `node scripts/live-demo.ts --rounds=1`. Its terminal prints the URL. Enter `stop` in that terminal to shut down and restore the site. Existing commands and read/write permission gates remain in force.

## User-requested concurrent motion continuation — 2026-10-12

The user requested simultaneous work, a moving truck and continuous visible loops after reviewing the live demonstration. This supersedes the earlier demo-only requirement that the scene become idle when the bounded ERP round finishes. Ordinary ERP work and recorded delivery dispatch remain finite.

The local demo advertises the explicit `motion_mode: "loop"` capability. `DemoMotion` gives each of the four demonstration identities its own route and working dwell, using the existing sidewalk graph, crossing reservations and work bays. A waiting team does not halt another team's cycle. Its actor intents carry a visible demo marker and no ERP task reference or server travel deadline. The ERP stage panel and document statuses retain authoritative, bounded sales-to-cash progress.

One explicitly marked demo truck follows the closed road circuit continuously, wraps distance without teleporting at the seam, and yields at occupied pedestrian crossings. The parked duplicate is hidden in this mode. The truck, scene actors, selected objects, activity panel and HUD distinguish demo motion from actual ERP/AI work. The ordinary submitted-delivery route, event deduplication and completion behavior are retained.

Pause and hidden-tab handling freeze these visual cycles without catch-up. Replay, disconnect, company-scope loss, capability loss and an ERP failure remove the loop. No cycle generates an ERP transaction, payment, agent run or external provider call. Real ERP rounds stay bounded to the existing launcher option.

Observed verification: 143 client unit tests; 34 isolated browser cases plus the final extended loop regression; client TypeScript/Vite build; race tests for `internal/livedemo` and `cmd/town-live-demo`; root lint (281 files, zero Go issues). The new regression was observed failing against the former finite implementation. Browser coverage includes concurrent actors after ERP completion, continuously advancing truck distance, pause/resume, hidden-tab handling, replay, failure cleanup, disconnect and zero business-write requests. Multi-cycle unit coverage exercises five minutes of actor motion and four minutes of truck motion with fixed identities and bounded resources.

The previous live host was stopped gracefully and restored `test.localhost` before restarting. The updated actual demo visibly confirmed submitted SO/DN/SI/PE, four independently active actors and a truck lap counter greater than two. It remains running for the user's review; its test-site snapshot will be restored when the launcher receives `stop`. A fresh full Go/Python/integration run is not claimed for this continuation.

[Updated concurrent live scene](town-live-demo-loop-2026-10-12.png) shows the running demo and explicit motion labels.
