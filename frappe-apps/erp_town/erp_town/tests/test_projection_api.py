# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

import json
from unittest.mock import patch

import frappe
from frappe.tests import IntegrationTestCase, UnitTestCase
from frappe.utils import add_days, nowdate

from erp_town.projection_api import _pages, read_projection
from erp_town.tests.factories import (
	TEST_ABBR, ensure_test_company, make_customer, make_item, make_pe_for_si, make_si, make_so,
)


class TestProjectionApi(UnitTestCase):
	def test_requires_dedicated_role_before_reading(self):
		with patch("frappe.get_roles", return_value=["Sales User"]), patch("frappe.get_list") as read:
			with self.assertRaises(frappe.PermissionError):
				read_projection("OTHER")
			self.assertEqual([call.args[0] for call in read.call_args_list if call.args[0] != "Translation"], [])

	def test_pagination_does_not_silently_truncate(self):
		rows = [frappe._dict(name=f"SO-{i:03}") for i in range(205)]
		def page(*args, **kwargs):
			start = kwargs["offset"]
			return rows[start:start + kwargs["limit"]]
		with patch("frappe.get_list", side_effect=page) as read:
			self.assertEqual(len(list(_pages("Sales Order", {}, ["name"]))), 205)
			self.assertEqual(read.call_count, 2)

	def test_company_permission_denial_prevents_other_reads(self):
		with patch("frappe.get_roles", return_value=["Town Projection Reader"]), patch("frappe.get_list", return_value=[]) as read:
			with self.assertRaises(frappe.PermissionError):
				read_projection("OTHER")
			# Frappe may read Translation while rendering the permission error.
			self.assertEqual([call.args[0] for call in read.call_args_list if call.args[0] != "Translation"], ["Company"])


class TestProjectionApiIntegration(IntegrationTestCase):
	@classmethod
	def setUpClass(cls):
		super().setUpClass()
		ensure_test_company()
		frappe.db.commit()

	def tearDown(self):
		frappe.set_user("Administrator")

	def test_read_projection_is_metadata_only_and_does_not_emit(self):
		frappe.set_user("Administrator")
		customer = make_customer()
		item = make_item()
		with patch("frappe.enqueue"):
			so = make_so(customer, [{"item_code": item, "qty": 1, "rate": 123.45}])
		before = frappe.db.count("Town Event Outbox")
		roles = [*frappe.get_roles("Administrator"), "Town Projection Reader"]
		with patch("frappe.get_roles", return_value=roles):
			out = read_projection(TEST_ABBR)
		self.assertEqual(out["company"], TEST_ABBR)
		fact = next(f for f in out["facts"] if f["name"] == so.name)
		self.assertEqual(set(fact), {"doctype", "name", "kind", "status"})
		self.assertTrue(any(s["name"] == so.name and s["seq"] > 0 for s in out["sequences"]))
		self.assertNotIn("grand_total", json.dumps(out))
		self.assertNotIn(customer, json.dumps(out))
		self.assertEqual(frappe.db.count("Town Event Outbox"), before)

	def test_endpoint_is_read_only_and_not_for_guest(self):
		self.assertIn(read_projection, frappe.whitelisted)
		self.assertNotIn(read_projection, frappe.guest_methods)
		self.assertEqual(frappe.allowed_http_methods_for_whitelisted_func[read_projection], ["GET"])

	def test_stock_ratio_and_paid_invoice_reconciliation(self):
		frappe.set_user("Administrator")
		with patch("frappe.enqueue"):
			low = make_item(reorder_level=10, opening_qty=1)
			above = make_item(reorder_level=10, opening_qty=20)
			boundary = make_item(reorder_level=1000, opening_qty=999)
			si = make_si(make_customer(), [low], posting_date=add_days(nowdate(), -2))
		roles = [*frappe.get_roles("Administrator"), "Town Projection Reader"]
		with patch("frappe.get_roles", return_value=roles):
			before = read_projection(TEST_ABBR)
		stock = {row["name"]: row for row in before["facts"] if row["kind"] == "stock"}
		self.assertEqual(stock[low]["fill_ratio"], 0.1)
		self.assertEqual(stock[boundary]["fill_ratio"], 1)
		self.assertNotIn(above, stock)
		self.assertEqual(next(row for row in before["facts"] if row["name"] == si.name)["days_overdue"], 2)
		with patch("frappe.enqueue"):
			make_pe_for_si(si)
		with patch("frappe.get_roles", return_value=roles):
			after = read_projection(TEST_ABBR)
		self.assertFalse(any(row["name"] == si.name for row in after["facts"]))
		self.assertTrue(any(row["name"] == si.name for row in after["sequences"]))
