# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""`emit_raw`: write one event into `Town Event Outbox` inside the caller's transaction (spec §4.2).

Town-layer failures never propagate to the caller (a document submit/cancel must not be blocked):
- schema-invalid or non-JSON events, and events the caller already knows are broken (`invalid_reason`,
  e.g. the mapper raised), are stored as `Invalid` rows and reported as `system.error` `EVENT_INVALID`;
- anything unexpected rolls back the town-layer writes (seq + outbox) to a savepoint, goes to the Error
  Log and `emit_raw` returns None; the caller's own writes in the same transaction are untouched.
The one exception: if that savepoint is gone (e.g. a deadlock made the server roll back the whole
transaction) the original error is re-raised, so the caller does not carry on in a transaction that
no longer holds its earlier writes.
"""

import json

import frappe
from frappe.utils import now_datetime
from frappe.utils.response import json_handler

from erp_town.events.actor import resolve_actor_with_issues
from erp_town.events.envelope import build_envelope, default_visibility
from erp_town.events.schema import EventSchemaError, validate_event
from erp_town.events.seq import next_seq
from erp_town.events.ulid import new_ulid

PUBLISH_METHOD = "erp_town.events.publisher.publish"
SYSTEM_ERROR = "system.error"
EVENT_INVALID = "EVENT_INVALID"
PENDING = "Pending"
INVALID = "Invalid"
_MAX_ERROR_MESSAGE = 1000


class InvalidSystemErrorEvent(Exception):
	"""A `system.error` event failed validation; it is logged, never re-reported (no recursion)."""


def emit_raw(
	event_type: str,
	*,
	source: str,
	subject_doctype: str,
	subject_name: str,
	company: str | None,
	data: dict,
	actor: dict | None = None,
	correlation_id: str | None = None,
	causation_id: str | None = None,
	visibility: dict | None = None,
	invalid_reason: str | None = None,
) -> str | None:
	"""Store an event and register its publish job; return the event id (also for `Invalid` rows).

	Defaults: `actor` from the session (`resolve_actor`, header problems reported as `system.error`),
	`correlation_id = subject_name`, `visibility = {"level": "public", "roles": []}`.
	`invalid_reason`: store the row `Invalid` with this `last_error` without schema validation (the event
	still takes its seq and is reported as `EVENT_INVALID`; nothing is enqueued).
	Returns None when nothing could be stored (see module docstring).
	"""
	title = f"ERP Town: emit {event_type} for {subject_doctype} {subject_name} failed"
	try:
		actor_issues: list[tuple[str, str]] = []
		if actor is None:
			actor, actor_issues = resolve_actor_with_issues()
		event_id = new_ulid()
		fields = {
			"event_id": event_id,
			"event_type": event_type,
			"source": source,
			"subject_doctype": subject_doctype,
			"subject_name": subject_name,
			"company": company,
			"data": data,
			"actor": actor,
			"correlation_id": correlation_id or subject_name,
			"causation_id": causation_id,
			"visibility": visibility if visibility is not None else default_visibility(),
		}
	except Exception:  # noqa: BLE001 -- town-layer failures must never block the caller's document
		log_failure(title)
		return None

	stored = _store_in_savepoint(fields, title, invalid_reason)
	report = {"subject_doctype": subject_doctype, "subject_name": subject_name, "company": company}

	if stored is not None:
		status, error = stored
		if status == PENDING:
			enqueue_publish([event_id])
		elif event_type != SYSTEM_ERROR:
			_report(EVENT_INVALID, f"event {event_id} ({event_type}) is invalid: {error}", event_id, report)

	for code, message in actor_issues:
		_report(code, message, event_id if stored is not None else None, report)

	return event_id if stored is not None else None


def _store_in_savepoint(
	fields: dict, title: str, invalid_reason: str | None = None
) -> tuple[str, str | None] | None:
	"""Allocate seq, validate and insert the outbox row; on failure undo only these writes."""
	savepoint = f"town_emit_{frappe.generate_hash(length=10)}"
	messages = len(getattr(frappe.local, "message_log", None) or [])
	frappe.db.savepoint(savepoint)
	try:
		result = _insert_outbox_row(**fields, invalid_reason=invalid_reason)
	except Exception:
		if not _rollback_to(savepoint):
			raise
		# a swallowed frappe.throw (e.g. link validation) must not reach the user as an error dialog
		if getattr(frappe.local, "message_log", None):
			del frappe.local.message_log[messages:]
		log_failure(title)
		return None
	frappe.db.release_savepoint(savepoint)
	return result


def _insert_outbox_row(
	*,
	event_id: str,
	event_type: str,
	subject_doctype: str,
	subject_name: str,
	invalid_reason: str | None = None,
	**envelope_fields,
) -> tuple[str, str | None]:
	seq = next_seq(subject_doctype, subject_name)
	event = build_envelope(
		event_id=event_id,
		event_type=event_type,
		subject_doctype=subject_doctype,
		subject_name=subject_name,
		seq=seq,
		**envelope_fields,
	)
	payload, error = _serialize(event)
	if invalid_reason is not None:
		error = (str(invalid_reason).strip() or "invalid")[:_MAX_ERROR_MESSAGE]
	elif error is None:
		try:
			validate_event(json.loads(payload))  # validate exactly what is stored and published
		except EventSchemaError as e:
			error = e.message
	if error is not None and event_type == SYSTEM_ERROR:
		raise InvalidSystemErrorEvent(error)

	status = INVALID if error else PENDING
	frappe.get_doc(
		{
			"doctype": "Town Event Outbox",
			"event_id": event_id,
			"event_type": event_type,
			"subject_doctype": subject_doctype,
			"subject_name": subject_name,
			"seq": seq,
			"correlation_id": event["correlation_id"],
			"payload": payload,
			"status": status,
			"last_error": error,
			"next_attempt_at": now_datetime() if status == PENDING else None,
		}
	).insert(ignore_permissions=True)
	return status, error


def _serialize(event: dict) -> tuple[str, str | None]:
	"""Compact JSON with Thai text unescaped; non-JSON values make the event invalid but still stored."""
	try:
		return _dumps(event, default=json_handler, allow_nan=False), None
	except (TypeError, ValueError) as e:
		error = f"payload is not JSON-serializable: {e}"
	try:
		return _dumps(event, default=repr, allow_nan=False), error
	except TypeError, ValueError:
		return _dumps({"id": event["id"], "type": event["type"], "repr": repr(event)}, default=repr), error


def _dumps(obj, **kwargs) -> str:
	return json.dumps(obj, ensure_ascii=False, sort_keys=True, separators=(",", ":"), **kwargs)


def _rollback_to(savepoint: str) -> bool:
	try:
		frappe.db.rollback(save_point=savepoint)
	except Exception:  # noqa: BLE001 -- any failure means the savepoint is gone; caller re-raises
		return False
	return True


def enqueue_publish(event_ids: list[str]) -> None:
	"""Register one `publish` job for these outbox rows, run after the current transaction commits."""
	try:
		frappe.enqueue(PUBLISH_METHOD, queue="short", event_ids=list(event_ids), enqueue_after_commit=True)
	except Exception:  # noqa: BLE001 -- e.g. redis-queue down: the rows stay Pending for the sweeper
		log_failure(f"ERP Town: enqueue publish of {', '.join(event_ids)[:100]} failed")


def _report(code: str, message: str, causation_id: str | None, subject: dict) -> None:
	from erp_town.events.system import emit_system_error

	emit_system_error(code, message[:_MAX_ERROR_MESSAGE], causation_id=causation_id, **subject)


def log_failure(title: str) -> None:
	"""Error Log entry (with the current traceback) that never raises out of the town layer."""
	try:
		frappe.log_error(title=title[:140])
	except Exception:  # noqa: BLE001 -- logging must never raise out of the town layer
		frappe.logger("erp_town").exception(title)
