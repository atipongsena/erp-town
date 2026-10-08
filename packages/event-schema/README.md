# @erp-town/event-schema

The ERP Town event contract (spec: `docs/superpowers/specs/2026-10-07-foundation-design.md` §3):
JSON Schema 2020-12 for the envelope and each event type's `data`, golden fixtures, generated TS types
and an ajv validator.

- `schema/envelope.json` — envelope (CloudEvents 1.0 based); `schema/events/<type>.json` — `data` per type
- `fixtures/valid/*.json`, `fixtures/invalid/*.json` — golden examples; the TS contract test (ajv) and the
  Python test (`erp_town.events.schema.validate_event`) run over the same files
- `src/generated.ts` — generated types; `src/index.ts` — `validateEvent()`, `createAjv()`

Edit only `schema/` and `fixtures/`, then run `pnpm schema:gen` from the repo root: it regenerates
`src/generated.ts` and copies schema + fixtures into `frappe-apps/erp_town/erp_town/events/schema/`
(never edit either by hand; `test/drift.test.ts` fails when the copy is stale). Changes are additive
only; anything else bumps `schema_version`.

## Envelope rules consumers rely on

- `id` (ULID) is the idempotency key: skip ids already seen (delivery is at-least-once).
- `company` is the company **code** (ERPNext Company `abbr`, e.g. `ACME-TH`), not the Company record
  name; `null` only for `system.*` events and subjects without a company.
- `seq` starts at 1 per subject (`subject.doctype` + `subject.name`) and is shared by **all** event
  types and statuses of that subject. **Gaps are expected**: an event stored `Invalid` takes a seq but
  is never published, and `system.error` events are visible to System Manager only. Order a subject's
  events by `seq`, but never wait for a missing `seq`.
- `correlation_id` is the flow's root document; `causation_id` the event that caused this one (or null).
- Public events never carry money fields (`no_money_in_public`).
