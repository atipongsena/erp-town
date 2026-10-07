# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Correlation / causation of document events (spec §3.1, §4.2 step 3).

- submitted: the first upstream document (mapper row order) that has an event gives correlation and
  causation; `against` lists every upstream name. No upstream event → the document is its own root.
- amended documents (`amended_from`) keep the original chain: correlation from the cancelled original's
  latest event; causation from the first upstream event, else from the original's latest event.
- cancelled: chained to the document's own latest event (same correlation, causation = that event).

`system.*` events (problem reports that share the document as subject) never count as a document's
latest event. `Invalid` rows do (Ruling H): they still carry the flow root as correlation, so the chain
keeps its root, but they are never published, so they give no causation (`causation_id = None`).
"""

from typing import NamedTuple

import frappe

from erp_town.events.emit import INVALID
from erp_town.events.mappers import CANCELLED, SUBMITTED, Mapper


class Lineage(NamedTuple):
	correlation_id: str
	causation_id: str | None
	against: list[str]


def latest_event(doctype: str, name: str) -> dict | None:
	"""The subject's highest-seq non-`system.*` outbox row, whatever its status, or None.

	Returns `{"event_id", "correlation_id", "invalid"}`; `invalid` is True for an `Invalid` row, which is
	never published: use `causation_of()` rather than `event_id` as a causation.
	"""
	if not name:
		return None
	rows = frappe.db.sql(
		"""select event_id, correlation_id, status from `tabTown Event Outbox`
		where subject_doctype = %s and subject_name = %s and event_type not like 'system.%%'
		order by seq desc limit 1""",
		(doctype, name),
		as_dict=True,
	)
	if not rows:
		return None
	row = rows[0]
	return {"event_id": row.event_id, "correlation_id": row.correlation_id, "invalid": row.status == INVALID}


def causation_of(event: dict | None) -> str | None:
	"""`event_id` of a `latest_event()` result usable as causation (None for missing or `Invalid`)."""
	return event["event_id"] if event and not event["invalid"] else None


def find_lineage(doc, mapper: Mapper, action: str = SUBMITTED) -> Lineage:
	if action == CANCELLED:
		own = latest_event(doc.doctype, doc.name)
		if own:
			return Lineage(own["correlation_id"], causation_of(own), [])
		return find_lineage(doc, mapper)._replace(against=[])

	upstream = mapper.upstream(doc)
	against = [name for _, name in upstream]
	cause = next(filter(None, (latest_event(dt, name) for dt, name in upstream)), None)
	original = latest_event(doc.doctype, doc.get("amended_from")) if doc.get("amended_from") else None

	if original:
		return Lineage(original["correlation_id"], causation_of(cause or original), against)
	if cause:
		return Lineage(cause["correlation_id"], causation_of(cause), against)
	return Lineage(doc.name, None, against)
