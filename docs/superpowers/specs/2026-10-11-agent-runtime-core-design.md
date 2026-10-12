# Agent runtime core

Date: 2026-10-11. Scope: the local provider-independent prerequisite for E,
within the continuing A/B/C/D/E/G/H work. This does not approve production
identity/policy configuration, persisted schemas or provider calls.

## Purpose and boundary

Agents must read and draft through the same controlled ERP tools as other users.
The approved Go platform design separates provider adapters, a tool registry,
the policy proxy, durable audit and River jobs. This increment implements the
run loop without enabling any of those operational integrations.

`go/internal/agents` imports no ERP adapter and exposes no HTTP/MCP endpoint or
process role. The injected provider receives instructions, advertised tool
definitions, bounded history and its remaining token allowance. There is no
credential, principal or ERP Port in the provider contract. Only the injected
executor can run advertised tool intents. A production executor must be the
policy proxy, with trusted server-side identity/company binding; a raw ERP
adapter must never be supplied as the executor.

## Run contract

- A Run object is single-use, including concurrent execution attempts. Reusing
  its id in another object is not durable idempotency.
- Default ceilings: 8 provider turns, 16 unique tool executions, 12,000 reported
  input/output tokens, 64 KiB per request/response/result and a 2-minute context
  deadline. These local safety ceilings are not an approved provider budget.
- Provider/executor/audit implementations must honor context deadlines. The loop
  does not detach uncooperative work into unbounded goroutines.
- Validate a whole tool batch before any execution: advertised names, opaque
  identifiers, object-shaped JSON arguments, unique call meanings and remaining
  call/token limits. Tool-specific JSON Schema and business checks belong to
  the future policy executor. Malformed/unknown/conflicting intents need a human.
- Cache successful tool outcomes within the run. An identical replayed call id
  reuses its result; changing its tool or compacted JSON arguments stops the run.
  JSON whitespace is ignored, but object key ordering is intentionally not
  normalized. Provider/tool inputs are copied to preserve run state.
- Record metadata-only audit intent before the tool and outcome afterward.
  An unavailable audit stops dispatch. A tool error/malformed reply is uncertain:
  stop immediately, do not retry it or run the rest of the batch. A human-review
  outcome also stops further tools/provider turns.
- After a tool may have committed, outcome audit outlives run cancellation with
  its own 5-second cleanup deadline. Cancellation before a tool records a cancelled
  intent; cancellation with a known successful reply records completion and stops
  the remaining batch. An uncertain reply needs a human, even during shutdown.
- Contain provider/executor/audit panics at their interface boundary, including
  panic(nil), using the same failure/uncertain outcomes without raw panic payloads.
- Return structured completed/needs_human/cancelled/failed results. Raw adapter
  errors, prompts, arguments and tool output are not written to metadata audit
  entries or error summaries. Model final text is task content, not permission
  to perform an action.

## Verification and remaining E scope

Use scripted local providers/executors and an in-memory journal to verify model
turns, exact decimal strings, whole-batch validation, replay, limits, uncertain
outcomes, audit failure, cancellation and concurrent reuse. No model invocation
or ERP business write is needed for these tests.

Before this kernel is connected to production: approve Go-owned subject names
and agent events; identity/company/on_behalf_of binding and policy values;
durable run claims and audit (including restart/ambiguous-write recovery); River
queue schema/dependency changes; provider SDK versions, credentials, disclosure
and spending limits; tool adapters and MCP; then real test-site acceptance.
No `agent.*` schema change, Go outbox emitter or private world-owner mapping is
introduced here. Current UI and E production readiness remain unchanged.

Observed local checks: 40 tests passed with `-race -count=1` and goleak in 1.467 s;
root `pnpm lint` passed (158 Biome files, 0 Go issues). The initial run-loop tests
and later adapter-panic regressions were observed failing before their fixes.
No real ERP/provider, restart recovery or pilot acceptance is claimed.
