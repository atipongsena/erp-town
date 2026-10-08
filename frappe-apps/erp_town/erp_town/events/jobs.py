# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Scheduled town events (spec §3.3, §4.3): `erp.sales_invoice.overdue`.

`emit_overdue_hourly` is registered on cron `0 * * * *` and only acts in the Town Settings
`overdue_job_hour` (system timezone), so the hour stays configurable without touching hooks.
"""

from datetime import date

import frappe
from frappe.utils import add_days, getdate, now_datetime, nowdate

from erp_town.erp_town.doctype.town_settings.town_settings import get_town_settings
from erp_town.events.emit import emit_raw
from erp_town.events.lineage import causation_of, latest_event
from erp_town.events.mappers import iso_date
from erp_town.events.system import report_build_failure

SOURCE = "erp/accounts"
OVERDUE = "erp.sales_invoice.overdue"


COMMIT_EVERY = 20  # invoices per transaction in the scheduled run


def emit_overdue_hourly() -> int | None:
	"""Run `emit_overdue()` when the current hour is `overdue_job_hour`; None in any other hour."""
	if now_datetime().hour != get_town_settings().overdue_job_hour:
		return None
	return emit_overdue(commit_every=COMMIT_EVERY)


def emit_overdue(today: date | None = None, commit_every: int | None = None) -> int:
	"""`erp.sales_invoice.overdue` for each submitted SI with `outstanding_amount > 0` and `due_date < today`,
	at most once per invoice per day (an overdue outbox row created today counts, whatever its status).

	`commit_every`: commit after every that many invoices and at the end, so the per-subject seq row locks
	are held for one small batch instead of the whole run (each commit also flushes that batch's publish
	job). None (default) leaves the transaction to the caller. One failing invoice never stops the run.
	Actor is `system.scheduler`. Returns the number of events stored.
	"""
	today = getdate(today or nowdate())
	previous = frappe.flags.town_system_actor
	frappe.flags.town_system_actor = True
	try:
		count = done = 0
		for done, invoice in enumerate(_overdue_invoices(today), start=1):
			if _emit(invoice, today):
				count += 1
			if commit_every and done % commit_every == 0:
				frappe.db.commit()
		if commit_every and done % commit_every:
			frappe.db.commit()
		return count
	finally:
		frappe.flags.town_system_actor = previous


def _overdue_invoices(today: date) -> list:
	return frappe.get_all(
		"Sales Invoice",
		filters={"docstatus": 1, "outstanding_amount": [">", 0], "due_date": ["<", today]},
		fields=["name", "company", "due_date"],
		order_by="due_date asc, name asc",
	)


def _emitted_on(invoice: str, day: date) -> bool:
	return bool(
		frappe.db.sql(
			"""select 1 from `tabTown Event Outbox`
			where subject_doctype = 'Sales Invoice' and subject_name = %s and event_type = %s
			and creation >= %s and creation < %s limit 1""",
			(invoice, OVERDUE, day, add_days(day, 1)),
		)
	)


def _emit(invoice, today: date) -> bool:
	"""Emit for one invoice unless it already has today's event; False when skipped or not stored."""
	try:
		if _emitted_on(invoice.name, today):
			return False
		own = latest_event("Sales Invoice", invoice.name)
		data = {
			"due_date": iso_date(invoice.due_date),
			"days_overdue": (today - getdate(invoice.due_date)).days,
		}
	except Exception as e:  # noqa: BLE001 -- one bad invoice must not stop the job
		report_build_failure(
			OVERDUE, e, subject_doctype="Sales Invoice", subject_name=invoice.name, company=invoice.company
		)
		return False
	return bool(
		emit_raw(
			OVERDUE,
			source=SOURCE,
			subject_doctype="Sales Invoice",
			subject_name=invoice.name,
			company=invoice.company,
			data=data,
			correlation_id=own["correlation_id"] if own else invoice.name,
			causation_id=causation_of(own),
		)
	)
