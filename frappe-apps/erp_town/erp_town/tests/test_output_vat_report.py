# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Sales VAT report `รายงานภาษีขาย` (spec §5.4).

Runs on `test.localhost` only. Invoices are dated in January / February of last year (a period no other
test module uses, inside the fiscal years the factories create) and rolled back after every test.
"""

import frappe
from frappe.tests import IntegrationTestCase
from frappe.utils import getdate, nowdate

from erp_town.tests.factories import TEST_COMPANY, ensure_test_company, make_customer, make_item, make_si
from erp_town.thai.dates import be_date

REPORT = "รายงานภาษีขาย"
COLUMNS = [
	"idx",
	"posting_date_be",
	"invoice_no",
	"customer_name",
	"customer_tax_id",
	"customer_branch",
	"net_amount",
	"vat_amount",
]
CUSTOMER_TAX_ID = "0994000165889"  # checksum-valid
YEAR = getdate(nowdate()).year - 1
IN_PERIOD = (f"{YEAR}-01-05", f"{YEAR}-01-12", f"{YEAR}-01-20", f"{YEAR}-01-31")
OTHER_MONTH = f"{YEAR}-02-01"


class TestOutputVatReport(IntegrationTestCase):
	@classmethod
	def setUpClass(cls):
		super().setUpClass()
		ensure_test_company()
		frappe.db.commit()  # master data only

	def setUp(self):
		self.vat_account = self._output_vat_account()
		self.customer = make_customer()
		frappe.db.set_value("Customer", self.customer, {"tax_id": CUSTOMER_TAX_ID, "th_branch_code": "00000"})
		self.customer_name = frappe.db.get_value("Customer", self.customer, "customer_name")
		self.item = make_item()

	def tearDown(self):
		frappe.db.rollback()

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

	def _si(self, posting_date: str, rate: float, *, inclusive: bool = False, customer: str | None = None):
		si = make_si(
			customer or self.customer,
			[{"item_code": self.item, "qty": 1, "rate": rate}],
			submit=False,
			posting_date=posting_date,
		)
		si.append(
			"taxes",
			{
				"charge_type": "On Net Total",
				"account_head": self.vat_account,
				"rate": 7,
				"description": "VAT 7%",
				"included_in_print_rate": int(inclusive),
			},
		)
		si.save()
		si.submit()
		return si

	def _run(self, month: int = 1, year: int = YEAR):
		from erp_town.thai_localization.report.รายงานภาษีขาย.รายงานภาษีขาย import execute

		return execute(frappe._dict(company=TEST_COMPANY, month=str(month), year=year))

	def test_report_is_a_standard_exportable_script_report(self):
		report = frappe.get_doc("Report", REPORT)
		self.assertEqual(
			(report.report_type, report.is_standard, report.module, report.ref_doctype, report.disabled),
			("Script Report", "Yes", "Thai Localization", "Sales Invoice", 0),
		)
		self.assertTrue({"Accounts User", "Accounts Manager"} <= {r.role for r in report.roles})

	def test_rows_total_and_cancelled_invoice(self):
		exclusive = self._si(IN_PERIOD[0], 1000)
		inclusive = self._si(IN_PERIOD[1], 1070, inclusive=True)
		cancelled = self._si(IN_PERIOD[2], 500)
		cancelled.cancel()
		self._si(OTHER_MONTH, 9999)  # another month: excluded

		columns, data = self._run()

		self.assertEqual([c["fieldname"] for c in columns], COLUMNS)
		self.assertEqual(len(data), 4)
		rows, total = data[:3], data[3]
		self.assertEqual([r["invoice_no"] for r in rows], [exclusive.name, inclusive.name, cancelled.name])
		self.assertEqual([r["idx"] for r in rows], [1, 2, 3])
		self.assertEqual(rows[0]["posting_date_be"], be_date(IN_PERIOD[0], "short"))
		self.assertEqual([(r["net_amount"], r["vat_amount"]) for r in rows], [(1000, 70), (1000, 70), (0, 0)])
		self.assertEqual(rows[0]["customer_name"], self.customer_name)
		self.assertEqual(rows[2]["customer_name"], f"{self.customer_name} (ยกเลิก)")
		self.assertEqual(rows[0]["customer_tax_id"], CUSTOMER_TAX_ID)
		self.assertEqual(rows[0]["customer_branch"], "00000")
		self.assertEqual(total["invoice_no"], "รวม")
		self.assertEqual(
			(f"{total['net_amount']:,.2f}", f"{total['vat_amount']:,.2f}"), ("2,000.00", "140.00")
		)

	def test_other_month_has_only_its_own_invoices(self):
		self._si(IN_PERIOD[0], 1000)
		other = self._si(OTHER_MONTH, 2000)

		_columns, data = self._run(month=2)

		self.assertEqual([r["invoice_no"] for r in data], [other.name, "รวม"])
		self.assertEqual((data[-1]["net_amount"], data[-1]["vat_amount"]), (2000, 140))

	def test_credit_note_is_included_with_negative_amounts(self):
		from erpnext.accounts.doctype.sales_invoice.sales_invoice import make_sales_return

		si = self._si(IN_PERIOD[0], 1000)
		credit_note = make_sales_return(si.name)
		credit_note.set_posting_time = 1
		credit_note.posting_date = IN_PERIOD[3]
		credit_note.due_date = IN_PERIOD[3]
		credit_note.insert(ignore_permissions=True)
		credit_note.submit()

		_columns, data = self._run()

		self.assertEqual(
			[(r["invoice_no"], r["net_amount"], r["vat_amount"]) for r in data],
			[(si.name, 1000, 70), (credit_note.name, -1000, -70), ("รวม", 0, 0)],
		)

	def test_rows_are_ordered_by_posting_date_then_name(self):
		later = self._si(IN_PERIOD[1], 100)
		first = self._si(IN_PERIOD[0], 100)
		same_day = self._si(IN_PERIOD[0], 100)

		_columns, data = self._run()

		self.assertEqual(
			[r["invoice_no"] for r in data[:-1]], [*sorted([first.name, same_day.name]), later.name]
		)

	def test_customer_branch_uses_the_shared_branch_rule(self):
		from erp_town.thai.printing import branch_code

		frappe.db.set_value("Customer", self.customer, "th_branch_code", "00003")
		self._si(IN_PERIOD[0], 1000)

		_columns, data = self._run()

		self.assertEqual(data[0]["customer_branch"], "00003")
		self.assertEqual(branch_code(None, "00003"), "00003")

	def test_only_vat_rows_on_the_output_vat_account_count(self):
		si = make_si(
			self.customer,
			[{"item_code": self.item, "qty": 1, "rate": 1000}],
			submit=False,
			posting_date=IN_PERIOD[0],
		)
		freight = frappe.db.get_value(
			"Account", {"company": TEST_COMPANY, "account_name": "Freight and Forwarding Charges"}
		)
		si.append(
			"taxes",
			{"charge_type": "Actual", "account_head": freight, "tax_amount": 50, "description": "ค่าขนส่ง"},
		)
		si.append(
			"taxes",
			{
				"charge_type": "On Net Total",
				"account_head": self.vat_account,
				"rate": 7,
				"description": "VAT 7%",
			},
		)
		si.save()
		si.submit()

		_columns, data = self._run()

		self.assertEqual((data[0]["net_amount"], data[0]["vat_amount"]), (1000, 70))

	def test_report_runs_through_desk(self):
		from frappe.desk.query_report import get_script, run

		self._si(IN_PERIOD[0], 1000)
		result = run(REPORT, filters={"company": TEST_COMPANY, "month": "1", "year": YEAR})
		self.assertEqual(result["result"][-1]["invoice_no"], "รวม")
		script = get_script(REPORT)["script"]
		for fieldname in ('"company"', '"month"', '"year"'):
			self.assertIn(fieldname, script)
