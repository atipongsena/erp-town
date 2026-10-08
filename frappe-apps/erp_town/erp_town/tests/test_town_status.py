# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Town Status (spec §4.4, §8): requeue of `Failed` outbox rows, number cards, report and workspace.

Runs on `test.localhost` only. Nothing here commits (`requeue_failed` enqueues after commit and
`frappe.enqueue` is patched), so every row is rolled back in `tearDown`.
"""

import json
from datetime import timedelta
from pathlib import Path
from unittest.mock import patch

import frappe
from frappe.tests import IntegrationTestCase
from frappe.utils import now_datetime

from erp_town.erp_town.report.town_outbox_status.town_outbox_status import execute
from erp_town.events.api import oldest_pending_age_seconds, requeue_failed
from erp_town.events.emit import PUBLISH_METHOD
from erp_town.events.ulid import new_ulid

OUTBOX = "Town Event Outbox"
SYSTEM_MANAGER = "System Manager"
STATUSES = ("Failed", "Invalid", "Pending", "Published")


class TestTownStatus(IntegrationTestCase):
	def setUp(self):
		self.enqueue = patch("frappe.enqueue").start()
		self.addCleanup(patch.stopall)
		frappe.set_user("Administrator")

	def tearDown(self):
		frappe.set_user("Administrator")
		frappe.db.rollback()

	def _row(self, status: str, age_seconds: int = 0, **fields) -> str:
		event_id = new_ulid()
		frappe.get_doc(
			{
				"doctype": OUTBOX,
				"event_id": event_id,
				"event_type": "erp.item.updated",
				"subject_doctype": "Item",
				"subject_name": f"ITEM-STATUS-{event_id[-6:]}",
				"seq": 1,
				"correlation_id": event_id,
				"payload": "{}",
				"status": status,
				**fields,
			}
		).insert(ignore_permissions=True)
		if age_seconds:
			frappe.db.set_value(
				OUTBOX,
				event_id,
				"creation",
				now_datetime() - timedelta(seconds=age_seconds),
				update_modified=False,
			)
		return event_id

	def _failed(self) -> str:
		return self._row(
			"Failed", attempts=10, last_error="ConnectionError: down", next_attempt_at=now_datetime()
		)

	# --- requeue ------------------------------------------------------------------------------

	def test_requeue_resets_failed_rows_and_enqueues_publish(self):
		failed = [self._failed(), self._failed()]
		pending = self._row("Pending", attempts=3, last_error="still retrying")
		published = self._row("Published")
		invalid = self._row("Invalid", last_error="schema")

		count = requeue_failed([*failed, pending, published, invalid, "NO-SUCH-EVENT"])

		self.assertEqual(count, 2)
		for name in failed:
			row = frappe.db.get_value(
				OUTBOX, name, ["status", "attempts", "next_attempt_at", "last_error"], as_dict=True
			)
			self.assertEqual(
				row, {"status": "Pending", "attempts": 0, "next_attempt_at": None, "last_error": None}
			)
		self.assertEqual(
			frappe.db.get_value(OUTBOX, pending, ["attempts", "last_error"]), (3, "still retrying")
		)
		self.assertEqual(frappe.db.get_value(OUTBOX, published, "status"), "Published")
		self.assertEqual(frappe.db.get_value(OUTBOX, invalid, "status"), "Invalid")
		self.enqueue.assert_called_once()
		args, kwargs = self.enqueue.call_args
		self.assertEqual(args, (PUBLISH_METHOD,))
		self.assertEqual(sorted(kwargs["event_ids"]), sorted(failed))
		self.assertEqual(kwargs["queue"], "short")
		self.assertTrue(kwargs["enqueue_after_commit"])

	def test_requeue_accepts_json_list_and_single_id(self):
		first, second = self._failed(), self._failed()
		self.assertEqual(requeue_failed(json.dumps([first])), 1)
		self.assertEqual(requeue_failed(second), 1)
		self.assertEqual(frappe.db.get_value(OUTBOX, second, "status"), "Pending")

	def test_requeue_with_nothing_failed_enqueues_nothing(self):
		self.assertEqual(requeue_failed([self._row("Published")]), 0)
		self.assertEqual(requeue_failed([]), 0)
		self.enqueue.assert_not_called()

	def test_non_system_manager_is_refused(self):
		failed = self._failed()
		user = self._user_without_system_manager()
		frappe.set_user(user)
		self.assertNotIn(SYSTEM_MANAGER, frappe.get_roles())
		with self.assertRaises(frappe.PermissionError):
			requeue_failed([failed])
		with self.assertRaises(frappe.PermissionError):
			oldest_pending_age_seconds()
		with self.assertRaises(frappe.PermissionError):
			execute({})
		frappe.set_user("Administrator")
		self.assertEqual(frappe.db.get_value(OUTBOX, failed, "status"), "Failed")
		self.assertNotIn(PUBLISH_METHOD, [c.args[0] for c in self.enqueue.call_args_list if c.args])

	def test_methods_are_whitelisted(self):
		for method in (requeue_failed, oldest_pending_age_seconds):
			self.assertIn(method, frappe.whitelisted)

	def _user_without_system_manager(self) -> str:
		email = "town-status-clerk@example.com"
		if not frappe.db.exists("User", email):
			frappe.get_doc(
				{"doctype": "User", "email": email, "first_name": "Clerk", "send_welcome_email": 0}
			).insert(ignore_permissions=True)
		user = frappe.get_doc("User", email)
		user.add_roles("Sales User")
		return email

	# --- oldest pending age -------------------------------------------------------------------

	def test_oldest_pending_age_seconds(self):
		self._row("Pending", age_seconds=60)
		self._row("Failed", age_seconds=99_999)  # only Pending rows count
		self.assertGreaterEqual(oldest_pending_age_seconds(), 60)
		self.assertIsInstance(oldest_pending_age_seconds(), int)

	def test_oldest_pending_age_is_zero_without_pending_rows(self):
		frappe.db.delete(OUTBOX, {"status": "Pending"})
		self._row("Failed", age_seconds=600)
		self.assertEqual(oldest_pending_age_seconds(), 0)
		self.assertEqual(oldest_pending_age_seconds(filters="{}"), 0)  # number cards pass `filters`

	# --- report -------------------------------------------------------------------------------

	def test_report_lists_rows_grouped_by_status_with_counts(self):
		frappe.db.delete(OUTBOX)
		published = self._row("Published", age_seconds=500)
		failed_old = self._failed()
		frappe.db.set_value(OUTBOX, failed_old, "creation", now_datetime() - timedelta(seconds=400))
		pending = self._row("Pending", age_seconds=300)
		invalid = self._row("Invalid", age_seconds=200)
		failed_new = self._row("Failed", age_seconds=100)

		columns, data, _message, chart, summary = execute({})

		self.assertEqual([r["event_id"] for r in data], [failed_old, failed_new, invalid, pending, published])
		self.assertIn("status", [c["fieldname"] for c in columns])
		self.assertEqual(
			{s["label"]: s["value"] for s in summary},
			{frappe._(s): n for s, n in zip(STATUSES, (2, 1, 1, 1), strict=True)},
		)
		self.assertEqual(chart["data"]["datasets"][0]["values"], [2, 1, 1, 1])

		_c, only_failed, *_ = execute({"status": "Failed"})
		self.assertEqual([r["event_id"] for r in only_failed], [failed_old, failed_new])

	def test_report_date_range(self):
		frappe.db.delete(OUTBOX)
		self._row("Pending", age_seconds=3 * 86_400)
		recent = self._row("Pending")
		today = now_datetime().date()
		_c, data, *_ = execute({"from_date": str(today - timedelta(days=1)), "to_date": str(today)})
		self.assertEqual([r["event_id"] for r in data], [recent])

	def test_report_has_no_default_date_window(self):
		"""Every Failed row the Town Failed card counts must be listed for "ส่งใหม่" by default."""
		js = (
			Path(frappe.get_app_path("erp_town"))
			/ "erp_town"
			/ "report"
			/ "town_outbox_status"
			/ "town_outbox_status.js"
		).read_text(encoding="utf-8")
		filters = js.split("filters: [", 1)[1].split("],\n\tonload", 1)[0]
		self.assertIn('fieldname: "from_date"', filters)
		self.assertNotIn("default:", filters)

	# --- desk records -------------------------------------------------------------------------

	def test_report_is_system_manager_only(self):
		report = frappe.get_doc("Report", "Town Outbox Status")
		self.assertEqual(report.report_type, "Script Report")
		self.assertEqual(report.ref_doctype, OUTBOX)
		self.assertEqual([r.role for r in report.roles], [SYSTEM_MANAGER])

	def test_number_cards(self):
		for name, status in (
			("Town Pending", "Pending"),
			("Town Failed", "Failed"),
			("Town Invalid", "Invalid"),
		):
			card = frappe.get_doc("Number Card", name)
			self.assertEqual(
				(card.type, card.document_type, card.function), ("Document Type", OUTBOX, "Count")
			)
			self.assertEqual(json.loads(card.filters_json), [[OUTBOX, "status", "=", status]])
		age = frappe.get_doc("Number Card", "Town Oldest Pending Age")
		self.assertEqual(age.type, "Custom")
		self.assertEqual(age.method, "erp_town.events.api.oldest_pending_age_seconds")

	def test_workspace(self):
		workspace = frappe.get_doc("Workspace", "ERP Town")
		self.assertEqual(workspace.module, "ERP Town")
		self.assertTrue(workspace.public)
		self.assertEqual([r.role for r in workspace.roles], [SYSTEM_MANAGER])
		self.assertEqual(
			{c.number_card_name for c in workspace.number_cards},
			{"Town Pending", "Town Failed", "Town Invalid", "Town Oldest Pending Age"},
		)
		self.assertIn("Town Outbox Status", {link.link_to for link in workspace.links})
