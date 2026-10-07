# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""JSON Schema validation of town events against the generated copy in `./schema/` (spec §3.4).

This module shadows the `schema/` data directory as an import name (a plain module wins over a
directory without `__init__.py`); the directory holds only generated JSON and is never imported.
"""

import json
from functools import cache
from pathlib import Path

from jsonschema import Draft202012Validator
from jsonschema.exceptions import best_match
from referencing import Registry, Resource

SCHEMA_DIR = Path(__file__).resolve().parent / "schema"


class EventSchemaError(Exception):
	"""An event does not match the envelope / per-type data schema."""

	def __init__(self, message: str):
		super().__init__(message)
		self.message = message


@cache
def get_validator() -> Draft202012Validator:
	"""Envelope validator with every per-type schema registered by `$id`; built once per process."""
	paths = [SCHEMA_DIR / "envelope.json", *sorted((SCHEMA_DIR / "events").glob("*.json"))]
	schemas = [json.loads(p.read_text(encoding="utf-8")) for p in paths]
	registry = Registry().with_resources((s["$id"], Resource.from_contents(s)) for s in schemas)
	envelope = schemas[0]
	Draft202012Validator.check_schema(envelope)
	# `date-time` is only asserted when rfc3339-validator is installed; `time` also carries a pattern.
	return Draft202012Validator(
		envelope, registry=registry, format_checker=Draft202012Validator.FORMAT_CHECKER
	)


def validate_event(event: dict) -> None:
	"""Raise `EventSchemaError` with the most relevant schema violation, if any."""
	error = best_match(get_validator().iter_errors(event))
	if error is not None:
		raise EventSchemaError(f"{error.json_path}: {error.message}")
