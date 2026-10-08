# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""`Town Outbox Status` (spec §4.4, §8): outbox rows grouped by status, with a count per status.

Filters: `status`, `from_date` / `to_date` (on the row's creation date). Rows are ordered by status
(Failed, Invalid, Pending, Published) then creation, capped at `ROW_LIMIT`; the summary and chart count
every matching row. The `.js` adds the "ส่งใหม่" button that requeues the `Failed` rows shown.
System Manager only.
"""

import frappe
from frappe import _
from frappe.utils import add_days, getdate

from erp_town.events.api import SYSTEM_MANAGER
from erp_town.events.publisher import OUTBOX

STATUSES = ("Failed", "Invalid", "Pending", "Published")
INDICATORS = {"Failed": "Red", "Invalid": "Orange", "Pending": "Blue", "Published": "Green"}
ROW_LIMIT = 1000


def execute(filters=None):
	frappe.only_for(SYSTEM_MANAGER)
	filters = frappe._dict(filters or {})
	conditions, values = _conditions(filters)
	counts = _counts(conditions, values)
	return get_columns(), _rows(conditions, values), None, _chart(counts), _summary(counts)


def get_columns() -> list[dict]:
	return [
		{
			"fieldname": "event_id",
			"label": _("Event ID"),
			"fieldtype": "Link",
			"options": OUTBOX,
			"width": 240,
		},
		{"fieldname": "status", "label": _("Status"), "fieldtype": "Data", "width": 100},
		{"fieldname": "event_type", "label": _("Event Type"), "fieldtype": "Data", "width": 200},
		{
			"fieldname": "subject_doctype",
			"label": _("Subject DocType"),
			"fieldtype": "Link",
			"options": "DocType",
			"width": 130,
		},
		{
			"fieldname": "subject_name",
			"label": _("Subject Name"),
			"fieldtype": "Dynamic Link",
			"options": "subject_doctype",
			"width": 160,
		},
		{"fieldname": "seq", "label": _("Seq"), "fieldtype": "Int", "width": 60},
		{"fieldname": "attempts", "label": _("Attempts"), "fieldtype": "Int", "width": 90},
		{
			"fieldname": "next_attempt_at",
			"label": _("Next Attempt At"),
			"fieldtype": "Datetime",
			"width": 160,
		},
		{"fieldname": "last_error", "label": _("Last Error"), "fieldtype": "Data", "width": 260},
		{"fieldname": "creation", "label": _("Created On"), "fieldtype": "Datetime", "width": 160},
		{"fieldname": "published_at", "label": _("Published At"), "fieldtype": "Datetime", "width": 160},
	]


def _conditions(filters) -> tuple[str, dict]:
	conditions, values = [], {}
	if filters.get("status"):
		conditions.append("status = %(status)s")
		values["status"] = filters.status
	if filters.get("from_date"):
		conditions.append("creation >= %(from_date)s")
		values["from_date"] = getdate(filters.from_date)
	if filters.get("to_date"):
		conditions.append("creation < %(to_date)s")
		values["to_date"] = add_days(getdate(filters.to_date), 1)
	return (" where " + " and ".join(conditions)) if conditions else "", values


def _rows(conditions: str, values: dict) -> list[dict]:
	return frappe.db.sql(
		f"""
		select name as event_id, status, event_type, subject_doctype, subject_name, seq, attempts,
			next_attempt_at, last_error, creation, published_at
		from `tab{OUTBOX}`{conditions}
		order by field(status, {", ".join(frappe.db.escape(s) for s in STATUSES)}), creation, name
		limit {ROW_LIMIT}
		""",
		values,
		as_dict=True,
	)


def _counts(conditions: str, values: dict) -> dict[str, int]:
	rows = frappe.db.sql(f"select status, count(*) from `tab{OUTBOX}`{conditions} group by status", values)
	found = dict(rows)
	return {status: int(found.get(status) or 0) for status in STATUSES}


def _summary(counts: dict[str, int]) -> list[dict]:
	return [
		{"value": count, "label": _(status), "datatype": "Int", "indicator": INDICATORS[status]}
		for status, count in counts.items()
	]


def _chart(counts: dict[str, int]) -> dict:
	return {
		"data": {
			"labels": [_(status) for status in counts],
			"datasets": [{"name": _("Status"), "values": list(counts.values())}],
		},
		"type": "bar",
	}
