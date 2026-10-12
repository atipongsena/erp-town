# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

import frappe
from frappe.core.doctype.system_settings.system_settings import clear_system_settings_cache
from frappe.tests import IntegrationTestCase

from erp_town.setup.masters import mark_setup_complete


class TestSetupCompletion(IntegrationTestCase):
	def tearDown(self):
		frappe.db.rollback()
		clear_system_settings_cache()
		frappe.local.system_settings = None
		frappe.clear_cache()
		super().tearDown()

	def test_completed_seed_exits_the_wizard_home_route(self):
		# A freshly installed site keeps this default even after masters exist.
		# Desk otherwise loads the wizard, which redirects straight back to Desk.
		frappe.db.set_default("desktop:home_page", "setup-wizard")
		mark_setup_complete()
		self.assertTrue(frappe.is_setup_complete())
		self.assertEqual(frappe.db.get_default("desktop:home_page"), "desktop")

	def test_existing_operator_home_route_is_preserved(self):
		frappe.db.set_default("desktop:home_page", "selling")
		mark_setup_complete()
		self.assertEqual(frappe.db.get_default("desktop:home_page"), "selling")

	def test_completing_twice_keeps_the_finished_home_route(self):
		frappe.db.set_default("desktop:home_page", "setup-wizard")
		mark_setup_complete()
		mark_setup_complete()
		self.assertEqual(frappe.db.get_default("desktop:home_page"), "desktop")
