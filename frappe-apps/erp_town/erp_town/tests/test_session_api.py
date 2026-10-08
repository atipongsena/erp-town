# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""`erp_town.session_api.whoami`: the session's user, roles, readable company codes and CSRF token."""

import frappe
from frappe.tests import IntegrationTestCase

from erp_town.session_api import whoami


class TestSessionApi(IntegrationTestCase):
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
