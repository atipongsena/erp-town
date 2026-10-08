from datetime import UTC, date, datetime
from decimal import Decimal

import frappe
from frappe.tests import IntegrationTestCase, UnitTestCase

from erp_town.thai.bahttext import bahttext
from erp_town.thai.dates import be_date
from erp_town.thai.taxid import is_valid_tax_id

CASES = [
	(0, "ศูนย์บาทถ้วน"),
	(1, "หนึ่งบาทถ้วน"),
	(10, "สิบบาทถ้วน"),
	(11, "สิบเอ็ดบาทถ้วน"),
	(20, "ยี่สิบบาทถ้วน"),
	(21, "ยี่สิบเอ็ดบาทถ้วน"),
	(100, "หนึ่งร้อยบาทถ้วน"),
	(101, "หนึ่งร้อยเอ็ดบาทถ้วน"),
	(120, "หนึ่งร้อยยี่สิบบาทถ้วน"),
	("1250.50", "หนึ่งพันสองร้อยห้าสิบบาทห้าสิบสตางค์"),
	(1_000_000, "หนึ่งล้านบาทถ้วน"),
	(2_000_000, "สองล้านบาทถ้วน"),
	(1_000_001, "หนึ่งล้านเอ็ดบาทถ้วน"),  # a trailing 1 after any higher digit / group is เอ็ด
	(2_000_001, "สองล้านเอ็ดบาทถ้วน"),
	(11_000_000, "สิบเอ็ดล้านบาทถ้วน"),
	("21000021.21", "ยี่สิบเอ็ดล้านยี่สิบเอ็ดบาทยี่สิบเอ็ดสตางค์"),
	("0.25", "ยี่สิบห้าสตางค์"),
	("0.01", "หนึ่งสตางค์"),
	(1.005, "หนึ่งบาทหนึ่งสตางค์"),
	(10_000_000, "สิบล้านบาทถ้วน"),
	(1_000_000_000_000, "หนึ่งล้านล้านบาทถ้วน"),
	(-5, "ลบห้าบาทถ้วน"),
	("100000.10", "หนึ่งแสนบาทสิบสตางค์"),
]

# Valid fixture computed with s = Σ d[i]·(13−i), i=0..11: s = 194, (11 − 194 mod 11) mod 10 = 4 = d[12].
VALID_TAX_ID = "0105536112014"


class TestThaiUtils(UnitTestCase):
	"""Pure functions: UnitTestCase (no DB transaction/rollback needed); runs fine under bench run-tests."""

	def test_bahttext(self):
		for amount, words in CASES:
			self.assertEqual(bahttext(amount), words, amount)

	def test_bahttext_accepts_decimal(self):
		self.assertEqual(bahttext(Decimal("1250.50")), "หนึ่งพันสองร้อยห้าสิบบาทห้าสิบสตางค์")

	def test_tax_id(self):
		self.assertTrue(is_valid_tax_id(VALID_TAX_ID))
		self.assertFalse(is_valid_tax_id("0105536112015"))
		self.assertFalse(is_valid_tax_id("010553611201"))
		self.assertFalse(is_valid_tax_id("01055361120a4"))

	def test_tax_id_rejects_non_ascii_and_non_str(self):
		self.assertFalse(is_valid_tax_id(" 0105536112014"))
		self.assertFalse(is_valid_tax_id("0105536112014 "))
		self.assertFalse(is_valid_tax_id("๐๑๐๕๕๓๖๑๑๒๐๑๔"))  # Thai digits are not ASCII
		self.assertFalse(is_valid_tax_id(105536112014))
		self.assertFalse(is_valid_tax_id(None))

	def test_be_date(self):
		self.assertEqual(be_date(date(2026, 10, 7)), "7 ตุลาคม 2569")
		self.assertEqual(be_date("2026-01-05", "short"), "05/01/2569")

	def test_be_date_datetime_inputs(self):
		self.assertEqual(be_date(datetime(2026, 12, 31, 23, 59, tzinfo=UTC)), "31 ธันวาคม 2569")
		self.assertEqual(be_date("2026-02-01 10:00:00", "short"), "01/02/2569")


class TestThaiJinjaFilters(IntegrationTestCase):
	def test_filters_registered_via_hooks(self):
		out = frappe.render_template(
			"{{ d | be_date }} {{ a | bahttext }}", {"d": date(2026, 10, 7), "a": "1250.50"}
		)
		self.assertEqual(out, "7 ตุลาคม 2569 หนึ่งพันสองร้อยห้าสิบบาทห้าสิบสตางค์")
