# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""`system.error` events (spec §3.3): town-layer problems, visible to System Manager only."""

from erp_town.events.actor import system_town_actor
from erp_town.events.emit import emit_raw

SYSTEM_ERROR_SOURCE = "erp/town"
SYSTEM_ERROR_ROLES = ["System Manager"]


def emit_system_error(
	code: str,
	message: str,
	*,
	subject_doctype: str,
	subject_name: str,
	company: str | None = None,
	causation_id: str | None = None,
) -> str | None:
	"""Emit `system.error` with `data = {code, message}` (message without stack trace).

	Subject is the offending document when known; publisher-level errors use
	`{doctype: "Town Event Outbox", name: <event_id>}`. If the error event itself is invalid it is only
	written to the Error Log (no further `system.error`), and None is returned.
	"""
	message = str(message or "").strip() or str(code)
	return emit_raw(
		"system.error",
		source=SYSTEM_ERROR_SOURCE,
		subject_doctype=subject_doctype,
		subject_name=subject_name,
		company=company,
		data={"code": code, "message": message},
		actor=system_town_actor(),
		causation_id=causation_id,
		visibility={"level": "roles", "roles": list(SYSTEM_ERROR_ROLES)},
	)
