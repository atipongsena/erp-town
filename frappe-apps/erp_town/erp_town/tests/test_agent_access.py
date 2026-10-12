# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

from unittest.mock import patch

import frappe
from frappe.tests import IntegrationTestCase

from erp_town.session_api import agent_access, draft_access


class TestAgentAccess(IntegrationTestCase):
	def tearDown(self):
		frappe.set_user("Administrator")

	def test_only_current_authenticated_identity_is_resolved(self):
		frappe.set_user("Administrator")
		real_get_value = frappe.db.get_value
		def value(doctype, *args, **kwargs):
			return "helper" if doctype == "Town Agent Identity" else real_get_value(doctype, *args, **kwargs)
		with patch("erp_town.session_api.frappe.db.get_value", side_effect=value) as get_value:
			out = agent_access()
			self.assertEqual(out["user"], "Administrator")
			self.assertEqual(out["agent_key"], "helper")
			get_value.assert_any_call(
				"Town Agent Identity", {"user": "Administrator", "enabled": 1}, "agent_key"
			)
		self.assertNotIn("csrf_token", out)
		self.assertEqual(set(out["permissions"]), {"Sales Order", "Delivery Note", "Sales Invoice", "Payment Entry"})

	def test_disabled_or_missing_agent_has_no_agent_key(self):
		frappe.set_user("Administrator")
		real_get_value = frappe.db.get_value
		def value(doctype, *args, **kwargs):
			return None if doctype == "Town Agent Identity" else real_get_value(doctype, *args, **kwargs)
		with patch("erp_town.session_api.frappe.db.get_value", side_effect=value):
			self.assertEqual(agent_access()["agent_key"], "")

	def test_disabled_account_fails_closed(self):
		frappe.set_user("Administrator")
		with patch("erp_town.session_api.frappe.db.exists", return_value=False):
			with self.assertRaises(frappe.PermissionError):
				agent_access()

	def test_doctype_permissions_use_current_erp_authority(self):
		frappe.set_user("Administrator")
		with patch("erp_town.session_api.frappe.has_permission", return_value=False) as has_permission:
			out = agent_access()
			self.assertTrue(all(not value for perms in out["permissions"].values() for value in perms.values()))
			self.assertEqual(has_permission.call_count, 8)

	def test_endpoint_is_not_whitelisted_for_guest(self):
		self.assertIn(agent_access, frappe.whitelisted)
		self.assertNotIn(agent_access, frappe.guest_methods)

	def test_direct_guest_calls_fail_closed(self):
		frappe.set_user("Guest")
		with self.assertRaises(frappe.PermissionError):
			agent_access()

	def test_draft_references_use_permission_filtered_queries(self):
		frappe.set_user("Administrator")
		with patch("erp_town.session_api.frappe.get_list", side_effect=[["Company"], ["CUST"], ["Cash"], ["ITEM"]]) as query:
			self.assertEqual(draft_access("Sales Order", "ACME", "CUST", ["ITEM", "ITEM"], "Cash"), {"allowed": True})
			self.assertEqual(query.call_count, 4)
			query.assert_any_call("Item", filters={"name": ["in", ["ITEM"]]}, pluck="name", limit_page_length=0)

	def test_hidden_customer_item_company_or_payment_mode_denies_draft(self):
		frappe.set_user("Administrator")
		for position in range(4):
			with self.subTest(position=position):
				responses = [["Company"], ["CUST"], ["Cash"], ["ITEM"]]
				responses[position] = []
				with patch("erp_town.session_api.frappe.get_list", side_effect=responses):
					with self.assertRaises(frappe.PermissionError):
						draft_access("Sales Order", "ACME", "CUST", ["ITEM"], "Cash")

	def test_invalid_draft_scope_and_reference_shapes_fail_closed(self):
		frappe.set_user("Administrator")
		for doctype, company, codes in [("User", "ACME", []), ("Sales Order", "", []), ("Sales Order", "ACME", None), ("Sales Order", "ACME", [1]), ("Sales Order", "ACME", ["ITEM"] * 101)]:
			with self.assertRaises(frappe.PermissionError):
				draft_access(doctype, company, item_codes=codes)
