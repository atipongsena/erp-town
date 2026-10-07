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
OTHER_CHARGE_ACCOUNT = "Freight and Forwarding Charges - ETT"  # ERPNext standard chart, test company
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
		self.vat_account = self._output_vat_account()

	def tearDown(self):
		frappe.db.rollback()
		frappe.clear_document_cache("Company", TEST_COMPANY)

	def _output_vat_account(self) -> str:
		"""The Thai output-VAT leaf `ภาษีขาย` (a Thai company's chart has it; added here inside the test)."""
		parent = frappe.db.get_value(
			"Account", {"company": TEST_COMPANY, "account_name": "Duties and Taxes", "is_group": 1}
		)
		return (
			frappe.get_doc(
				{
					"doctype": "Account",
					"account_name": "ภาษีขาย",
					"parent_account": parent,
					"company": TEST_COMPANY,
					"account_type": "Tax",
					"is_group": 0,
				}
			)
			.insert(ignore_permissions=True)
			.name
		)

	def _vat_row(self) -> dict:
		return {
			"charge_type": "On Net Total",
			"account_head": self.vat_account,
			"rate": 7,
			"description": "VAT 7%",
		}

	def _with_taxes(self, doc, taxes: list[dict], submit: bool = True):
		for row in taxes:
			doc.append("taxes", row)
		doc.save()
		if submit:
			doc.submit()
		return doc

	def _si_with_vat(self, rate: float = 1250, qty: float = 2, extra_taxes: list[dict] | None = None):
		"""Default 2 x 1,250.00 + VAT 7% = 2,675.00: grand total and rounded total agree."""
		si = make_si(self.customer, [{"item_code": self.item, "qty": qty, "rate": rate}], submit=False)
		return self._with_taxes(si, [*(extra_taxes or []), self._vat_row()])

	def _address(self, link_doctype: str, link_name: str, line1: str, branch_code: str) -> str:
		if not frappe.db.exists("Address Template", {"country": "Thailand"}):
			# ERPNext renders every saved address; a blank test site (no setup wizard) has no template
			frappe.get_doc({"doctype": "Address Template", "country": "Thailand", "is_default": 1}).insert(
				ignore_permissions=True
			)
		return (
			frappe.get_doc(
				{
					"doctype": "Address",
					"address_title": line1,
					"address_type": "Billing",
					"address_line1": line1,
					"city": "กรุงเทพมหานคร",
					"country": "Thailand",
					"th_branch_code": branch_code,
					"is_your_company_address": int(link_doctype == "Company"),
					"links": [{"link_doctype": link_doctype, "link_name": link_name}],
				}
			)
			.insert(ignore_permissions=True)
			.name
		)

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
			"1,250.00",  # unit price
			"2,500.00",  # line amount 2 x 1,250.00
			"175.00",  # VAT on its own line
			"ภาษีมูลค่าเพิ่ม 7%",
			"2,675.00",  # grand total
			bahttext(si.grand_total),
		]:
			self.assertIn(needle, html)
		self.assertEqual(si.rounded_total, si.grand_total)
		self.assertNotIn("ปัดเศษ", html)

	def test_tax_invoice_rounding_prints_adjustment_and_payable_in_words(self):
		si = self._si_with_vat(rate=1250.5)  # 2,501.00 + 175.07 = 2,676.07, ERPNext rounds to 2,676.00
		self.assertEqual((si.grand_total, si.rounded_total, si.rounding_adjustment), (2676.07, 2676, -0.07))
		html = _print("Sales Invoice", si.name, TAX_INVOICE)
		for needle in [
			"2,676.07",
			"ปัดเศษ",
			"-0.07",
			"จำนวนเงินที่ต้องชำระ",
			"2,676.00",
			bahttext(si.rounded_total),
		]:
			self.assertIn(needle, html)
		self.assertNotIn(bahttext(si.grand_total), html)

	def test_sales_order_rounding_prints_adjustment_and_payable_in_words(self):
		so = make_so(self.customer, [{"item_code": self.item, "qty": 2, "rate": 1250.5}], submit=False)
		so = self._with_taxes(so, [self._vat_row()])
		self.assertEqual((so.grand_total, so.rounded_total), (2676.07, 2676))
		html = _print("Sales Order", so.name, "ใบยืนยันคำสั่งซื้อ")
		for needle in ["ภาษีมูลค่าเพิ่ม 7%", "175.07", "ปัดเศษ", "-0.07", "2,676.00", bahttext(so.rounded_total)]:
			self.assertIn(needle, html)
		self.assertNotIn(bahttext(so.grand_total), html)

	def test_vat_line_only_counts_output_vat_rows(self):
		freight = {
			"charge_type": "Actual",
			"account_head": OTHER_CHARGE_ACCOUNT,
			"tax_amount": 100,
			"description": "ค่าขนส่ง",
		}
		si = self._si_with_vat(extra_taxes=[freight])  # 2,500 + freight 100 + VAT 7% of 2,500
		self.assertEqual((si.total_taxes_and_charges, si.grand_total), (275, 2775))
		html = _print("Sales Invoice", si.name, TAX_INVOICE)
		self.assertRegex(html, r"ภาษีมูลค่าเพิ่ม 7%</td><td[^>]*>175\.00<")
		self.assertRegex(html, r"ค่าขนส่ง</td><td[^>]*>100\.00<")
		self.assertNotIn("275.00", html)

	def test_vat_inclusive_template_prints_net_amounts(self):
		from erpnext.controllers.accounts_controller import get_taxes_and_charges

		from erp_town.thai.setup import _ensure_sales_tax_templates

		_ensure_sales_tax_templates(TEST_COMPANY, self.vat_account)
		template = frappe.db.get_value(
			"Sales Taxes and Charges Template", {"company": TEST_COMPANY, "title": "ภาษีขาย 7% (ราคารวม VAT)"}
		)
		si = make_si(self.customer, [{"item_code": self.item, "qty": 1, "rate": 1070}], submit=False)
		si.taxes_and_charges = template
		si = self._with_taxes(si, get_taxes_and_charges("Sales Taxes and Charges Template", template))
		self.assertEqual((si.net_total, si.total_taxes_and_charges, si.grand_total), (1000, 70, 1070))
		html = _print("Sales Invoice", si.name, TAX_INVOICE)
		# item row and subtotal at the net price; net + VAT = grand total
		self.assertEqual(html.count("1,000.00"), 3)  # unit price, line amount, มูลค่าสินค้า/บริการ
		self.assertEqual(html.count("1,070.00"), 1)  # only the grand total
		self.assertRegex(html, r"ภาษีมูลค่าเพิ่ม 7%</td><td[^>]*>70\.00<")
		self.assertNotIn("รวมเงิน</td>", html)
		self.assertIn(bahttext(1070), html)

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

	def test_branch_with_linked_addresses(self):
		# buyer: branch customer whose (default-coded) head-office address is linked -> the customer's code;
		# seller: head-office company whose linked address is a branch -> the address's code
		frappe.db.set_value("Customer", self.customer, "th_branch_code", "00001")
		customer_address = self._address("Customer", self.customer, "99 ถนนพระราม 9", "00000")
		company_address = self._address("Company", TEST_COMPANY, "1 ถนนสีลม", "00002")
		si = make_si(self.customer, [{"item_code": self.item, "qty": 2, "rate": 1250}], submit=False)
		si.customer_address = customer_address
		si.company_address = company_address
		si = self._with_taxes(si, [self._vat_row()])
		self.assertEqual((si.customer_address, si.company_address), (customer_address, company_address))
		# render from the linked Address records, not the display text stored on the invoice
		frappe.db.set_value("Sales Invoice", si.name, {"address_display": "", "company_address_display": ""})
		html = _print("Sales Invoice", si.name, TAX_INVOICE)
		for needle in ["สาขาที่ 00001", "สาขาที่ 00002", "99 ถนนพระราม 9", "1 ถนนสีลม", "กรุงเทพมหานคร"]:
			self.assertIn(needle, html)
		self.assertNotIn("สำนักงานใหญ่", html)

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
