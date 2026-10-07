# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Correlation / causation of document events (spec §3.1, §4.2 step 3).

- submitted: the first upstream document (mapper row order) that has an event gives correlation and
  causation; `against` lists every upstream name. No upstream event → the document is its own root.
- amended documents (`amended_from`) keep the original chain: correlation from the cancelled original's
  latest event; causation from the first upstream event, else from the original's latest event.
- cancelled: chained to the document's own latest event (same correlation, causation = that event).

Only events consumers can see count as "latest": `Invalid` rows (never published) and `system.*` events
(problem reports that share the document as subject) are skipped.
"""

from typing import NamedTuple

import frappe

from erp_town.events.mappers import CANCELLED, SUBMITTED, Mapper


class Lineage(NamedTuple):
	correlation_id: str
	causation_id: str | None
	against: list[str]


def latest_event(doctype: str, name: str) -> dict | None:
	"""`{"event_id", "correlation_id"}` of the subject's highest-seq published-or-pending event."""
	if not name:
		return None
	rows = frappe.db.sql(
		"""select event_id, correlation_id from `tabTown Event Outbox`
		where subject_doctype = %s and subject_name = %s
			and status != 'Invalid' and event_type not like 'system.%%'
		order by seq desc limit 1""",
		(doctype, name),
		as_dict=True,
	)
	return {"event_id": rows[0].event_id, "correlation_id": rows[0].correlation_id} if rows else None


def find_lineage(doc, mapper: Mapper, action: str = SUBMITTED) -> Lineage:
	if action == CANCELLED:
		own = latest_event(doc.doctype, doc.name)
		if own:
			return Lineage(own["correlation_id"], own["event_id"], [])
		return find_lineage(doc, mapper)._replace(against=[])

	upstream = mapper.upstream(doc)
	against = [name for _, name in upstream]
	cause = next(filter(None, (latest_event(dt, name) for dt, name in upstream)), None)
	original = latest_event(doc.doctype, doc.get("amended_from")) if doc.get("amended_from") else None

	if original:
		causation = cause["event_id"] if cause else original["event_id"]
		return Lineage(original["correlation_id"], causation, against)
	if cause:
		return Lineage(cause["correlation_id"], cause["event_id"], against)
	return Lineage(doc.name, None, against)
