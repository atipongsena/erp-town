# Agent runtime core implementation

Design: [agent-runtime-core-design](../specs/2026-10-11-agent-runtime-core-design.md).
Execute sequentially in the current checkout; retain pre-existing changes.
No dependencies, migrations, auth configuration, provider calls or commits.

- [x] Inspect Git, program ownership, E boundaries and the existing Port.
- [x] Define the local provider/executor/audit contract and observe failing
  behavior tests before implementing the loop.
- [x] Implement bounded turns, tool dispatch, replay and stop outcomes.
- [x] Check payload isolation, cancellation, failed adapter behavior and reuse.
- [x] Run focused race/leak tests and required lint; update the program handoff
  with observed results and the remaining operational gates.

The complete E subsystem remains outstanding. This checklist covers its local
run-loop prerequisite only; provider/policy/audit adapters are not mock features
presented as a connected ERP service.

That paragraph describes the original core checkpoint. The user subsequently
approved local policy, durable queue/audit, native SDK/MCP contracts and AI events;
their current evidence and remaining activation gates are in
`2026-10-11-program-completion.md` and `../../local-runtime-operations.md`.

Observed: `go -C go tool gotestsum --format pkgname -- -race ./internal/agents -count=1 -timeout 30s` passed 40 tests in 1.467 s with goleak. Root `pnpm lint`
passed with 158 Biome files and 0 Go issues. This new package has no production
caller, so existing Go/ERP/browser suites were not rerun; the preceding full Go
checkpoint remains the 436-test outbox increment. Initial behavioral and subsequent
panic regressions were observed failing. No protected operational change occurred.
