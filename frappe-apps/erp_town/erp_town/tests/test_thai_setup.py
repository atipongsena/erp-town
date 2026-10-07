# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Thai localization setup (spec §5.1, §5.2): company/chart/VAT, branch codes, tax-ID validation, BE numbering.

Runs on `test.localhost` only. Every test rolls its documents back; tests that touch metadata caches
(naming Property Setters, a new company) also clear those caches so nothing leaks into other modules.
"""

import json
from pathlib import Path
from types import SimpleNamespace

import frappe
from frappe.tests import IntegrationTestCase, UnitTestCase
from frappe.utils import getdate, nowdate

from erp_town.erp_town.doctype.town_settings.town_settings import clear_town_settings_cache
from erp_town.tests.factories import (
	CURRENCY,
	PRICE_LIST,
	TEST_COMPANY,
	ensure_test_company,
	make_customer,
	make_item,
	make_si,
	make_so,
)
from erp_town.thai.naming import be_mm, be_yy

VALID_TAX_ID = "0105536112014"  # checksum-valid (see test_thai_utils)
INVALID_TAX_ID = "0105536112015"
NAMED_DOCTYPES = ("Sales Order", "Delivery Note", "Sales Invoice", "Payment Entry")
DEFAULT_SERIES = {
	"Sales Order": "SO.BEYY.BEMM.-.####",
	"Delivery Note": "DN.BEYY.BEMM.-.####",
	"Sales Invoice": "IV.BEYY.BEMM.-.####",
	"Payment Entry": "RC.BEYY.BEMM.-.####",
}
THAI_COMPANY = "ทดสอบ จำกัด"
THAI_ABBR = "TT"
CHART = Path(frappe.get_app_path("erp_town", "setup", "charts", "th_tfrs_npaes.json"))
ROOTS = {
	"สินทรัพย์": "Asset",
	"หนี้สิน": "Liability",
	"ส่วนของเจ้าของ": "Equity",
	"รายได้": "Income",
	"ค่าใช้จ่าย": "Expense",
}
LEAVES = {
	"เงินสด": "Cash",
	"เงินฝากธนาคาร": "Bank",
	"ลูกหนี้การค้า": "Receivable",
	"สินค้าคงเหลือ": "Stock",
	"ภาษีซื้อ": "Tax",
	"เจ้าหนี้การค้า": "Payable",
	"ภาษีขาย": "Tax",
	"สินค้ารับแล้วยังไม่ได้รับใบแจ้งหนี้": "Stock Received But Not Billed",
	"ทุนจดทะเบียน": "Equity",
	"กำไรสะสม": "Equity",
	"รายได้จากการขาย": "Income Account",
	"ต้นทุนขาย": "Cost of Goods Sold",
	"ผลต่างจากการปรับปรุงสต็อก": "Stock Adjustment",
	"ปัดเศษ": "Round Off",
	"ค่าเสื่อมราคา": "Depreciation",
	"ส่วนลดจ่าย": "Expense Account",
}


def _clear_naming_caches() -> None:
	for doctype in NAMED_DOCTYPES:
		frappe.clear_cache(doctype=doctype)
	frappe.local.new_doc_templates.clear()  # per-process template keeps the naming_series default
	clear_town_settings_cache()


def _isolate_series(key: str, doctype: str) -> None:
	"""Make `key` (e.g. `IV6910-`) a fresh series inside this test's transaction (rolled back in tearDown).

	The counter row is dropped, and any committed document already numbered in that series is moved
	aside so the restarted counter cannot collide with it.
	"""
	frappe.db.delete("Series", {"name": key})
	frappe.db.sql(
		f"update `tab{doctype}` set name = concat('zz-isolated-', name) where name like %s",
		(key + "%",),
	)


class TestBENamingParts(UnitTestCase):
	def test_be_parts_from_posting_date(self):
		doc = frappe._dict(posting_date="2026-09-30")
		self.assertEqual((be_yy(doc, "BEYY"), be_mm(doc, "BEMM")), ("69", "09"))

	def test_be_parts_from_transaction_date(self):
		doc = frappe._dict(transaction_date="2027-01-02")
		self.assertEqual((be_yy(doc, "BEYY"), be_mm(doc, "BEMM")), ("70", "01"))

	def test_be_parts_default_to_today(self):
		today = getdate(nowdate())
		expected = (f"{(today.year + 543) % 100:02d}", f"{today.month:02d}")
		self.assertEqual((be_yy(None, "BEYY"), be_mm(None, "BEMM")), expected)
		self.assertEqual((be_yy(SimpleNamespace(), "BEYY"), be_mm(frappe._dict(), "BEMM")), expected)


class TestBENaming(IntegrationTestCase):
	@classmethod
	def setUpClass(cls):
		super().setUpClass()
		ensure_test_company()
		frappe.db.commit()  # master data only
		_clear_naming_caches()

	def setUp(self):
		self.customer = make_customer()
		self.item = make_item(opening_qty=0)

	def tearDown(self):
		frappe.db.rollback()
		_clear_naming_caches()

	def test_default_series_for_the_four_doctypes(self):
		for doctype, series in DEFAULT_SERIES.items():
			field = frappe.get_meta(doctype).get_field("naming_series")
			self.assertEqual(field.options, series, doctype)
			self.assertEqual(field.default, series, doctype)

	def test_back_dated_invoice_uses_document_month(self):
		# Review Focus 4: posting date in a previous month -> number from the document's BE year/month
		si = make_si(self.customer, [self.item], posting_date="2026-09-30", submit=False)
		self.assertRegex(si.name, r"^IV6909-\d{4}$")

	def test_new_month_starts_a_fresh_series(self):
		_isolate_series("IV6910-", "Sales Invoice")
		first = make_si(self.customer, [self.item], posting_date="2026-10-15", submit=False)
		second = make_si(self.customer, [self.item], posting_date="2026-10-31", submit=False)
		self.assertEqual((first.name, second.name), ("IV6910-0001", "IV6910-0002"))

	def test_unticked_posting_time_numbers_by_the_date_erpnext_will_post(self):
		# without `set_posting_time` ERPNext resets posting_date to today on validate; the number follows
		si = frappe.get_doc(
			{
				"doctype": "Sales Invoice",
				"company": TEST_COMPANY,
				"customer": self.customer,
				"posting_date": "2026-09-30",
				"currency": CURRENCY,
				"selling_price_list": PRICE_LIST,
				"items": [{"item_code": self.item, "qty": 1, "rate": 100}],
			}
		).insert(ignore_permissions=True)
		today = getdate(nowdate())
		self.assertEqual(getdate(si.posting_date), today)
		self.assertTrue(si.name.startswith(f"IV{(today.year + 543) % 100:02d}{today.month:02d}-"), si.name)

	def test_sales_order_uses_transaction_date(self):
		so = make_so(self.customer, [self.item], submit=False)
		today = getdate(so.transaction_date)
		self.assertRegex(so.name, rf"^SO{(today.year + 543) % 100:02d}{today.month:02d}-\d{{4}}$")

	def test_changing_a_naming_rule_prefix(self):
		settings = frappe.get_single("Town Settings")
		settings.set("naming_rules", [{"document_type": "Sales Invoice", "prefix": "INV"}])
		settings.save()
		si = make_si(self.customer, [self.item], posting_date="2026-10-15", submit=False)
		self.assertTrue(si.name.startswith("INV69"), si.name)
		self.assertRegex(si.name, r"^INV6910-\d{4}$")
		# doctypes without a rule keep their default prefix
		self.assertEqual(
			frappe.get_meta("Sales Order").get_field("naming_series").options, "SO.BEYY.BEMM.-.####"
		)

		settings.set("naming_rules", [])
		settings.save()
		si = make_si(self.customer, [self.item], posting_date="2026-10-15", submit=False)
		self.assertRegex(si.name, r"^IV6910-\d{4}$")

	def test_invalid_prefix_is_rejected(self):
		settings = frappe.get_single("Town Settings")
		settings.set("naming_rules", [{"document_type": "Sales Invoice", "prefix": "IV@"}])
		with self.assertRaises(frappe.ValidationError):
			settings.save()


class TestThaiFieldsAndTaxId(IntegrationTestCase):
	@classmethod
	def setUpClass(cls):
		super().setUpClass()
		ensure_test_company()
		frappe.db.commit()  # master data only

	def tearDown(self):
		frappe.db.rollback()

	def test_branch_code_field_on_company_customer_address(self):
		for doctype in ("Company", "Customer", "Address"):
			field = frappe.get_meta(doctype).get_field("th_branch_code")
			self.assertIsNotNone(field, doctype)
			self.assertEqual(
				(field.fieldtype, field.default, field.length, field.label),
				("Data", "00000", 5, "รหัสสาขา"),
				doctype,
			)

	def test_new_customer_defaults_to_head_office(self):
		customer = frappe.get_doc("Customer", make_customer())
		self.assertEqual(customer.th_branch_code, "00000")

	def test_invalid_customer_tax_id_blocks_save(self):
		customer = frappe.get_doc("Customer", make_customer())
		customer.tax_id = INVALID_TAX_ID
		with self.assertRaises(frappe.ValidationError) as ctx:
			customer.save()
		self.assertIn("เลขประจำตัวผู้เสียภาษีไม่ถูกต้อง", str(ctx.exception))
		self.assertIn(INVALID_TAX_ID, str(ctx.exception))

	def test_valid_and_empty_customer_tax_id_save(self):
		customer = frappe.get_doc("Customer", make_customer())
		customer.tax_id = VALID_TAX_ID
		customer.save()
		customer.tax_id = ""
		customer.save()
		self.assertEqual(frappe.db.get_value("Customer", customer.name, "tax_id") or "", "")

	def test_invalid_company_tax_id_blocks_save(self):
		company = frappe.get_doc("Company", TEST_COMPANY)
		company.tax_id = "12345"
		with self.assertRaises(frappe.ValidationError) as ctx:
			company.save()
		self.assertIn("เลขประจำตัวผู้เสียภาษีไม่ถูกต้อง", str(ctx.exception))


class TestThaiChartFile(UnitTestCase):
	def test_chart_is_marked_draft_and_in_erpnext_format(self):
		chart = json.loads(CHART.read_text(encoding="utf-8"))
		self.assertIn("draft — accountant review required", chart["_comment"])
		self.assertEqual(chart["country_code"], "th")
		self.assertEqual(set(chart["tree"]), set(ROOTS))
		readme = (CHART.parent / "README.md").read_text(encoding="utf-8")
		self.assertIn("draft — accountant review required", readme)


class TestCreateThaiCompany(IntegrationTestCase):
	"""The company is created inside each test's transaction and rolled back (no implicit commit)."""

	@classmethod
	def setUpClass(cls):
		super().setUpClass()
		ensure_test_company()  # base masters (tree roots, UOM, fiscal years) as on a blank test site
		frappe.db.commit()

	@classmethod
	def tearDownClass(cls):
		assert not frappe.db.exists("Company", THAI_COMPANY), "create_thai_company committed its company"
		super().tearDownClass()

	def tearDown(self):
		frappe.db.rollback()
		frappe.clear_cache()
		frappe.cache.delete_value("fiscal_years")

	def _create(self) -> str:
		from erp_town.thai.setup import create_thai_company

		return create_thai_company(THAI_COMPANY, THAI_ABBR, VALID_TAX_ID)

	def _account(self, account_name: str) -> str:
		return frappe.db.get_value("Account", {"company": THAI_COMPANY, "account_name": account_name})

	def test_creates_company_chart_defaults_and_vat_templates(self):
		self.assertEqual(self._create(), THAI_COMPANY)
		company = frappe.get_doc("Company", THAI_COMPANY)
		self.assertEqual(
			(company.abbr, company.tax_id, company.country), (THAI_ABBR, VALID_TAX_ID, "Thailand")
		)
		self.assertEqual((company.default_currency, company.valuation_method), ("THB", "Moving Average"))
		self.assertEqual(company.th_branch_code, "00000")

		for name, root_type in ROOTS.items():
			row = frappe.db.get_value(
				"Account",
				{"company": THAI_COMPANY, "account_name": name},
				["is_group", "root_type", "parent_account"],
				as_dict=True,
			)
			self.assertEqual((row.is_group, row.root_type, row.parent_account), (1, root_type, None), name)
		for name, account_type in LEAVES.items():
			row = frappe.db.get_value(
				"Account",
				{"company": THAI_COMPANY, "account_name": name},
				["is_group", "account_type"],
				as_dict=True,
			)
			self.assertIsNotNone(row, name)
			self.assertEqual((row.is_group, row.account_type), (0, account_type), name)

		defaults = {
			"default_cash_account": "เงินสด",
			"default_bank_account": "เงินฝากธนาคาร",
			"default_receivable_account": "ลูกหนี้การค้า",
			"default_payable_account": "เจ้าหนี้การค้า",
			"default_income_account": "รายได้จากการขาย",
			"default_expense_account": "ต้นทุนขาย",
			"default_inventory_account": "สินค้าคงเหลือ",
			"stock_received_but_not_billed": "สินค้ารับแล้วยังไม่ได้รับใบแจ้งหนี้",
			"stock_adjustment_account": "ผลต่างจากการปรับปรุงสต็อก",
			"round_off_account": "ปัดเศษ",
			"depreciation_expense_account": "ค่าเสื่อมราคา",
			"default_discount_account": "ส่วนลดจ่าย",
		}
		for fieldname, account_name in defaults.items():
			self.assertEqual(company.get(fieldname), self._account(account_name), fieldname)
		self.assertTrue(frappe.db.exists("Warehouse", {"company": THAI_COMPANY, "is_group": 0}))
		self.assertTrue(frappe.db.exists("Cost Center", {"company": THAI_COMPANY, "is_group": 0}))

		vat = self._account("ภาษีขาย")
		for title, included in (("ภาษีขาย 7%", 0), ("ภาษีขาย 7% (ราคารวม VAT)", 1)):
			name = frappe.db.get_value(
				"Sales Taxes and Charges Template", {"company": THAI_COMPANY, "title": title}
			)
			self.assertIsNotNone(name, title)
			taxes = frappe.get_doc("Sales Taxes and Charges Template", name).taxes
			self.assertEqual(
				[(t.charge_type, t.account_head, t.rate, t.included_in_print_rate) for t in taxes],
				[("On Net Total", vat, 7, included)],
				title,
			)
		for title in ("ยกเว้น VAT", "VAT 0%"):
			self.assertTrue(
				frappe.db.exists("Item Tax Template", {"company": THAI_COMPANY, "title": title}), title
			)
		self.assertTrue(frappe.db.get_single_value("Stock Settings", "valuation_method"))

	def test_is_idempotent(self):
		self._create()
		self.assertEqual(self._create(), THAI_COMPANY)
		self.assertEqual(frappe.db.count("Sales Taxes and Charges Template", {"company": THAI_COMPANY}), 2)
		self.assertEqual(frappe.db.count("Item Tax Template", {"company": THAI_COMPANY}), 2)
		self.assertEqual(frappe.db.count("Account", {"company": THAI_COMPANY, "account_name": "ภาษีขาย"}), 1)

	def test_works_without_fiscal_years(self):
		# a site where ERPNext's setup wizard never ran has no fiscal year (rolled back in tearDown)
		frappe.db.delete("Fiscal Year Company")
		frappe.db.delete("Fiscal Year")
		frappe.cache.delete_value("fiscal_years")
		self._create()
		today = nowdate()
		self.assertTrue(
			frappe.db.exists(
				"Fiscal Year", {"year_start_date": ["<=", today], "year_end_date": [">=", today]}
			)
		)

	def test_rejects_invalid_tax_id(self):
		from erp_town.thai.setup import create_thai_company

		with self.assertRaises(frappe.ValidationError):
			create_thai_company(THAI_COMPANY, THAI_ABBR, "0000000000000")
