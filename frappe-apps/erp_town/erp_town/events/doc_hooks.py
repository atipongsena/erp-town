# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""`doc_events` hook for the sales slice (spec §4.2): document submit/cancel → outbox event.

Never blocks the document: a mapper failure stores the event `Invalid` (via `emit_raw(invalid_reason=…)`),
any other failure while building the event is reported as `system.error` `EVENT_INVALID`. `emit_raw` itself
swallows town-layer errors; it only re-raises when the database already lost the surrounding transaction,
which must reach the caller (see `erp_town.events.emit`).
"""

from erp_town.events.emit import EVENT_INVALID, emit_raw, log_failure
from erp_town.events.lineage import find_lineage
from erp_town.events.mappers import CANCELLED, REGISTRY, SUBMITTED
from erp_town.events.system import emit_system_error

ACTIONS = {"on_submit": SUBMITTED, "on_cancel": CANCELLED}


def on_doc_event(doc, method: str) -> None:
	action = ACTIONS.get(method)
	mapper = REGISTRY.get(doc.doctype)
	if action is None or mapper is None:
		return
	event_type = f"{mapper.type_prefix}.{action}"
	try:
		if not mapper.applies(doc):
			return
		lineage = find_lineage(doc, mapper, action)
		data, invalid_reason = _data(doc, mapper, action, lineage.against, event_type)
	except Exception as e:  # noqa: BLE001 -- town-layer failures must never block the document
		_report_failure(doc, event_type, e)
		return

	emit_raw(
		event_type,
		source=mapper.source,
		subject_doctype=doc.doctype,
		subject_name=doc.name,
		company=doc.get("company"),
		data=data,
		correlation_id=lineage.correlation_id,
		causation_id=lineage.causation_id,
		invalid_reason=invalid_reason,
	)


def _data(doc, mapper, action: str, against: list[str], event_type: str) -> tuple[dict, str | None]:
	try:
		data = mapper.data(doc, action)
	except Exception as e:  # noqa: BLE001 -- stored as an Invalid event instead
		log_failure(f"ERP Town: mapper for {event_type} failed on {doc.doctype} {doc.name}")
		return {}, f"mapper failed: {_describe(e)}"
	if action == SUBMITTED and mapper.lists_against:
		data["against"] = list(against)
	return data, None


def _report_failure(doc, event_type: str, error: Exception) -> None:
	log_failure(f"ERP Town: building {event_type} failed on {doc.doctype} {doc.name}")
	emit_system_error(
		EVENT_INVALID,
		f"could not build {event_type} for {doc.doctype} {doc.name}: {_describe(error)}",
		subject_doctype=doc.doctype,
		subject_name=doc.name,
		company=doc.get("company"),
	)


def _describe(error: Exception) -> str:
	return f"{type(error).__name__}: {error}"
