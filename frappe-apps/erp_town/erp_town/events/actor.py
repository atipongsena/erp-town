# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Who caused an event (spec §4.2 step 2)."""

import frappe

ON_BEHALF_OF_HEADER = "X-Town-On-Behalf-Of"
ON_BEHALF_OF_REJECTED = "ON_BEHALF_OF_REJECTED"
ON_BEHALF_OF_INVALID = "ON_BEHALF_OF_INVALID"


def system_scheduler_actor() -> dict:
	return {"kind": "system", "id": "system.scheduler", "on_behalf_of": None}


def system_town_actor() -> dict:
	return {"kind": "system", "id": "system.town", "on_behalf_of": None}


def resolve_actor(default_kind: str = "user") -> dict:
	"""`{"kind", "id", "on_behalf_of"}` for the current session (header problems are not reported)."""
	return resolve_actor_with_issues(default_kind)[0]


def resolve_actor_with_issues(default_kind: str = "user") -> tuple[dict, list[tuple[str, str]]]:
	"""The actor plus `(code, message)` problems to report as `system.error` (never blocks).

	- `frappe.flags.town_system_actor` (scheduled jobs) → `system.scheduler`
	- `default_kind == "system"` → `system.town`
	- session user mapped to an enabled `Town Agent Identity` → `agent` with `id = agent_key`; the
	  `X-Town-On-Behalf-Of` header (a User name) becomes `on_behalf_of = "user.<name>"`, an unknown user
	  gives `on_behalf_of = None` + `ON_BEHALF_OF_INVALID`
	- anyone else → `<default_kind>.<User name>`; a sent header is ignored + `ON_BEHALF_OF_REJECTED`
	"""
	if frappe.flags.town_system_actor:
		return system_scheduler_actor(), []
	if default_kind == "system":
		return system_town_actor(), []

	user = frappe.session.user
	header = _on_behalf_of_header()
	agent_key = frappe.db.get_value("Town Agent Identity", {"user": user, "enabled": 1}, "agent_key")

	if agent_key:
		actor = {"kind": "agent", "id": agent_key, "on_behalf_of": None}
		if header is None:
			return actor, []
		if on_behalf_of := frappe.db.exists("User", header):  # canonical name (lookup is case-insensitive)
			actor["on_behalf_of"] = f"user.{on_behalf_of}"
			return actor, []
		return actor, [
			(
				ON_BEHALF_OF_INVALID,
				f"{ON_BEHALF_OF_HEADER} from {agent_key} names a user that does not exist; on_behalf_of null",
			)
		]

	actor = {"kind": default_kind, "id": f"{default_kind}.{user}", "on_behalf_of": None}
	if header is None:
		return actor, []
	return actor, [
		(
			ON_BEHALF_OF_REJECTED,
			f"{ON_BEHALF_OF_HEADER} sent by a session that is not an agent; header ignored",
		)
	]


def _on_behalf_of_header() -> str | None:
	"""The header value, or None outside an HTTP request (jobs, console, tests) or when blank."""
	if getattr(frappe.local, "request", None) is None:
		return None
	try:
		value = frappe.get_request_header(ON_BEHALF_OF_HEADER)
	except Exception:  # noqa: BLE001 -- no usable request object: same as "header not sent"
		return None
	value = (value or "").strip()
	return value or None
