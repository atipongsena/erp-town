# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Town Status Desk API (spec §4.4, §8); System Manager only.

- `requeue_failed(event_ids)`: the "ส่งใหม่" button of the `Town Outbox Status` report. Rows that are
  currently `Failed` go back to `Pending` with a fresh retry budget and one `publish` job is enqueued
  after commit; any other id (unknown, `Pending`, `Published`, `Invalid`) is ignored. POST only (it
  changes state); `frappe.call` posts.
- `oldest_pending_age_seconds()`: the `Town Oldest Pending Age` number card.
"""

import frappe
from frappe.utils import now_datetime

from erp_town.events.emit import PENDING, enqueue_publish
from erp_town.events.publisher import FAILED, OUTBOX

SYSTEM_MANAGER = "System Manager"


@frappe.whitelist(methods=["POST"])
def requeue_failed(event_ids: list[str] | str) -> int:
	"""Reset the listed `Failed` rows to `Pending` (`attempts = 0`) and enqueue publish; return the count."""
	frappe.only_for(SYSTEM_MANAGER)
	ids = _parse_ids(event_ids)
	if not ids:
		return 0
	failed = frappe.db.sql_list(
		f"select name from `tab{OUTBOX}` where name in %(ids)s and status = %(failed)s for update",
		{"ids": ids, "failed": FAILED},
	)
	if not failed:
		return 0
	frappe.db.set_value(
		OUTBOX,
		{"name": ("in", failed)},
		{"status": PENDING, "attempts": 0, "next_attempt_at": None, "last_error": None},
	)
	enqueue_publish(failed)
	return len(failed)


@frappe.whitelist()
def oldest_pending_age_seconds(filters=None) -> int:
	"""Age in seconds of the oldest `Pending` outbox row, 0 when there is none (`filters` is unused)."""
	frappe.only_for(SYSTEM_MANAGER)
	oldest = frappe.db.sql(f"select min(creation) from `tab{OUTBOX}` where status = %s", PENDING)[0][0]
	if not oldest:
		return 0
	return max(int((now_datetime() - oldest).total_seconds()), 0)


def _parse_ids(event_ids: list[str] | str | None) -> list[str]:
	if isinstance(event_ids, str):
		text = event_ids.strip()
		event_ids = frappe.parse_json(text) if text.startswith("[") else [text]
	return list(dict.fromkeys(str(i) for i in (event_ids or []) if i))
