# Town client (D)

2026-10-10: the user authorized continuing the remaining ERP Town program and
deferred F (PromptPay/LINE). This document narrows the first D increment to C's
existing binary protocol before adding renderer dependencies. A remains the
existing Sunny Office Town art source in `D:/ERP_Town-art-direction/`; its open
score/gates are not implicitly approved by implementation progress.

## State and connection contract

- Consume the generated `world.v1.ServerFrame` contract without changing the proto
  or any generated files. Room versions stay `bigint` end to end.
- Keep one in-memory state per selected company. The first frame is a snapshot,
  unless reconnect retained the exact same state and version. A snapshot replaces
  all prior entities, including private facts that lost visibility.
- A patch applies only against the current `baseVersion`; the room version can
  advance by more than one because C coalesces updates at 10 Hz. A resume patch
  can keep the same room version while rotating its token.
- Validate the complete frame before replacing state. Reject another company,
  unknown frame kind, missing/invalid keys, duplicate/conflicting mutations and
  entity versions beyond the room version. A gap or invalid frame clears displayed
  state and the resume token and requires a new full snapshot.
- Identify entities by all four key fields: doctype, name, kind and scope. Stock
  signals in two warehouses and approval/document facts remain independent.
- Store payload bytes as opaque server-filtered facts; never fetch ERP financial
  fields through world sync. Clone input/output bytes and keys so consumers cannot
  mutate stored state. Renderer mapping will whitelist display fields separately.
- JOIN carries JWT only in its first binary frame. No credentials or resume tokens
  in URLs, browser persistence, logs or diagnostic payloads. Session/company/logout
  changes clear state. No client intent can invoke an ERP command.
- Connection wiring must reject late callbacks from a previous socket, cap retry
  delays, distinguish authentication failure from transient disconnection and
  avoid automatically retrying forbidden sessions. Actual reader provisioning and
  live credential use remain protected operations.

## Scene and ERP UI

Use the program's approved Vite/React 19/R3F 9/Three.js, Drei, koota, Zustand and
Motion stack. No dependency has been added by writing this design. Reuse A's
GLBs, roof clips, metre scales, anchors, robot/human distinction, palette, fonts
and bilingual copy. Keep the offline review separate from the live client.

The first working slice is Shop → Warehouse → Bank, with an orthographic camera,
selection/object-list alternatives and accessible document inspection. Transactions
use the existing gateway and ERP permissions/audit, not the WebSocket. Projection
facts cannot justify displaying fixture amounts as live balances. Loading, no
permission, connection loss and empty state must be explicit.

Measure actual WebGL behavior before claiming performance. Reduced motion opens
roofs directly, skips camera flights and retains static feedback. Keyboard/touch
controls, Thai/English copy and Classic alternatives remain required.

Document desk increment, 2026-10-11: transactions stay on the cookie-authenticated
gateway with the existing CSRF header, never on the world socket. Create draft,
read its ERP details, and confirm are separate actions. Fulfillment creates carry
the selected company and a submitted source, with source company/status checked
before writes. Payment records the full outstanding invoice balance; partial
operations, cancellation and returns retain Classic ERP alternatives.

Amounts remain server decimal strings in the document currency. A false submit
acknowledgement means a workflow step, not a submitted document. Serialize commands,
bound requests and ignore stale responses. A lost write acknowledgement requires
checking ERP before another write; reconnect cannot silently clear that warning.
Clear private details on company/session/connection changes. Use the same forms
for keyboard, mobile and no-WebGL users, with natural Thai/English copy and
focus returned to the retrieved document heading.

## Dependency proposal (approved and installed)

The user explicitly approved this proposal and related package/root/lockfile edits
on 2026-10-10. Exact versions below are installed, plus the existing internal
`@erp-town/world-proto` workspace link. No provider dependencies were added.

Read-only npm registry metadata was checked on 2026-10-10. These exact versions
fit the approved React 19 / R3F 9 stack and the installed Node 26.8.2:

| Runtime | Version | Development tool/types | Version |
| --- | --- | --- | --- |
| react | 19.3.0 | vite | 8.3.4 |
| react-dom | 19.3.0 | @vitejs/plugin-react | 6.1.2 |
| three | 0.186.1 | @types/react | 19.3.0 |
| @react-three/fiber | 9.8.1 | @types/react-dom | 19.3.0 |
| @react-three/drei | 10.7.9 | @types/three | 0.186.0 |
| zustand | 5.0.15 | | |
| koota | 0.6.6 | | |
| motion | 14.1.0 | | |

The approval covers these dependencies, `packages/town-client/package.json`,
associated workspace/root scripts and `pnpm-lock.yaml` under AGENTS.md.
No remote schema upload, provider call, deployment or credential change is part
of that proposed local installation. Provider/runtime dependencies for E are a
separate later proposal once its policy design is concrete.
