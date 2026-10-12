# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

from unittest.mock import patch

import frappe
from frappe.tests import UnitTestCase

from erp_town.setup.testing import issue_client_projection_keys


class TestClientAcceptanceSetup(UnitTestCase):
	def test_refuses_an_existing_role_with_write_permissions(self):
		def exists(doctype, *args, **kwargs):
			return doctype != "User"
		with patch.dict(frappe.conf, {"allow_tests": True}), patch.object(frappe.local, "site", "test.localhost"), patch("frappe.db.get_value", return_value="Test Company"), patch("frappe.db.exists", side_effect=exists), patch("frappe.get_all", return_value=[frappe._dict(write=1)]), patch("frappe.get_doc") as create:
			with self.assertRaises(frappe.PermissionError):
				issue_client_projection_keys()
			create.assert_not_called()

	def test_refuses_non_test_sites_before_any_database_access(self):
		with patch.dict(frappe.conf, {"allow_tests": False}), patch("frappe.get_doc") as read:
			with self.assertRaises(frappe.PermissionError):
				issue_client_projection_keys()
			read.assert_not_called()

	def test_refuses_other_allow_tests_sites_before_any_database_access(self):
		with patch.dict(frappe.conf, {"allow_tests": True}), patch.object(frappe.local, "site", "other.localhost"), patch("frappe.get_doc") as read:
			with self.assertRaises(frappe.PermissionError):
				issue_client_projection_keys()
			read.assert_not_called()
