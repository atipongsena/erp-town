# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Test factories for the sales flow on `test.localhost` (never run against `town.localhost`).

`ensure_test_company()` builds the minimum ERPNext master data the sales flow needs — tree roots, UOM,
fiscal years, a company with ERPNext's standard chart, a price list — idempotently, so a blank test site
(setup wizard never run) works. Call it once from `setUpClass` and commit; the document factories then
work inside the test's own transaction.
"""

import json

import frappe
from frappe.utils import add_days, getdate, nowdate

TEST_COMPANY = "ERP Town Test Company"
TEST_ABBR = "ETT"
CURRENCY = "THB"
COUNTRY = "Thailand"
PRICE_LIST = "ERP Town Test Selling"
UOM = "Nos"
ITEM_GROUP = "ERP Town Test Items"
CUSTOMER_GROUP = "ERP Town Test Customers"
TERRITORY = "ERP Town Test Territory"
DEFAULT_RATE = 100
OPENING_QTY = 100
TEST_WAREHOUSE = f"Stores - {TEST_ABBR}"  # created by ERPNext with the company


def ensure_test_company() -> str:
	"""Create (once) everything the factories need; returns the company name. Caller commits.

	Refuses on any site without `allow_tests` (only `test.localhost`): the caller commits, and test data
	must never reach `town.localhost`.
	"""
	if not frappe.conf.get("allow_tests"):
		raise RuntimeError(
			f"ensure_test_company() refused on site {frappe.local.site}: allow_tests is not enabled"
		)
	_ensure_roots()
	_ensure_fiscal_years()
	if not frappe.db.exists("Company", TEST_COMPANY):
		frappe.get_doc(
			{
				"doctype": "Company",
				"company_name": TEST_COMPANY,
				"abbr": TEST_ABBR,
				"default_currency": CURRENCY,
				"country": COUNTRY,
				"create_chart_of_accounts_based_on": "Standard Template",
				"chart_of_accounts": "Standard",
			}
		).insert(ignore_permissions=True)
	if not frappe.db.exists("Price List", PRICE_LIST):
		frappe.get_doc(
			{
				"doctype": "Price List",
				"price_list_name": PRICE_LIST,
				"currency": CURRENCY,
				"selling": 1,
				"enabled": 1,
			}
		).insert(ignore_permissions=True)
	return TEST_COMPANY


def _ensure_roots() -> None:
	"""Tree roots (as ERPNext's setup wizard names them) plus one leaf each for the test masters."""
	for doctype, root, leaf, name_field in (
		("Item Group", "All Item Groups", ITEM_GROUP, "item_group_name"),
		("Customer Group", "All Customer Groups", CUSTOMER_GROUP, "customer_group_name"),
		("Territory", "All Territories", TERRITORY, "territory_name"),
	):
		parent_field = f"parent_{frappe.scrub(doctype)}"
		if not frappe.db.exists(doctype, root):
			frappe.get_doc({"doctype": doctype, name_field: root, "is_group": 1}).insert(
				ignore_permissions=True
			)
		if not frappe.db.exists(doctype, leaf):
			frappe.get_doc({"doctype": doctype, name_field: leaf, "is_group": 0, parent_field: root}).insert(
				ignore_permissions=True
			)
	if not frappe.db.exists("UOM", UOM):
		frappe.get_doc({"doctype": "UOM", "uom_name": UOM, "must_be_whole_number": 1}).insert(
			ignore_permissions=True
		)
	if not frappe.db.exists("Warehouse Type", "Transit"):
		frappe.get_doc({"doctype": "Warehouse Type", "name": "Transit"}).insert(ignore_permissions=True)


def _ensure_fiscal_years() -> None:
	"""Calendar fiscal years around today, unless some fiscal year already covers that year."""
	year = getdate(nowdate()).year
	for y in (year - 1, year, year + 1):
		start, end = f"{y}-01-01", f"{y}-12-31"
		covered = frappe.db.exists(
			"Fiscal Year", {"year_start_date": ["<=", start], "year_end_date": [">=", end]}
		)
		if not covered:
			frappe.get_doc(
				{
					"doctype": "Fiscal Year",
					"year": f"ERP Town Test FY {y}",
					"year_start_date": start,
					"year_end_date": end,
				}
			).insert(ignore_permissions=True)


def _code(prefix: str) -> str:
	return f"{prefix}-{frappe.generate_hash(length=8).upper()}"


def make_customer() -> str:
	"""A company-type customer; returns its code (the Customer name)."""
	return (
		frappe.get_doc(
			{
				"doctype": "Customer",
				"customer_name": _code("TOWN-CUST"),
				"customer_type": "Company",
				"customer_group": CUSTOMER_GROUP,
				"territory": TERRITORY,
			}
		)
		.insert(ignore_permissions=True)
		.name
	)


def make_item(reorder_level: float | None = None, opening_qty: float = OPENING_QTY) -> str:
	"""A stock item stocked in the test warehouse (`opening_qty`), optionally with a reorder level there."""
	item = {
		"doctype": "Item",
		"item_code": _code("TOWN-ITEM"),
		"item_group": ITEM_GROUP,
		"stock_uom": UOM,
		"is_stock_item": 1,
		"valuation_rate": DEFAULT_RATE,
		"standard_rate": DEFAULT_RATE,
		"item_defaults": [{"company": TEST_COMPANY, "default_warehouse": TEST_WAREHOUSE}],
	}
	if opening_qty:
		item["opening_stock"] = opening_qty
	if reorder_level is not None:
		item["reorder_levels"] = [
			{
				"warehouse": TEST_WAREHOUSE,
				"warehouse_reorder_level": reorder_level,
				"warehouse_reorder_qty": reorder_level or 1,
				"material_request_type": "Purchase",
			}
		]
	return frappe.get_doc(item).insert(ignore_permissions=True).name


def _item_rows(items: list, *, delivery_date: str | None = None) -> list[dict]:
	rows = []
	for entry in items:
		row = {"item_code": entry} if isinstance(entry, str) else dict(entry)
		row.setdefault("qty", 1)
		row.setdefault("rate", DEFAULT_RATE)
		row.setdefault("warehouse", TEST_WAREHOUSE)
		if delivery_date:
			row.setdefault("delivery_date", delivery_date)
		rows.append(row)
	return rows


def make_so(customer: str, items: list, *, submit: bool = True):
	"""Sales Order for `items` (item codes, or row dicts overriding qty/rate/warehouse)."""
	today = nowdate()
	delivery_date = add_days(today, 7)
	so = frappe.get_doc(
		{
			"doctype": "Sales Order",
			"company": TEST_COMPANY,
			"customer": customer,
			"order_type": "Sales",
			"transaction_date": today,
			"delivery_date": delivery_date,
			"currency": CURRENCY,
			"selling_price_list": PRICE_LIST,
			"items": _item_rows(items, delivery_date=delivery_date),
		}
	).insert(ignore_permissions=True)
	if submit:
		so.submit()
	return so


def make_dn_from_so(so, *more_sos, submit: bool = True):
	"""Delivery Note for the open qty of `so` (and of `more_sos`, merged in that order)."""
	from erpnext.selling.doctype.sales_order.sales_order import make_delivery_note

	dn = make_delivery_note(so.name)
	for other in more_sos:
		dn = make_delivery_note(other.name, target_doc=dn)
	dn.insert(ignore_permissions=True)
	if submit:
		dn.submit()
	return dn


def make_si_from_dn(dn, *, submit: bool = True):
	from erpnext.stock.doctype.delivery_note.delivery_note import make_sales_invoice

	si = make_sales_invoice(dn.name)
	si.insert(ignore_permissions=True)
	if submit:
		si.submit()
	return si


def make_si(customer: str, items: list, *, submit: bool = True, posting_date: str | None = None):
	"""A standalone Sales Invoice (no Sales Order / Delivery Note upstream, no stock update).

	Due on its posting date (`posting_date`, default today).
	"""
	posting_date = str(getdate(posting_date or nowdate()))
	si = frappe.get_doc(
		{
			"doctype": "Sales Invoice",
			"company": TEST_COMPANY,
			"customer": customer,
			"set_posting_time": 1,
			"posting_date": posting_date,
			"due_date": posting_date,
			"currency": CURRENCY,
			"selling_price_list": PRICE_LIST,
			"items": _item_rows(items),
		}
	).insert(ignore_permissions=True)
	if submit:
		si.submit()
	return si


def make_pe_for_si(si, *, submit: bool = True):
	"""Full payment of `si` into the company's default cash account."""
	from erpnext.accounts.doctype.payment_entry.payment_entry import get_payment_entry

	pe = get_payment_entry("Sales Invoice", si.name)
	pe.reference_no = pe.reference_no or si.name
	pe.reference_date = pe.reference_date or nowdate()
	pe.insert(ignore_permissions=True)
	if submit:
		pe.submit()
	return pe


def outbox_events_in_creation_order(correlation_id: str | None = None) -> list[dict]:
	"""Parsed payloads of outbox rows (optionally one correlation), oldest first."""
	filters = {"correlation_id": correlation_id} if correlation_id else {}
	rows = frappe.get_all(
		"Town Event Outbox",
		filters=filters,
		fields=["payload"],
		order_by="creation asc, event_id asc",
	)
	return [json.loads(r.payload) for r in rows]
