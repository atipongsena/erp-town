# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""`erp_town.setup.testing`: Administrator API keys for the TS integration/e2e suites (Ruling D).

Runs on `test.localhost` only; every change is rolled back in `tearDown`.
"""

from unittest.mock import patch

import frappe
from frappe.tests import IntegrationTestCase
from frappe.utils.password import get_decrypted_password

from erp_town.setup.testing import enable_town_scheduler, issue_test_api_keys


class TestIssueTestApiKeys(IntegrationTestCase):
	def setUp(self):
		frappe.set_user("Administrator")

	def tearDown(self):
		frappe.set_user("Administrator")
		frappe.db.rollback()

	def test_returns_key_and_secret_of_administrator(self):
		key, secret = issue_test_api_keys().split(":")
		self.assertTrue(key)
		self.assertTrue(secret)
		self.assertEqual(frappe.db.get_value("User", "Administrator", "api_key"), key)
		self.assertEqual(get_decrypted_password("User", "Administrator", "api_secret"), secret)

	def test_is_stable_across_calls(self):
		self.assertEqual(issue_test_api_keys(), issue_test_api_keys())

	def test_refuses_without_allow_tests(self):
		with patch.dict(frappe.local.conf, {"allow_tests": False}), self.assertRaises(frappe.PermissionError):
			issue_test_api_keys()


class TestEnableTownScheduler(IntegrationTestCase):
	def setUp(self):
		frappe.set_user("Administrator")

	def tearDown(self):
		frappe.set_user("Administrator")
		frappe.db.rollback()

	def test_runs_only_the_erp_town_jobs(self):
		running = enable_town_scheduler()
		self.assertIn("erp_town.events.publisher.sweep", running)
		active = frappe.get_all("Scheduled Job Type", filters={"stopped": 0}, pluck="method")
		self.assertEqual(sorted(active), sorted(running))
		self.assertTrue(all(m.startswith("erp_town.") for m in active))
		self.assertTrue(frappe.db.count("Scheduled Job Type", {"stopped": 1}))
		self.assertEqual(frappe.db.get_single_value("System Settings", "enable_scheduler"), 1)

	def test_refuses_without_allow_tests(self):
		with patch.dict(frappe.local.conf, {"allow_tests": False}), self.assertRaises(frappe.PermissionError):
			enable_town_scheduler()
