# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Pilot bootstrap (spec §6.3 pre-pilot checklist): the real Thai company on an empty pilot site.

Runs on `test.localhost` only. `bootstrap()` never commits; every test rolls back and the class checks that
no company, user or setting outlived it.
"""

import frappe
from frappe.core.doctype.system_settings.system_settings import clear_system_settings_cache
from frappe.tests import IntegrationTestCase

from erp_town.setup import pilot

COMPANY = "บริษัท นำร่องทดสอบ จำกัด"
ABBR = "PILOT-T"
TAX_ID = "0105555123450"  # checksum-valid
SETTINGS = ("language", "time_zone", "country", "currency", "setup_complete")
DEMO_DOCTYPES = ("Customer", "Item", "User", "Town Agent Identity", "Sales Order", "Sales Invoice")


def _settings() -> dict:
	return {field: frappe.db.get_single_value("System Settings", field) for field in SETTINGS}


def _counts() -> dict:
	return {doctype: frappe.db.count(doctype) for doctype in DEMO_DOCTYPES}


def _clear_caches() -> None:
	clear_system_settings_cache()
	frappe.local.system_settings = None
	frappe.clear_cache()
	frappe.cache.delete_value("fiscal_years")


class TestPilotBootstrap(IntegrationTestCase):
	@classmethod
	def setUpClass(cls):
		super().setUpClass()
		cls.settings_before = _settings()
		cls.counts_before = _counts()
		cls.lang_before = frappe.local.lang

	@classmethod
	def tearDownClass(cls):
		frappe.db.rollback()
		frappe.local.lang = cls.lang_before
		_clear_caches()
		try:
			leaks = []
			if frappe.db.exists("Company", COMPANY):
				leaks.append("company")
			if _settings() != cls.settings_before:
				leaks.append("System Settings")
			if _counts() != cls.counts_before:
				leaks.append("documents")
			assert not leaks, f"test_pilot leaked: {', '.join(leaks)}"
		finally:
			super().tearDownClass()

	def tearDown(self):
		frappe.db.rollback()
		frappe.local.lang = self.lang_before
		_clear_caches()

	def test_bootstrap_creates_the_thai_company_and_settings_idempotently(self):
		before = _counts()
		self.assertEqual(pilot.bootstrap(COMPANY, ABBR, TAX_ID), COMPANY)
		accounts = frappe.db.count("Account", {"company": COMPANY})
		self.assertEqual(pilot.bootstrap(COMPANY, ABBR, TAX_ID), COMPANY)  # second run adds nothing

		company = frappe.get_doc("Company", COMPANY)
		self.assertEqual(
			(company.abbr, company.tax_id, company.country, company.default_currency),
			(ABBR, TAX_ID, "Thailand", "THB"),
		)
		self.assertEqual(frappe.db.count("Company", {"name": COMPANY}), 1)
		self.assertEqual(frappe.db.count("Account", {"company": COMPANY}), accounts)
		self.assertTrue(frappe.db.exists("Account", {"company": COMPANY, "account_name": "ภาษีขาย"}))
		self.assertEqual(
			frappe.db.count("Sales Taxes and Charges Template", {"company": COMPANY}), 2
		)  # ภาษีขาย 7% and the VAT-inclusive variant
		self.assertEqual(
			_settings(),
			{
				"language": "th",
				"time_zone": "Asia/Bangkok",
				"country": "Thailand",
				"currency": "THB",
				"setup_complete": 1,
			},
		)
		self.assertTrue(frappe.db.get_single_value("Global Defaults", "default_company"))  # set when empty
		self.assertEqual(_counts(), before)  # no demo customers, items, users, agents or documents

	def test_bootstrap_rejects_an_invalid_tax_id(self):
		with self.assertRaises(frappe.ValidationError):
			pilot.bootstrap(COMPANY, ABBR, "1234567890123")
		self.assertFalse(frappe.db.exists("Company", COMPANY))

	def test_bootstrap_requires_all_arguments(self):
		for args in (("", ABBR, TAX_ID), (COMPANY, "", TAX_ID), (COMPANY, ABBR, "")):
			with self.assertRaises(frappe.ValidationError, msg=repr(args)):
				pilot.bootstrap(*args)
