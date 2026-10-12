# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""`erp_town.session_api.whoami`: the session's user, roles, readable company codes and CSRF token."""

import frappe
from unittest.mock import patch
from frappe.tests import IntegrationTestCase

from erp_town.session_api import whoami


class TestSessionApi(IntegrationTestCase):
	def test_disabled_account_fails_closed_even_with_an_existing_session(self):
		frappe.set_user("Administrator")
		with patch("erp_town.session_api.frappe.db.exists", return_value=False):
			with self.assertRaises(frappe.PermissionError):
				whoami()
	def tearDown(self):
		frappe.set_user("Administrator")

	def test_whoami_returns_roles_companies_and_csrf(self):
		frappe.set_user("Administrator")
		out = whoami()
		self.assertEqual(out["user"], "Administrator")
		self.assertIn("System Manager", out["roles"])
		self.assertTrue(out["csrf_token"])
		self.assertIsInstance(out["companies"], list)

	def test_companies_are_abbr_codes(self):
		frappe.set_user("Administrator")
		abbrs = frappe.get_all("Company", pluck="abbr")
		self.assertCountEqual(whoami()["companies"], abbrs)

	def test_whoami_is_whitelisted_but_not_for_guest(self):
		self.assertIn(whoami, frappe.whitelisted)
		self.assertNotIn(whoami, frappe.guest_methods)

	def test_user_without_company_read_gets_no_companies(self):
		email = "session-api-no-company@example.com"
		if not frappe.db.exists("User", email):
			frappe.get_doc(
				{"doctype": "User", "email": email, "first_name": "No Company", "send_welcome_email": 0}
			).insert(ignore_permissions=True)
		self.assertFalse(frappe.has_permission("Company", "read", user=email))
		frappe.set_user(email)
		frappe.local.message_log = []  # drop the user-creation notice
		out = whoami()
		self.assertEqual(out["user"], email)
		self.assertEqual(out["companies"], [])
		self.assertTrue(out["csrf_token"])
		self.assertEqual(frappe.local.message_log, [])
