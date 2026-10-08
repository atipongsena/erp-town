# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Base master data ERPNext's setup wizard would create, for sites where the wizard never ran.

Idempotent: on a site set up through the wizard everything already exists and nothing is written.
"""

from collections.abc import Iterable

import frappe
from frappe.utils import getdate, nowdate

TREE_ROOTS = (
	("Item Group", "All Item Groups", "item_group_name"),
	("Customer Group", "All Customer Groups", "customer_group_name"),
	("Supplier Group", "All Supplier Groups", "supplier_group_name"),
	("Territory", "All Territories", "territory_name"),
)
DEFAULT_UOM = "Nos"


def ensure_base_masters() -> None:
	"""Tree roots, the `Nos` UOM, the `Transit` warehouse type and calendar fiscal years around today."""
	ensure_tree_roots()
	ensure_fiscal_years()


def ensure_tree_roots() -> None:
	for doctype, root, name_field in TREE_ROOTS:
		if not frappe.db.exists(doctype, root):
			frappe.get_doc({"doctype": doctype, name_field: root, "is_group": 1}).insert(
				ignore_permissions=True
			)
	if not frappe.db.exists("UOM", DEFAULT_UOM):
		frappe.get_doc({"doctype": "UOM", "uom_name": DEFAULT_UOM, "must_be_whole_number": 1}).insert(
			ignore_permissions=True
		)
	if not frappe.db.exists("Warehouse Type", "Transit"):  # Company creates a `Goods In Transit` warehouse
		frappe.get_doc({"doctype": "Warehouse Type", "name": "Transit"}).insert(ignore_permissions=True)


def ensure_fiscal_years(name_format: str = "{year}", years: Iterable[int] | None = None) -> None:
	"""Calendar fiscal years for `years` (default: last, this and next year), unless one already covers
	that year."""
	if years is None:
		year = getdate(nowdate()).year
		years = (year - 1, year, year + 1)
	for y in years:
		start, end = f"{y}-01-01", f"{y}-12-31"
		covered = frappe.db.exists(
			"Fiscal Year", {"year_start_date": ["<=", start], "year_end_date": [">=", end]}
		)
		if not covered:
			frappe.get_doc(
				{
					"doctype": "Fiscal Year",
					"year": name_format.format(year=y),
					"year_start_date": start,
					"year_end_date": end,
				}
			).insert(ignore_permissions=True)
