# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

from unittest.mock import patch

import frappe
from frappe.tests import UnitTestCase

from erp_town.setup.local_operator import provision, select_roles


class TestLocalOperator(UnitTestCase):
	def test_never_grants_service_automatic_or_unknown_roles(self):
		rows = [frappe._dict(name=name, desk_access=1, disabled=0) for name in ("System Manager", "Sales User", "Town Projection Reader", "Town Agent", "Administrator", "Guest", "All", "Unknown Manager")]
		self.assertEqual(select_roles(rows), ["Sales User", "System Manager"])
		self.assertEqual(select_roles([frappe._dict(name="HR Manager", desk_access=1, disabled=1)]), [])

	def test_refuses_other_sites_before_any_database_access(self):
		with patch.object(frappe.local, "site", "test.localhost"), patch("frappe.get_doc") as read:
			with self.assertRaises(frappe.PermissionError):
				provision()
			read.assert_not_called()

	def test_refuses_non_development_sites(self):
		with patch.object(frappe.local, "site", "town.localhost"), patch.dict(frappe.conf, {"developer_mode": False}), patch("frappe.get_doc") as read:
			with self.assertRaises(frappe.PermissionError):
				provision()
			read.assert_not_called()

	def test_refuses_non_administrator_console(self):
		with patch.object(frappe.local, "site", "town.localhost"), patch.dict(frappe.conf, {"developer_mode": True}), patch.dict(frappe.session, {"user": "sales@acme-th.test"}), patch("frappe.get_doc") as read:
			with self.assertRaises(frappe.PermissionError):
				provision()
			read.assert_not_called()
