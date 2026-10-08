# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE
# ruff: noqa: N999 -- Frappe names a standard report's module after the report (Thai name)

"""Sales VAT report `รายงานภาษีขาย` (spec §5.4).

One row per submitted or cancelled Sales Invoice (credit notes included, negative) of the company dated in
the tax month, ordered by posting date then number, plus a final total row. Opening invoices
(`is_opening = Yes`, balances brought forward) are excluded. Amounts are in company
currency: `net_amount` is the invoice's `base_net_total`, `vat_amount` the sum of its tax rows on the
company's output-VAT account `ภาษีขาย`. Cancelled invoices keep their row, marked ` (ยกเลิก)`, with zero
amounts.

The folder and module carry the report's Thai name because Frappe resolves a standard report's code at
`<module>/report/<scrub(name)>/<scrub(name)>.py`.
"""

from datetime import date

import frappe
from frappe import _
from frappe.query_builder.functions import Sum
from frappe.utils import cint, flt, get_last_day, getdate, nowdate

from erp_town.thai.dates import be_date
from erp_town.thai.printing import HEAD_OFFICE_BRANCH, branch_code
from erp_town.thai.setup import VAT_OUTPUT_ACCOUNT, company_account

CANCELLED_SUFFIX = " (ยกเลิก)"
TOTAL_LABEL = "รวม"


def execute(filters=None):
	filters = frappe._dict(filters or {})
	if not filters.get("company"):
		frappe.throw(_("{0} is mandatory").format(_("Company")))
	return get_columns(), get_data(filters.company, *_period(filters))


def get_columns() -> list[dict]:
	return [
		{"fieldname": "idx", "label": "ลำดับ", "fieldtype": "Int", "width": 70},
		{"fieldname": "posting_date_be", "label": "วันที่", "fieldtype": "Data", "width": 100},
		{
			"fieldname": "invoice_no",
			"label": "เลขที่ใบกำกับภาษี",
			"fieldtype": "Link",
			"options": "Sales Invoice",
			"width": 140,
		},
		{"fieldname": "customer_name", "label": "ชื่อผู้ซื้อสินค้า/ผู้รับบริการ", "fieldtype": "Data", "width": 240},
		{
			"fieldname": "customer_tax_id",
			"label": "เลขประจำตัวผู้เสียภาษีของผู้ซื้อ",
			"fieldtype": "Data",
			"width": 160,
		},
		{"fieldname": "customer_branch", "label": "สาขา", "fieldtype": "Data", "width": 80},
		{"fieldname": "net_amount", "label": "มูลค่าสินค้าหรือบริการ", "fieldtype": "Currency", "width": 150},
		{"fieldname": "vat_amount", "label": "จำนวนเงินภาษีมูลค่าเพิ่ม", "fieldtype": "Currency", "width": 150},
	]


def get_data(company: str, start: date, end: date) -> list[dict]:
	vat_account = company_account(company, VAT_OUTPUT_ACCOUNT)
	if not vat_account:
		frappe.throw(_("Account {0} not found for company {1}").format(VAT_OUTPUT_ACCOUNT, company))

	invoices = frappe.get_all(
		"Sales Invoice",
		filters={
			"company": company,
			"docstatus": ["in", [1, 2]],
			"posting_date": ["between", [start, end]],
			"is_opening": ["!=", "Yes"],  # opening balances are not sales of the month
		},
		fields=[
			"name",
			"posting_date",
			"docstatus",
			"customer",
			"customer_name",
			"tax_id",
			"customer_address",
			"base_net_total",
		],
		order_by="posting_date asc, name asc",
	)
	vat = _vat_by_invoice([inv.name for inv in invoices], vat_account)
	customers = _customers({inv.customer for inv in invoices})

	rows, net_total, vat_total = [], 0.0, 0.0
	for idx, inv in enumerate(invoices, start=1):
		customer = customers.get(inv.customer) or frappe._dict()
		cancelled = inv.docstatus == 2
		net_amount = 0.0 if cancelled else flt(inv.base_net_total, 2)
		vat_amount = 0.0 if cancelled else flt(vat.get(inv.name), 2)
		net_total += net_amount
		vat_total += vat_amount
		rows.append(
			{
				"idx": idx,
				"posting_date_be": be_date(inv.posting_date, "short"),
				"invoice_no": inv.name,
				"customer_name": (inv.customer_name or customer.customer_name or inv.customer)
				+ (CANCELLED_SUFFIX if cancelled else ""),
				"customer_tax_id": inv.tax_id or customer.tax_id or "",
				"customer_branch": branch_code(inv.customer_address, customer.th_branch_code)
				or HEAD_OFFICE_BRANCH,
				"net_amount": net_amount,
				"vat_amount": vat_amount,
			}
		)
	rows.append({"invoice_no": TOTAL_LABEL, "net_amount": flt(net_total, 2), "vat_amount": flt(vat_total, 2)})
	return rows


def _period(filters) -> tuple[date, date]:
	today = getdate(nowdate())
	month = cint(filters.get("month")) or today.month
	year = cint(filters.get("year")) or today.year
	if not 1 <= month <= 12:
		frappe.throw(_("Invalid month: {0}").format(filters.get("month")))
	start = date(year, month, 1)
	return start, getdate(get_last_day(start))


def _vat_by_invoice(names: list[str], vat_account: str) -> dict[str, float]:
	if not names:
		return {}
	tax = frappe.qb.DocType("Sales Taxes and Charges")
	rows = (
		frappe.qb.from_(tax)
		.select(tax.parent, Sum(tax.base_tax_amount_after_discount_amount))
		.where(
			(tax.parenttype == "Sales Invoice") & (tax.parent.isin(names)) & (tax.account_head == vat_account)
		)
		.groupby(tax.parent)
	).run()
	return {parent: flt(amount) for parent, amount in rows}


def _customers(names: set[str]) -> dict[str, frappe._dict]:
	if not names:
		return {}
	return {
		c.name: c
		for c in frappe.get_all(
			"Customer",
			filters={"name": ["in", list(names)]},
			fields=["name", "customer_name", "tax_id", "th_branch_code"],
		)
	}
