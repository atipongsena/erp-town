# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Helpers for the Thai print formats (spec §5.3), registered in hooks.py ``jinja``.

Filters: ``th_branch_label``, ``th_money``, ``th_qty``. Methods: ``th_parties``, ``th_totals``,
``th_reference_date``.
"""

import frappe
from frappe.contacts.doctype.address.address import get_default_address, render_address
from frappe.utils import cint, flt

from erp_town.thai.setup import HEAD_OFFICE_BRANCH, VAT_OUTPUT_ACCOUNT, company_account

HEAD_OFFICE_LABEL = "สำนักงานใหญ่"
VAT_LABEL = "ภาษีมูลค่าเพิ่ม"


def th_branch_label(code) -> str:
	"""`00000` (or no code) -> `สำนักงานใหญ่`; `00001` -> `สาขาที่ 00001` (Revenue Code §86/4 branch line)."""
	code = (code or "").strip()
	if not code or code == HEAD_OFFICE_BRANCH:
		return HEAD_OFFICE_LABEL
	return f"สาขาที่ {code}"


def th_money(value) -> str:
	"""`2501` -> `2,501.00` (fixed 2 decimals, independent of the site's number format)."""
	return f"{flt(value):,.2f}"


def th_qty(value) -> str:
	"""`2` -> `2`, `1250.5` -> `1,250.5` (thousands separator, up to 3 decimals, no trailing zeros)."""
	return f"{flt(value):,.3f}".rstrip("0").rstrip(".")


def th_parties(doc) -> dict:
	"""Seller (company) and buyer (customer) blocks: name, address (HTML), tax ID and branch label.

	The buyer's tax ID is the document's own (`tax_id`, fetched when the document was made), else the
	customer's. Branch: the company's / customer's `th_branch_code`, overridden by the code of the address
	the document links (`company_address` / `customer_address`) only when that code is a branch (not
	`00000`): new addresses default to `00000` and ERPNext links the party's address automatically.
	"""
	company = (
		frappe.db.get_value(
			"Company", doc.company, ["company_name", "tax_id", "th_branch_code"], as_dict=True
		)
		or frappe._dict()
	)
	company_address = doc.get("company_address")
	seller = frappe._dict(
		name=company.company_name or doc.company,
		tax_id=company.tax_id or "",
		address=doc.get("company_address_display")
		or _address_html(company_address or get_default_address("Company", doc.company)),
		branch=th_branch_label(branch_code(company_address, company.th_branch_code)),
	)

	customer_name = doc.get("customer") or (doc.get("party") if doc.get("party_type") == "Customer" else None)
	customer = (
		frappe.db.get_value(
			"Customer", customer_name, ["customer_name", "tax_id", "th_branch_code"], as_dict=True
		)
		if customer_name
		else None
	) or frappe._dict()
	customer_address = doc.get("customer_address")
	buyer = frappe._dict(
		name=doc.get("customer_name") or doc.get("party_name") or customer.customer_name or "",
		tax_id=doc.get("tax_id") or customer.tax_id or "",
		address=doc.get("address_display")
		or _address_html(
			customer_address or (get_default_address("Customer", customer_name) if customer_name else None)
		),
		branch=th_branch_label(branch_code(customer_address, customer.th_branch_code)),
	)
	return frappe._dict(seller=seller, buyer=buyer)


def th_totals(doc) -> frappe._dict:
	"""Totals block of a selling document.

	- VAT: only tax rows on the company's output-VAT account (`ภาษีขาย`); `vat_label` carries their rate
	  (`ภาษีมูลค่าเพิ่ม 7%`), `vat_amount` their sum. Other charge rows are listed separately (`charges`).
	- `inclusive`: the VAT rows are included in the print rate (template `ภาษีขาย 7% (ราคารวม VAT)`), so
	  item rows show `net_rate` / `net_amount` and the block starts at the net total.
	- `rounding_adjustment` (else None) when ERPNext rounded the grand total; `payable` is what the customer
	  owes (`rounded_total`, else `grand_total`) and is the amount written in words.
	"""
	vat_account = company_account(doc.company, VAT_OUTPUT_ACCOUNT)
	taxes = doc.get("taxes") or []
	vat_rows = [row for row in taxes if vat_account and row.account_head == vat_account]
	charges = [
		frappe._dict(description=row.description or row.account_head, amount=_row_amount(row))
		for row in taxes
		if row not in vat_rows and _row_amount(row)
	]
	rate = next((flt(row.rate) for row in vat_rows if flt(row.rate)), 0)
	grand_total = flt(doc.get("grand_total"), 2)
	rounded_total = flt(doc.get("rounded_total"), 2)
	rounded = bool(rounded_total) and rounded_total != grand_total
	return frappe._dict(
		vat_label=f"{VAT_LABEL} {rate:g}%",
		vat_amount=sum(_row_amount(row) for row in vat_rows),
		charges=charges,
		inclusive=any(cint(row.included_in_print_rate) for row in vat_rows),
		grand_total=grand_total,
		rounding_adjustment=flt(doc.get("rounding_adjustment"), 2) if rounded else None,
		payable=rounded_total or grand_total,
	)


def _row_amount(row) -> float:
	return flt(row.tax_amount_after_discount_amount)


def th_reference_date(row):
	"""Document date of a Payment Entry reference row (`posting_date`, or `transaction_date` for orders)."""
	meta = frappe.get_meta(row.reference_doctype)
	field = "posting_date" if meta.has_field("posting_date") else "transaction_date"
	if not meta.has_field(field):
		return None
	return frappe.db.get_value(row.reference_doctype, row.reference_name, field)


def branch_code(address: str | None, party_code: str | None) -> str | None:
	"""A party's branch code on a document: the linked address's `th_branch_code` when it is a branch (not
	`00000`), else the party's own `th_branch_code`. Shared by the print formats and the sales VAT report."""
	address_code = (
		(frappe.db.get_value("Address", address, "th_branch_code") or "").strip() if address else ""
	)
	if address_code and address_code != HEAD_OFFICE_BRANCH:
		return address_code
	return party_code


def _address_html(address: str | None) -> str:
	return (render_address(address, check_permissions=False) or "") if address else ""
