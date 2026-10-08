# GENERATED — edit packages/event-schema

Do not edit files in this directory. They are copied from `packages/event-schema/` by
`pnpm schema:gen` (run from the repo root); edit the source there and re-run the command.

- `envelope.json` — event envelope (JSON Schema 2020-12), `$id` `https://erp-town.local/schema/envelope.json`
- `events/<type>.json` — `data` schema per event type, `$id` `https://erp-town.local/schema/events/<type>.json`
- `fixtures/valid/*.json`, `fixtures/invalid/*.json` — golden examples shared with the TS contract test

The envelope references the per-type schemas by relative `$ref` (`events/<type>.json`), which
resolves against its `$id`. A Python consumer must therefore register every schema in a
`referencing` registry (keyed by `$id`) before validating:

```python
import json
from pathlib import Path

from jsonschema import Draft202012Validator
from referencing import Registry, Resource

SCHEMA_DIR = Path(__file__).parent / "schema"  # this directory


def load_validator() -> Draft202012Validator:
    paths = [SCHEMA_DIR / "envelope.json", *sorted((SCHEMA_DIR / "events").glob("*.json"))]
    schemas = [json.loads(p.read_text(encoding="utf-8")) for p in paths]
    registry = Registry().with_resources(
        (s["$id"], Resource.from_contents(s)) for s in schemas
    )
    envelope = schemas[0]
    Draft202012Validator.check_schema(envelope)
    return Draft202012Validator(
        envelope,
        registry=registry,
        format_checker=Draft202012Validator.FORMAT_CHECKER,
    )
```

Envelope rules (see `packages/event-schema/README.md`): `company` is the company code (Company
abbr); `seq` is per subject across all event types and statuses and has expected gaps (Invalid rows
are never published, `system.error` is role-restricted), so consumers never wait for a missing seq.

`format` assertions are optional in Python (`date-time` needs `rfc3339-validator`); the schemas
also carry `pattern`s for `time` and dates so validation results match ajv either way.
