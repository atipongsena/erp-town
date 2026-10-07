# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Event envelope (CloudEvents 1.0 based, spec §3.1)."""

from zoneinfo import ZoneInfo

from frappe.utils import get_system_timezone, now_datetime

SPECVERSION = "1.0"
SCHEMA_VERSION = 1


def default_visibility() -> dict:
	return {"level": "public", "roles": []}


def event_time() -> str:
	"""Now in the system timezone as ISO 8601 with offset, seconds precision (`2026-10-07T10:15:00+07:00`).

	`now_datetime()` is naive wall-clock time in the system timezone, independent of the container clock's
	own zone; the offset is re-attached here.
	"""
	return now_datetime().replace(tzinfo=ZoneInfo(get_system_timezone())).isoformat(timespec="seconds")


def build_envelope(
	*,
	event_id: str,
	event_type: str,
	source: str,
	subject_doctype: str,
	subject_name: str,
	company: str | None,
	data: dict,
	actor: dict,
	seq: int,
	correlation_id: str,
	causation_id: str | None,
	visibility: dict,
	time: str | None = None,
) -> dict:
	return {
		"specversion": SPECVERSION,
		"id": event_id,
		"type": event_type,
		"source": source,
		"time": time or event_time(),
		"schema_version": SCHEMA_VERSION,
		"company": company,
		"actor": actor,
		"subject": {"doctype": subject_doctype, "name": subject_name},
		"seq": seq,
		"correlation_id": correlation_id,
		"causation_id": causation_id,
		"visibility": visibility,
		"data": data,
	}
