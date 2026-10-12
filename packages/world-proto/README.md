# World protocol

Proprietary, shared binary WebSocket protocol for ERP Town C and D. Source of truth:
`proto/world/v1/world.proto`. JWTs travel in the first JOIN frame, never URL parameters.
All room data must be company/visibility filtered before serialization. Versions are
`uint64` / TypeScript `bigint`; never convert them to JavaScript `number`.

Run `pnpm world:gen` from the repository root. Buf and both plugins run locally; no
remote schema/plugin upload. Generated paths are `go/internal/worldpb/world/v1/`
and `packages/world-proto/src/gen/`. Do not edit generated code by hand.

## Client state

`WorldClientState(company)` consumes binary `ServerFrame` snapshots/patches with
`acceptBinary(bytes)` or decoded frames with `accept(frame)`. It preserves bigint
versions, all four entity-key fields and visibility removals. Coalesced version
jumps are valid when the base matches; equal-version resume frames can rotate
the token without mutations. A fresh snapshot replaces the complete prior view.

`entities()` returns owned keys/payload bytes; `reconnectCursor()` returns a copied
memory-only cursor. Payload bytes remain opaque server-filtered facts, not an
ERP document or authorization to expose financial fields. Never persist tokens
or put them in URLs. Reset on session/company changes or logout.

Invalid binary/frame data, a version gap or a company mismatch clears the view
and throws `WorldSyncError` with a fixed reason. Rejoin for a complete snapshot;
do not display the cleared facts as a current connection. This module performs
no networking, credential provisioning, storage or ERP writes.

## Connection lifecycle

`WorldConnection(url, factory)` accepts an injected browser WebSocket factory,
for example `url => new WebSocket(url)`. Call `connect({company, jwt})` after
obtaining a session through the existing authenticated gateway. Credentials and
resume cursors stay in memory; this helper does not provision or refresh them.

`view()` returns an owned, token-free view. Only `live` exposes facts; `connecting`,
`offline` and `auth-required` hide stale facts. `subscribe(listener)` returns an
unsubscribe function. `disconnect()` cancels timers and clears the session;
`connect()` replaces it and ignores all callbacks from the previous socket.

Transient disconnections resume the same company's exact in-memory state. Invalid
binary data, a gap or C's `1008 / resync required` response clears the cursor and
rejoins for a snapshot. Other policy closes stop retries and clear credentials;
the host must obtain a permitted session and explicitly call `connect()` again.
Do not present a reconnecting state as a verified empty business backlog.

Retry delay doubles from 250 ms to a 5 s cap. Network opening is bounded to 15 s,
then the first valid world frame must acknowledge JOIN within 5 s. `disconnect()`
prevents subsequent automatic retries, including late close/open callbacks.
Other remote closes retry; the browser owns WebSocket ping/pong control frames.

`displayIntent(name, target)` sends only `camera.focus` or `avatar.emote` with C's
128-byte UTF-8 target bound while live, and returns whether it was sent. It never
queues actions offline. ERP document commands must use the gateway instead.

Lifecycle tests use injected EventTarget sockets with real protobuf bytes. A
type check verifies the native WebSocket adapter contract; these checks do not
claim a browser/Go network handshake, live ERP acceptance or WebGL performance.
