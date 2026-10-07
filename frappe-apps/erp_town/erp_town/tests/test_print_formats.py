# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Thai print formats (spec §5.3), Desk theme tokens (§4.5) and Thai UI translations (§5.5).

Runs on `test.localhost` only. Documents and the tax IDs / branch codes set on the test company are rolled
back after every test.
"""

import csv
import re
from pathlib import Path

import frappe
from frappe.tests import IntegrationTestCase, UnitTestCase

from erp_town.tests.factories import (
	TEST_COMPANY,
	ensure_test_company,
	make_customer,
	make_dn_from_so,
	make_item,
	make_pe_for_si,
	make_si,
	make_so,
)
from erp_town.thai.bahttext import bahttext
from erp_town.thai.dates import be_date

APP = Path(frappe.get_app_path("erp_town"))
TAX_INVOICE = "ใบกำกับภาษี/ใบแจ้งหนี้"
FORMATS = {
	"ใบยืนยันคำสั่งซื้อ": "Sales Order",
	"ใบส่งของ": "Delivery Note",
	TAX_INVOICE: "Sales Invoice",
	"ใบเสร็จรับเงิน": "Payment Entry",
}
COMPANY_TAX_ID = "0105555123450"  # checksum-valid
CUSTOMER_TAX_ID = "0994000165889"  # checksum-valid
VAT_ACCOUNT = "VAT - ETT"  # leaf of ERPNext's standard chart on the test company
THAI = re.compile("[ก-๛]")  # Thai block: strings already written in Thai need no entry
MESSAGE = re.compile(r'(?<![\w.])_\(\s*"([^"]*)"')  # _("literal") in the app's Python
FONT_URLS = ("/assets/erp_town/fonts/Sarabun-Regular.ttf", "/assets/erp_town/fonts/Sarabun-Bold.ttf")


def _print(doctype: str, name: str, print_format: str) -> str:
	return frappe.get_print(doctype, name, print_format=print_format, no_letterhead=1)


class TestThaiPrintFormats(IntegrationTestCase):
	@classmethod
	def setUpClass(cls):
		super().setUpClass()
		ensure_test_company()
		frappe.db.commit()  # master data only

	def setUp(self):
		frappe.db.set_value("Company", TEST_COMPANY, {"tax_id": COMPANY_TAX_ID, "th_branch_code": "00000"})
		self.customer = make_customer()
		frappe.db.set_value("Customer", self.customer, "tax_id", CUSTOMER_TAX_ID)
		self.item = make_item()

	def tearDown(self):
		frappe.db.rollback()
		frappe.clear_document_cache("Company", TEST_COMPANY)

	def _si_with_vat(self):
		si = make_si(self.customer, [{"item_code": self.item, "qty": 2, "rate": 1250.5}], submit=False)
		si.append(
			"taxes",
			{"charge_type": "On Net Total", "account_head": VAT_ACCOUNT, "rate": 7, "description": "VAT 7%"},
		)
		si.save()
		si.submit()
		return si

	def test_formats_are_standard_jinja_in_thai_localization(self):
		for name, doctype in FORMATS.items():
			pf = frappe.get_doc("Print Format", name)
			self.assertEqual(
				(pf.doc_type, pf.standard, pf.print_format_type, pf.module, pf.disabled),
				(doctype, "Yes", "Jinja", "Thai Localization", 0),
				name,
			)

	def test_tax_invoice_is_the_sales_invoice_default(self):
		self.assertEqual(frappe.get_meta("Sales Invoice").default_print_format, TAX_INVOICE)

	def test_default_print_format_replaces_erpnext_default_but_keeps_a_chosen_one(self):
		from frappe.custom.doctype.property_setter.property_setter import make_property_setter

		from erp_town.thai.setup import ensure_default_print_formats

		def set_default(value):
			make_property_setter(
				"Sales Invoice", None, "default_print_format", value, "Data", for_doctype=True
			)
			frappe.clear_cache(doctype="Sales Invoice")

		try:
			set_default("Sales Invoice Modern with Images")  # what ERPNext's installer sets
			ensure_default_print_formats()
			self.assertEqual(frappe.get_meta("Sales Invoice").default_print_format, TAX_INVOICE)

			set_default("Sales Invoice Return")  # chosen in Customize Form: kept
			ensure_default_print_formats()
			ensure_default_print_formats()
			self.assertEqual(frappe.get_meta("Sales Invoice").default_print_format, "Sales Invoice Return")
		finally:
			frappe.db.rollback()
			frappe.clear_cache(doctype="Sales Invoice")

	def test_tax_invoice_has_section_86_4_elements(self):
		si = self._si_with_vat()
		html = _print("Sales Invoice", si.name, TAX_INVOICE)
		for needle in [
			"ใบกำกับภาษี",
			TEST_COMPANY,
			COMPANY_TAX_ID,
			si.customer_name,
			CUSTOMER_TAX_ID,
			"สำนักงานใหญ่",
			si.name,
			be_date(si.posting_date),
			self.item,
			"2,501.00",  # line amount 2 x 1,250.50
			"175.07",  # VAT on its own line
			"ภาษีมูลค่าเพิ่ม 7%",
			"2,676.07",  # grand total
			bahttext(si.grand_total),
		]:
			self.assertIn(needle, html)

	def test_branch_label(self):
		frappe.db.set_value("Company", TEST_COMPANY, "th_branch_code", "00002")
		frappe.db.set_value("Customer", self.customer, "th_branch_code", "00001")
		si = self._si_with_vat()
		html = _print("Sales Invoice", si.name, TAX_INVOICE)
		self.assertIn("สาขาที่ 00002", html)
		self.assertIn("สาขาที่ 00001", html)
		self.assertNotIn("สำนักงานใหญ่", html)

		frappe.db.set_value("Customer", self.customer, "th_branch_code", "00000")
		html = _print("Sales Invoice", si.name, TAX_INVOICE)
		self.assertIn("สำนักงานใหญ่", html)
		self.assertNotIn("สาขาที่ 00001", html)

	def test_receipt_references_invoice(self):
		si = self._si_with_vat()
		pe = make_pe_for_si(si)
		html = _print("Payment Entry", pe.name, "ใบเสร็จรับเงิน")
		for needle in [
			"ใบเสร็จรับเงิน",
			pe.name,
			be_date(pe.posting_date),
			si.name,
			CUSTOMER_TAX_ID,
			bahttext(pe.paid_amount),
		]:
			self.assertIn(needle, html)

	def test_dn_and_so_render(self):
		so = make_so(self.customer, [{"item_code": self.item, "qty": 2}])
		dn = make_dn_from_so(so)
		so_html = _print("Sales Order", so.name, "ใบยืนยันคำสั่งซื้อ")
		for needle in ["ใบยืนยันคำสั่งซื้อ", so.name, be_date(so.transaction_date), self.item]:
			self.assertIn(needle, so_html)
		dn_html = _print("Delivery Note", dn.name, "ใบส่งของ")
		for needle in ["ใบส่งของ", dn.name, be_date(dn.posting_date), self.item, so.name]:
			self.assertIn(needle, dn_html)

	def test_formats_embed_sarabun(self):
		si = self._si_with_vat()
		html = _print("Sales Invoice", si.name, TAX_INVOICE)
		self.assertIn("@font-face", html)
		for url in FONT_URLS:
			self.assertIn(url, html)


class TestBranchLabel(UnitTestCase):
	def test_head_office_and_branches(self):
		from erp_town.thai.printing import th_branch_label

		for code in (None, "", "00000", " 00000 "):
			self.assertEqual(th_branch_label(code), "สำนักงานใหญ่", repr(code))
		self.assertEqual(th_branch_label("00001"), "สาขาที่ 00001")


class TestThemeAndFonts(UnitTestCase):
	def test_sarabun_fonts_and_licence_are_shipped(self):
		fonts = APP / "public" / "fonts"
		for name in ("Sarabun-Regular.ttf", "Sarabun-Bold.ttf"):
			data = (fonts / name).read_bytes()
			self.assertEqual(data[:4], b"\x00\x01\x00\x00", name)  # TrueType sfnt version
			self.assertGreater(len(data), 50_000, name)
		self.assertIn("SIL Open Font License", (fonts / "OFL.txt").read_text(encoding="utf-8"))

	def test_print_css_declares_sarabun_font_faces(self):
		css = (APP / "public" / "css" / "print-th.css").read_text(encoding="utf-8")
		self.assertEqual(css.count("@font-face"), 2)
		for url in FONT_URLS:
			self.assertIn(url, css)

	def test_desk_theme_tokens(self):
		self.assertIn("/assets/erp_town/css/town-desk.css", frappe.get_hooks("app_include_css"))
		css = (APP / "public" / "css" / "town-desk.css").read_text(encoding="utf-8")
		for token in ("--town-primary:", "--town-surface:", "--town-radius:", "--town-font-ui:"):
			self.assertIn(token, css)


class TestThaiTranslations(UnitTestCase):
	def setUp(self):
		frappe.translate.clear_cache()

	def tearDown(self):
		frappe.translate.clear_cache()

	def test_app_labels_translate_to_thai(self):
		self.assertEqual(frappe._("Town Event Outbox", lang="th"), "กล่องขาออกเหตุการณ์ของเมือง")
		self.assertEqual(frappe._("Max Publish Attempts", lang="th"), "จำนวนครั้งสูงสุดที่พยายามส่ง")
		self.assertEqual(
			frappe._("Account {0} not found for company {1}", lang="th"), "ไม่พบบัญชี {0} ของบริษัท {1}"
		)

	def _untranslated(self, strings) -> list[str]:
		with open(APP / "translations" / "th.csv", encoding="utf-8", newline="") as f:
			translated = {row[0] for row in csv.reader(f) if row}
		frappe_th = frappe.translate.get_translations_from_apps("th", apps=["frappe", "erpnext", "hrms"])
		return [s for s in strings if s not in translated and s not in frappe_th and not THAI.search(s)]

	def test_every_doctype_label_has_a_thai_translation(self):
		labels = []
		for path in (APP / "erp_town" / "doctype").glob("*/*.json"):
			meta = frappe.parse_json(path.read_text(encoding="utf-8"))
			labels += [meta["name"]] + [
				text for df in meta["fields"] for text in (df.get("label"), df.get("description")) if text
			]
		self.assertTrue(labels)
		self.assertEqual(self._untranslated(labels), [])

	def test_every_app_message_has_a_thai_translation(self):
		messages = []
		for path in APP.rglob("*.py"):
			if "tests" not in path.parts:
				messages += MESSAGE.findall(path.read_text(encoding="utf-8"))
		self.assertTrue(messages)
		self.assertEqual(self._untranslated(messages), [])
