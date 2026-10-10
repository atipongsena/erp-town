# World protocol

Proprietary, shared binary WebSocket protocol for ERP Town C and D. Source of truth:
`proto/world/v1/world.proto`. JWTs travel in the first JOIN frame, never URL parameters.
All room data must be company/visibility filtered before serialization. Versions are
`uint64` / TypeScript `bigint`; never convert them to JavaScript `number`.

Run `pnpm world:gen` from the repository root. Buf and both plugins run locally; no
remote schema/plugin upload. Generated paths are `go/internal/worldpb/world/v1/`
and `packages/world-proto/src/gen/`. Do not edit generated code by hand.
