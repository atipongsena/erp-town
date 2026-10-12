# ERP Town task simulation and game dashboard implementation plan

User-directed continuation, 2026-10-12: the live demo now provides concurrent, continuous **presentation** cycles for its four teams and one truck. The bounded real ERP workflow and ordinary finite dispatch retain their contracts. Demo motion is labeled across the scene and HUD and stops on pause/hidden, replay, failure or scope exit. Acceptance and updated checks are recorded in [the evidence report](../../town-simulation-dashboard-evidence-2026-10-12.md#user-requested-concurrent-motion-continuation--2026-10-12).

> **For agentic workers:** Implement sequentially with `superpowers:executing-plans`. Read this plan and the linked existing designs first. No parallel agents, automatic commits, or implementation during the planning request. Execution completed after the subsequent implementation approval; checked items describe the scoped work and evidence recorded below.

**Goal:** Make the town visibly explain real authorized work through coherent actor/vehicle movement, a dashboard over the game scene, and inspectable business journeys.

**Architecture:** ERP and the existing Go gateway remain the business authority. A bounded presentation simulation consumes authorized work observations and controls positions, animations and scene reservations. A shared dashboard view supplies the HUD, selected-object details, process flow and in-world screens; movement completion cannot complete an ERP document.

**Tech Stack:** Existing React 19.3.0, R3F 9.8.1, Three.js 0.186.1, Drei 10.7.9, Zustand 5.0.15, TypeScript, Vite 8.3.4, Vitest, Playwright and Go. No new dependencies are proposed.

**Spec / design basis:** Requirements and design decisions below capture the current conversation. Preserve [town client design](../specs/2026-10-10-town-client-design.md), [program completion design](../specs/2026-10-11-program-completion-design.md), [program handoff](2026-10-10-program-handoff.md), and A's existing art ownership in `D:/ERP_Town-art-direction/`.

**Status:** Scoped implementation completed, 2026-10-12 Asia/Bangkok, following explicit user approval to develop and make routine decisions. Tasks 0–10 are verified below. No commit, publication or deployment was performed.

## 1. Requirements captured from the conversation

| ID | Requirement | Owning tasks |
| --- | --- | --- |
| R1 | Study NuriTrack at algorithm level and reimplement suitable patterns using our existing code/art. | Design basis, 1–10 |
| R2 | Actors move because of observable work, go to the appropriate department, perform the corresponding action, and stop when work ends. | 1–3 |
| R3 | Roads, workstations and queues affect visible operation; routes appear only during actual presentation travel. | 1–4 |
| R4 | Truck dispatch follows a new submitted Delivery Note; reconnect, snapshots and replay do not manufacture journeys. | 4, 9 |
| R5 | Desktop is the primary experience; existing keyboard, reduced-motion and no-WebGL access remain available. | 3, 6, 10 |
| R6 | Put the dashboard on the game page: KPIs, process steps, selected details, work list and history. | 5–7, 9 |
| R7 | Clicking a scene object or dashboard row selects the same object and can focus/follow it. | 5–6 |
| R8 | Numbers, status labels and business links come from authorized evidence, with explicit unavailable/partial states. | 5, 7, 9 |
| R9 | Include useful dashboards on monitors inside the town, backed by the same view as the HUD. | 8 |
| R10 | Reuse day/sunset/night controls and existing replay; environment cannot accelerate ERP work. | 6, 9–10 |
| R11 | Respect progress from other chats and preserve their uncommitted work and art. | 0, 10 |
| R12 | Plan first, including algorithms, file ownership, tests and acceptance before development. | This document |

## 2. Current baseline: reuse rather than rebuild

Inspected on 2026-10-12: Git HEAD `b719ca5`, with substantial tracked and untracked work. HEAD alone does not describe the current client. Other chats inspected: **สรุปสถานะโปรเจกต์ ERP Town**, **พัฒนางานต่อ**, and **โคลนระบบโลจิสติกส์ 3D**; the last provided no additional readable implementation evidence. The latest status chat also fixed the Classic ERP reload loop. Recheck Git and relevant chat progress before execution.

| Existing implementation | Evidence / limitation |
| --- | --- |
| Company/role-filtered world synchronization and recovery | `go/internal/world/`, `packages/world-proto/`; room patches coalesce at 100 ms, versions remain `bigint`. |
| Dijkstra pedestrian routes and finite truck route | `packages/town-client/src/work-navigation.ts`; preserve the greybox sidewalk/crossing geometry. |
| Work-driven characters and GLB clips | `live-work.ts`, `WorkCharacters.tsx`, `Characters.tsx`; preserve identity and owned skeleton cleanup. |
| Event-driven dispatch | `DeliveryTransitions` primes initial snapshots and emits only a new `erp.delivery_note.submitted` event. |
| Real test-site sales-to-cash demonstration | `go/internal/livedemo/scenario.go`; bounded to three rounds, local AI provider, actual ERP calls. Travel currently waits a fixed 25 seconds. |
| Four independent document queues | `operations-flow.ts`, `OperationsFlow.tsx`; these do not establish document lineage or settlement. |
| Lighting controls | `scene-environment.ts`, `EnvironmentControl.tsx`; reuse the existing two lights. |
| Client replay and retained business history | `replay.ts`, `history.ts`, `HistoryPanel.tsx`; historical status display is distinct from positional simulation playback. |
| Existing document reads/commands | `documents.ts`, `DocumentDesk.tsx`, Go gateway; delivery reads include `against_sales_orders`, payment reads include `against_invoices`. Invoice reads lack delivery lineage. |

Historical evidence, not checks rerun for this plan: [live ERP round](../../town-live-work-evidence-2026-10-12.json) records submitted SO/DN/SI/PE and invoice outstanding `0.0`. Client README records 112 unit tests, 26 isolated browser cases, build/typecheck and root lint passing after the first operations/lighting increment. [Renderer evidence](../../client-renderer-evidence-2026-10-11.md) records the existing 300-call/750k-triangle budgets and remaining hardware/art gates. Neither test count nor a screenshot certifies the proposed work.

### NuriTrack findings used as design input

Primary reference: [interactive demo](https://nuri-track.vercel.app/) and its [public deployed bundle](https://nuri-track.vercel.app/assets/index-peh6Nd-7.js), statically inspected on 2026-10-12. Minified symbols and asset names can change. No source repository/license was established; implement our own algorithms and reuse our art. The supplied videos were not independently inspected.

- Waypoint paths, corner rounding, cumulative distances and binary-search sampling; queued drive/work/wait actions and resource reservation.
- Separate frame motion from HUD updates: the inspected app publishes a UI tick every 250 ms.
- Screen-space HTML over a canvas, transparent input pass-through outside interactive panels, and world-anchored HTML labels.
- Shared selection for scene, list, details and camera; process-step buttons focus an entity or a zone.
- Bounded positional replay and risk-first sorting of operational lists.
- Some people follow loops, AMR choices are randomized, some KPI values are seeded, and collision recovery can temporarily ignore stationary blockers. Do not use these as ERP work evidence or replicate those shortcuts in our live experience.

## 3. Global constraints

- Implement sequentially in the current checkout; preserve unrelated changes. Reuse A's palette, fonts, metre scale, building anchors, models and roof animations. G-A3/G-A4 remain separate art acceptance gates.
- No dependencies, manifest/lockfile edits, persisted simulation tables, migrations or world protobuf changes in this plan's core tasks.
- Task 7's optional invoice-lineage DTO extension is a separately reviewable API change; it requires approval before implementation. Missing approval permits a truthful partial journey, not an invented edge.
- Do not change auth, credentials, ERP permissions, CI, infrastructure, deployments or external provider settings. PromptPay/LINE remain deferred; real Claude/Codex hooks and paid models remain outside this increment.
- Do not copy GPL ERP source into proprietary Go code or hand-edit generated event/protobuf/sqlc files. Use the existing port and clean-room adapter conventions.
- Tests that write ERP use `test.localhost` and the existing seed/restore harness only. Opening the automatic live demo can cause ERP test writes; do not treat it as a read-only preview or casually restart it.
- Company/session/visibility loss clears sensitive details, task associations, selection and screen textures. A reconnect snapshot does not replay old dispatches.
- World facts have no financial balances. HUD counts mean **visible records**, not company-wide totals. No "today", SLA or rate without timestamps and a defined source/window. Amounts remain decimal strings in authorized document details.
- Human avatars represent coarse department activity with existing consent, never inferred GPS, productivity scoring or employee tracking. AI progress remains separate from human work.
- Keep TH/EN UI; write code/comments/tests in English. Use LF. No commit/push/PR/deploy without explicit authorization.
- Meaningful behavior changes follow failing regression → implementation → passing check. UI composition uses browser interaction/layout evidence; do not manufacture tests for static styling.

## 4. Proposed design decisions

### 4.1 Separate business outcomes and presentation phases

```text
ERP event / authorized task observation
                    |
             presentation work intent
                    |
       queued -> reserved -> travelling -> arrived -> working
                    |                              |
             blocked / cancelled             observed outcome
                                                   |
                                      completed / failed / idle
```

`arrived` is spatial presentation state. `completed` requires an observed task result; a submitted Delivery Note proves a recorded dispatch, not physical delivery. Reading a document can produce a read animation, but it cannot mark that business process complete. Completed records alone must not keep workers busy forever. Unknown actor activity stays unknown/idle rather than assigning imaginary work.

For the automated test demo, the Go scenario owns stage deadlines and actual ERP execution. Replace the fixed travel delay with a route-length-based deadline using the same layout and speed as the client. A browser acknowledgement never authorizes an ERP command or blocks server operation when the browser is hidden. Ordinary live human/AI tasks use existing observations; they are not delayed by rendering.

### 4.2 Navigation, timing and reservations

- Preserve Dijkstra for this small sidewalk graph. No navmesh/A* dependency is needed.
- Share graph nodes, edges, department work bays and scene speeds as a versioned static JSON layout. Canonical file: `go/internal/livedemo/work-layout.json`; Go embeds it and the Vite client imports the same JSON. It contains public geometry only, never business data. Build verification must prove the cross-workspace import works without config/dependency changes.
- Human and AI walking speed initially stays at the current 8 scene units/second; truck cruise stays 9. These are stylized presentation speeds, not actual measured km/h. Distinguish scene seconds from ERP timestamps.
- Route samples use accumulated distance and binary search; headings use the shortest angular difference. Explicit turn/dock segments handle reversing. Truck wheels use distance/radius.
- The presentation runtime owns positions and scratch vectors outside React state. Retain demand rendering and the existing bounded active ticker. Use elapsed travel timestamps for scheduled demo movement, so hidden tabs and low FPS do not extend server tasks. Resume directly to the scheduled position; do not fast-forward a backlog of frames.
- Reserve work bays and shared crossing conflict zones. Stable FIFO order plus actor ID breaks ties; reserve the next zone only, release the previous zone after exit, and never hold a crossing while waiting for a desk. This avoids cyclic multi-resource holds. Cancellation/scope reset releases everything. For scheduled demo travel, Go computes non-conflicting departure times centrally (including simultaneous returns); the client follows that schedule without adding another independent reservation delay. Ordinary unscheduled presentation intents use the local runtime arbiter.
- Reservations are scene coordination, not warehouse stock locks or proof of physical resource availability. Busy desk feedback must not claim an ERP task is blocked unless its source says so.
- Bound visible actors to the existing 16 and pending truck trips to 8. Extra work remains inspectable in the HUD. Keep one moving truck initially, matching current behavior; multi-truck expansion is unnecessary for this slice.

### 4.3 Dashboard on the game page

```text
+------------------ Header / company / connection ------------------+
| KPIs                                         Camera / environment |
| Process flow            Interactive town             Detail panel |
|                                                                  |
| Selected business journey                        Work queue       |
+------------------ Business timeline / LIVE ----------------------+
```

- HUD is DOM, not geometry baked into the scene. Parent overlay uses `pointer-events: none`; each panel uses `auto`. Decorative regions pass drag/zoom through to the canvas.
- Reuse existing art tokens. Header sits above panels; modal/document desk sits above the header. World labels stay below HUD and do not steal panel clicks.
- Proposed desktop sizes: process flow 240 px, details 320 px, work queue 360 px, journey up to 720 px, outer gap 16 px. These are ERP Town decisions, not copied NuriTrack requirements.
- Reserve header/bottom-timeline space using CSS variables. At 1366×768 use compact bottom panels with expandable details; at 1920×1080 show the full composition. Below 1100 px use one active drawer and retain the existing accessible document/list fallback. Desktop usability is the acceptance priority.
- Click scene object, queue row or process step through one typed selection. Department selections focus buildings; actor/trip selections resolve current runtime positions; document selections remain permission checked. Camera follow ends on manual navigation.
- Derive counts once per changed world version/scope/replay mode. Runtime progress may refresh at most four times/second, independent of mesh updates. Reuse stable IDs; avoid remounting actor models on HUD updates.
- Empty, unauthorized, offline, reconnecting, unknown and historical states have explicit TH/EN copy. Unavailable counts show a dash, not zero. No seeded KPI values.

### 4.4 Business journeys, monitors and replay

- Model journeys as a graph because orders can have multiple deliveries/invoices/payments. Show only verified visible edges. Missing access and missing lineage do not mean a completed or absent business stage.
- Initially use the existing authorized delivery/order and payment/invoice links. Full DN→SI linkage requires Task 7's bounded DTO extension. No bulk background ERP scraping, inference by customer/time/name, or dependency on a demo's local ordered document list.
- In-world monitors show compact department queue counts/statuses from the same dashboard view. Create one 512×256 canvas texture per screen; redraw only when its visible content changes, at most twice/second. Clicking opens the matching HUD panel. Use HUD text as the keyboard/no-WebGL alternative.
- Preserve current replay limits: 64 points, 1 MiB, 15 minutes. Historical mode displays saved coarse business statuses; it does not replay workers or truck positions, issue commands, or fetch present-day document details and label them historical. Historical lineage is unavailable unless actually captured and authorized.
- Keep day/sunset/night. Procedural rain/snow, seasonal vegetation, tour automation, air/sea machinery, autonomous AMRs and Jev integration are deferred expansions, not acceptance blockers for this ERP slice.

## 5. Review focus

| Failure mode | Expected behavior | Tests owned by |
| --- | --- | --- |
| Background tab, poor frame rate, mid-route retarget or cancelled job | Bounded motion; correct scheduled pose; no teleport to old origin, stale task or stuck reservation. | 1–3 |
| Duplicate event, reconnect snapshot, version jump or stale company response | Exactly one visible dispatch; atomic current-scope data; old details cleared. | 4–5, 7, 9 |
| Many-to-many document links, partial return/payment, invisible source | Partial graph with explicit evidence; no false paid/delivered state or unauthorized count. | 7 |
| Panels expanded, long Thai copy, active label under HUD, keyboard/no-WebGL | No overlap at accepted desktop sizes; controls remain reachable; canvas receives only background input. | 6, 8 |
| Replay, consent withdrawal, permission removal or disconnected screen | No commands, historical/present mixing, employee motion history or stale private texture. | 3, 8–9 |

## 6. Sequential implementation tasks

### Task 0: Establish the execution baseline

**Files:** Read the handoff, this plan, existing client README, renderer evidence and relevant current source. No feature edits.

- [x] Recheck Git status/HEAD and progress from the owning chats; record conflicting concurrent work before proceeding.
- [x] Run client unit tests and `pnpm --filter @erp-town/town-client build`; record current failures instead of overwriting unrelated work.
- [x] Inspect existing scene anchors and model clips. Confirm a target desktop/GPU for final performance evidence; do not create duplicate art.

**Deliverable:** A recorded baseline in this plan's execution log, with no new claim of program-wide completion.

### Task 1: Shared route layout and scheduled demo travel

**Create:** `go/internal/livedemo/work-layout.json`, `go/internal/livedemo/navigation.go`, `go/internal/livedemo/navigation_test.go`.
**Modify:** `go/internal/livedemo/scenario.go`, `scenario_test.go`; `packages/town-client/src/work-navigation.ts`, `live-work.ts`, `LiveDemoPanel.tsx`, `test/work-navigation.test.ts`, `test/live-work.test.ts`.

**Interfaces:** `RouteFor(from, to string, kind string) (Route, error)`, `TravelDuration(route Route) time.Duration`, and `ScheduleTravel(requests []TravelRequest, start time.Time) ([]Travel, error)` in Go. Existing TS `pedestrianRoute`, `routeLength`, `actorStation` retain their callers. `LiveDemo` adds `layout_version: string`, `stage_started_at: string`, `stage_duration_ms: number`; `WorkActor` adds optional `travel: { started_at: string; duration_ms: number }`. Corresponding Go JSON fields carry UTC timestamps and integer milliseconds. Scheduled departures may be in the future; show queued until departure. These are local demo display fields, not world wire fields or permissions. Retain company matching and extend the explicit unknown-key allowlist.

- [x] Add failing parity tests for every supported department pair/human-AI bay; assert legal crossings, equal route lengths within 1e-6 and duration `ceil(length / speed * 1000)` ms.
- [x] Add fake-clock/cancellation tests: zero-distance travel, differing route durations, return paths, invalid layout and no ERP operation before the scheduled demo deadline. Simultaneous demo returns get stable non-overlapping travel windows when their paths share a conflict zone; the stage deadline is the latest actor arrival. Failure/cancellation clears the schedule.
- [x] Extract existing geometry into the shared layout, implement Go route timing, and replace the fixed `travel` delay. Decode timestamps/durations with safe finite bounds; accept durations from 0 to 120000 ms.
- [x] Run `go test -race ./internal/livedemo` from `go/`; run the focused client navigation/live-work tests and client build. Expected: all pass, imported layout included in the Vite build.

### Task 2: Presentation runtime and conflict-zone reservations

**Create:** `packages/town-client/src/work-simulation.ts`, `test/work-simulation.test.ts`.
**Modify:** `work-navigation.ts`, `test/work-navigation.test.ts`.

**Interfaces:** `WorkSimulation.observe(actors: readonly WorkActor[], scope: string, nowMs: number)`, `step(nowMs: number)`, `view(): WorkSimulationView`, `reset()`. View exposes stable actor IDs, phase, position, yaw, route, task reference and scene-only waiting reason; it carries no document amounts or credentials.

- [x] Test stable identity, mid-route retarget from current position, scheduled resume, unscheduled intent movement, malformed time and zero-length paths.
- [x] Test simultaneous unscheduled arrivals, FIFO ties, crossing release, cancellation while queued/travelling, and reset cleanup; assert eventual progress without ghost-through recovery. Test that a valid Go-scheduled demo route is sampled directly rather than delayed by a second client queue.
- [x] Implement cumulative-distance route sampling, phase transitions and one-next-zone reservation. Runtime completion updates presentation only; completed task state requires its source observation.
- [x] Run focused simulation/navigation tests. Expected: deterministic outcomes with injected time and no React/ERP dependency.

### Task 3: Actors perform visible work coherently

**Modify:** `packages/town-client/src/WorkCharacters.tsx`, `Characters.tsx`, `Scene.tsx`, `App.tsx`, `browser/live-work.spec.ts`, `browser/live-motion.spec.ts`.

**Interfaces:** `WorkCharacters` renders Task 2's runtime view; mesh transforms are updated from refs. Map travelling→walk, reading→read, typing→sit_type, warehouse work→carry, idle→idle; retain available-clip fallback. Keep human and AI work bays distinct.

- [x] Add browser assertions for correct department, route only while moving, arrival before working pose, idle after outcome, and cancellation/reset without a stale actor.
- [x] Verify background/resume and reduced motion with a controlled demo clock; reduced motion reaches a readable static state without requiring animation completion.
- [x] Wire the runtime, reuse existing skeletons/materials, and blend clip changes over 150 ms where supported. Do not spawn an always-working crew from document counts.
- [x] Run relevant live-work/live-motion/browser actor cases and client build. Expected: existing human consent and AI-only progress behavior retained.

### Task 4: Finite truck jobs and resource-aware transport

**Create:** `packages/town-client/src/delivery-simulation.ts`, `test/delivery-simulation.test.ts`.
**Modify:** `live-work.ts`, `TownLife.tsx`, `work-navigation.ts`, `browser/live-work.spec.ts`.

**Interfaces:** `DeliverySimulation.observe(trips, scope, nowMs)`, `step(nowMs)`, `view()`, `reset()`; phases `queued | departing | travelling | finished | cancelled`. `finished` means visual outbound travel ended. Preserve event ID identity and the existing 8-trip/one-moving-truck bounds.

- [x] Test initial/reconnect snapshots, duplicate events, cancellation, replay/reset, queue overflow and FIFO progression; assert no lost active trip or redispatch after visual completion.
- [x] Test route endpoint clamping, curvature/reverse heading, speed cap at a crossing and reservation release. Keep current outbound road and bridge geometry.
- [x] Implement route-distance progress, smooth heading and wheel motion when the existing asset exposes wheels. Label the trip as recorded dispatch; remove its route immediately on stop/completion.
- [x] Run focused delivery/live-work tests and browser dispatch cases. Assert zero ERP write requests from movement callbacks.

### Task 5: Dashboard view and one selection model

**Create:** `packages/town-client/src/dashboard-view.ts`, `test/dashboard-view.test.ts`.
**Modify:** `operations-flow.ts`, `ui-store.ts`, `App.tsx`, `Scene.tsx`, `test/operations-flow.test.tsx`.

**Interfaces:** `dashboardView({ facts, phase, replaying, company, version, work }): DashboardView`; `TownSelection` discriminates department, actor, trip and document references. Store selects/focuses these references without duplicating business data.

- [x] Test document-only queues, independent approval/stock/overdue signals, visible AI run counts, zero versus unavailable, stable ordering and source labels.
- [x] Test cancellation, completed documents not counted as active actors, permission removal, company reset, bigint version jumps and historical mode.
- [x] Derive once per immutable source change; publish runtime progress at most 4 Hz. Work lists prioritize observed failures/needs-human/overdue states, then active work, then stable ID; do not invent ETA/SLA risk.
- [x] Run focused dashboard/operations tests and existing session/world-view tests. Expected: every surface consumes the same typed view and selection.

### Task 6: Compose the desktop game HUD

**Create:** `packages/town-client/src/TownDashboard.tsx`, `dashboard.css`, `browser/dashboard.spec.ts`.
**Modify:** `App.tsx`, `styles.css`, `OperationsFlow.tsx`, `Scene.tsx`, `ui-store.ts`, existing document/agent/history panel composition as needed.

**Interfaces:** `TownDashboard` consumes `DashboardView`, `TownSelection` and explicit selection/camera/document callbacks. Reuse document forms, permission gates and Classic ERP links; the HUD does not issue commands itself.

- [x] Add browser cases for scene/list selection equality, process-step camera focus, manual camera cancelling follow, panel collapse and keyboard focus return after document inspection.
- [x] Compose the proposed desktop layout with transparent pointer pass-through, reserved header/timeline space and modal layering. Keep controls and counts accessible without WebGL.
- [x] Check 1366×768 and 1920×1080, long Thai/English copy, expanded detail/queue and a selected actor behind a panel. Below 1100 px use a single drawer. Require no overlapping interactive rectangles or clipped critical actions.
- [x] Run `browser/dashboard.spec.ts` plus existing client/documents/operations-flow cases. Capture review screenshots in both languages; geometry assertions supplement visual inspection rather than replace it.

### Task 7: Evidence-backed business journey

**Create:** `packages/town-client/src/business-journey.ts`, `BusinessJourney.tsx`, `test/business-journey.test.ts`, `browser/business-journey.spec.ts`.
**Modify:** `documents.ts`, `DocumentDesk.tsx`, `TownDashboard.tsx`.
**Conditional API files:** `go/internal/erp/types.go`, `erpnext/billing.go` and its adapter tests, `erpfake/fake.go` and its contract tests; `go/internal/gateway/fulfillment.go`, `fulfillment_test.go`.

**Interfaces:** `businessJourney(root: DocumentRef, records: readonly TownDocument[]): BusinessJourneyView`; graph nodes use company+kind+name identity, edges include their verified source. Loader reads only requested authorized references, maximum 32 nodes, concurrency 2, a 5-second read-only timeout and scope cancellation; an incomplete result stays partial. Keep the existing document command session's 45-second timeout unchanged.

- [x] Test multiple deliveries/invoices/payments, cancellations, return documents, missing access, cyclic/malformed links, node limit and stale responses. Payment Entry submission alone must not mark an invoice settled; actual authorized invoice outstanding must support that label.
- [x] Implement existing DN→SO and PE→SI edges first. Show DN→SI as unavailable, not guessed. No financial totals in world/KPI views and no background journey expansion while the panel is closed. Existing endpoints cannot discover unseen child documents: candidate nodes come only from current authorized world facts or explicitly selected/read references; explain that coverage boundary in the panel.
- [x] Prepare the bounded read-only proposal to expose `against_delivery_notes: string[]` on invoice details, derived from authorized ERP item references. Review it before implementing the public DTO/port extension; no migration or event/protobuf extension is proposed.
- [x] After explicit approval, add adapter/gateway contract regressions and strict client decoding, then verify the full graph. Without approval, retain the partial graph and report full lineage acceptance as pending.
- [x] Run focused journey/document tests, browser cases, and changed Go package tests. Expected: clear evidence/missing-edge copy and no unauthorized automatic writes.

### Task 8: Department dashboards on 3D monitors

**Create:** `packages/town-client/src/DepartmentScreen.tsx`, `screen-content.ts`, `test/screen-content.test.ts`, `browser/department-screen.spec.ts`.
**Modify:** `WorkCharacters.tsx`, `Scene.tsx`, `TownDashboard.tsx`.

**Interfaces:** `departmentScreenContent(view: DashboardView, department: Department): ScreenContent`; `DepartmentScreen` owns one canvas/texture and selects its department on click.

- [x] Test that screen counts/statuses exactly match the HUD, unavailable is not zero, replay is labeled, and company/permission removal immediately clears previous content.
- [x] Render on the existing workstation monitor plane first. Add a building board only where A's reviewed anchors support it; do not remodel buildings.
- [x] Cache the 512×256 texture; repaint at most 2 Hz for ordinary changes, immediately clear on scope loss, skip offscreen refresh, and dispose owned resources on unmount. Keep text brief; opening the HUD supplies readable detail.
- [x] Run screen tests and browser click/pixel checks across day/night, Thai and reduced motion. Verify HUD/monitor interaction does not initiate ERP commands.

### Task 9: Consistent replay, history and connection states

**Modify:** `packages/town-client/src/replay.ts`, `session.ts`, `HistoryPanel.tsx`, `TownDashboard.tsx`, `test/replay.test.ts`, `test/session.test.ts`, `browser/replay.spec.ts`.

**Interfaces:** Preserve `ReplayHistory`/`ReplayView` and their current bounds. Feed HUD and screens from the selected saved facts; reset Task 2/4 runtimes while replaying. Show business event timestamps through the existing history API.

- [x] Test LIVE→replay→LIVE, retention/permission expiry, company switch and reconnect during replay. Counts, labels and screens must reference the same source version/mode.
- [x] Keep document commands disabled in historical mode; do not fetch present-day documents or create truck jobs when selecting a historical point.
- [x] Provide a bottom history/replay control with clear LIVE return and connection state, and coordinate HUD offsets. Do not add positional human replay or claim reconstruction of missing lineage.
- [x] Run replay/session tests and integrated replay/dashboard/screen browser cases. Expected: existing 64-point/1-MiB/15-minute limits and privacy behavior remain intact.

### Task 10: End-to-end acceptance, performance and handoff

**Modify:** `packages/town-client/README.md`, this plan's execution log; relevant tests under `tests/integration/client-browser/` and existing live-work/scenario suites. Add an evidence report under `docs/` after execution, not while planning.

- [x] Run all client unit tests, `pnpm --filter @erp-town/town-client build`, and the complete isolated client browser suite sequentially, then root `pnpm lint`. Broaden Go checks only to affected packages/contracts; complete the repository checks needed for any later authorized commit.
- [x] Run the existing isolated `test.localhost` sales-to-cash acceptance with its approved identities and restore path. Confirm actual SO/DN/SI/PE status/outstanding separately from motion. Verify denied roles, uncertain writes, error/cancellation and the unchanged Classic ERP entry.
- [x] Observe at least one bounded demo round: actors reach intended bays, work is backed by calls/results, a new DN emits one finite truck trip, all surfaces agree, and all actors/vehicles eventually stop. Use actual captured requests/events, not screenshot labels alone.
- [x] Measure the accepted desktop/GPU with HUD open, moving actors, a truck and monitors. Retain budgets ≤300 draw calls and ≤750000 triangles; record active frame p95, texture count/memory, idle scheduling and sample duration. Target 30 FPS for this active slice; treat p95 >33.3 ms as a finding to fix or explicitly report, not a universal hardware guarantee.
- [x] Test lighting restoration, hidden-tab resume, no-WebGL, reduced motion and a narrow-layout smoke test. Desktop screenshots and hardware evidence remain the primary acceptance.
- [x] Update README/handoff references with exact completed tasks, observed commands, remaining API/art/pilot gates and evidence paths. Stage/commit only if separately authorized; do not edit another chat's completion record to claim its open gates passed.

## 7. Verification commands and completion criteria

Run from the repository root unless noted:

```powershell
pnpm --filter @erp-town/town-client exec vitest run test/work-navigation.test.ts test/live-work.test.ts
pnpm --filter @erp-town/town-client test
pnpm --filter @erp-town/town-client build
pnpm --filter @erp-town/town-client test:browser
pnpm lint
git diff --check
```

For each new test file, use `pnpm --filter @erp-town/town-client exec vitest run test/<file>.test.ts`. For targeted browser checks, use `pnpm --filter @erp-town/town-client exec playwright test -c playwright.config.ts browser/<file>.spec.ts`. Go checks run from `go/`, starting with `go test -race ./internal/livedemo`; add only affected adapter/gateway packages. Existing live ERP acceptance commands and destructive seed/restore protections must be read from their current harness before running them.

The slice is accepted when R2–R10 pass in a desktop live test, queues/journeys/screens are evidence-backed, and no scope/privacy/command regression remains. If invoice-lineage approval has not been supplied, core HUD/motion may be accepted separately, with the complete business journey explicitly pending. Open A art gates, production pilot restore drills, external model integration and deferred F remain program-level work; they do not disappear when this slice passes.

## 8. Plan self-review and execution log

Plan self-review performed at authoring: all conversation requirements are mapped; task interfaces have named consumers/producers; all five review-focus failure classes have owning regression work; existing implementations are reused; full lineage's missing contract is visible rather than inferred. No feature code was changed and no runtime tests were run for this planning request.

| Task | Status | Observed verification / remaining issue |
| --- | --- | --- |
| 0 | Done | Existing checkout/other-chat progress and art ownership rechecked; baseline 112 client tests/build passed. Unrelated changes preserved. |
| 1 | Done | Shared layout, 72 route parity rows, bounded UTC schedules, fake-clock deadlines and independent snapshots; Go race and client decoding/build passed. |
| 2 | Done | Bounded mutable runtime, current-pose retargeting, binary-search sampling, work bays, crossing release and scheduled pedestrian priority; deterministic tests passed. |
| 3 | Done | Real clips blend over 150 ms; live movement/arrival/working/idle pixels and controlled hidden reduced-motion resume passed. |
| 4 | Done | Eight admitted finite jobs, one moving truck, FIFO/cancellation/endpoint/crossing and event-reappearance regressions passed. Baked truck has no independent wheel nodes. |
| 5 | Done | Immutable authorized business projection and 4-Hz presentation; independent counts, bigint versions, scoped selection and historical/unavailable semantics passed. |
| 6 | Done | Desktop bilingual HUD at 1366×768 and 1920×1080, shared scene/list selection, manual follow stop, focus return and one narrow drawer passed. |
| 7 | Done | User approval included the read-only invoice-source DTO. Adapter/gateway/fake/client tests, bounded streamed reads and verified branching graph passed; browser replay performs no current reads. |
| 8 | Done | Existing monitor planes show shared content, repaint at most 2 Hz, clear/dispose on scope loss; actual monitor raycast, Thai/night and reduced-motion browser case passed. No new building board/anchor was required. |
| 9 | Done | Existing bounded replay/history reused; scope/permission/reconnect cleanup and historical command/read isolation passed. Human positional replay was not added. |
| 10 | Done | 138 client unit tests, 33 isolated browser cases, production build, affected Go race packages, root lint (278 files/0 Go issues) and whitespace check passed. Real two-case ERP acceptance and one bounded observed demo completed and restored the test site. |


Execution decisions and final evidence:

- Approved on 2026-10-12 by the user's instruction to develop through completion and make implementation decisions, including Task 7's specified read-only invoice-lineage extension.
- Scheduled demo returns use conservative whole-route serialization; ordinary unscheduled work uses local crossing/work-bay reservations. Movement never invokes ERP commands.
- Existing asset limitations are retained: no independent truck wheels; monitors use existing workstations; no new building anchors or canonical art edits.
- Author review replaces the optional execution skill's fresh-agent workflow because this plan expressly requires sequential execution without parallel agents. Review checked R1–R12, scope/privacy, command isolation, resource cleanup and the final observed tests. No independent reviewer is claimed.
- RTX 3080 Ti/installed Chrome active fixture: 4,141 samples over 17.25 seconds, 270 peak calls, 368,028 triangles, p95 4.3 ms; textures 27→9 after scope loss, idle six demand frames/three seconds. Total GPU byte allocation, maximum-population and mobile stress remain unmeasured, with monitor-only memory estimates labeled.
- The actual ERP observation records four submitted documents, invoice outstanding `0.0`, one dispatch ID, zero browser business writes/page errors and four idle actors/no truck at completion. The launcher stopped gracefully; both acceptance/demo snapshots were restored.
- Final report: [simulation/dashboard evidence](../../town-simulation-dashboard-evidence-2026-10-12.md). README and the existing program handoff have additive continuation records; prior records and unrelated uncommitted work remain intact. No commit/push/deployment was performed.
