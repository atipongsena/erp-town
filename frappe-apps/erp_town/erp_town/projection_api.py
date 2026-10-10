# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Bounded read-only world metadata, never ERP documents or amounts.

The dedicated caller also needs normal read permissions for the queried parent
doctypes. Child rows and Town Subject Seq are read only for those already
permission-filtered parents. No permission is granted by this module.
"""

import frappe
from frappe.utils import getdate, nowdate

from erp_town.events.stock import fill_ratio

PAGE_SIZE = 200
MAX_ROWS = 100_000
PROJECTION_ROLE = "Town Projection Reader"


def _pages(doctype, filters, fields):
	start = 0
	while True:
		rows = frappe.get_list(
			doctype, filters=filters, fields=fields, order_by="name asc",
			offset=start, limit=PAGE_SIZE,
		)
		if start + len(rows) > MAX_ROWS:
			frappe.throw("World projection exceeds its bounded read size", frappe.ValidationError)
		yield from rows
		if len(rows) < PAGE_SIZE:
			return
		start += PAGE_SIZE


@frappe.whitelist(methods=["GET"])
def read_projection(company: str) -> dict:
	if PROJECTION_ROLE not in frappe.get_roles(frappe.session.user):
		frappe.throw("Dedicated world projection role required", frappe.PermissionError)
	companies = frappe.get_list("Company", filters={"abbr": company}, fields=["name"], limit=1)
	if not companies:
		frappe.throw("Company is not readable", frappe.PermissionError)
	# The first Company read establishes a consistent snapshot. Facts and their
	# sequence fences must see the same committed ERP transaction throughout.
	isolation = frappe.db.sql("SELECT @@tx_isolation")[0][0]
	if isolation not in ("REPEATABLE-READ", "SERIALIZABLE"):
		frappe.throw("World projection requires consistent read isolation", frappe.ValidationError)
	company_name = companies[0].name
	facts = []
	refs = set()
	invoice_names = []
	for doctype in ("Sales Order", "Delivery Note", "Sales Invoice"):
		fields = ["name", "docstatus", "status"]
		if doctype == "Delivery Note":
			fields.append("per_billed")
		if doctype == "Sales Invoice":
			fields.extend(["due_date", "is_return"])
		for row in _pages(doctype, {"company": company_name}, fields):
			refs.add((doctype, row.name))
			if row.docstatus != 1:
				continue
			if doctype == "Sales Invoice" and not row.is_return:
				invoice_names.append(row.name)
			if doctype == "Sales Order" and row.status not in ("Closed", "Completed", "Cancelled"):
				facts.append({"doctype": doctype, "name": row.name, "kind": "document", "status": row.status})
			if doctype == "Delivery Note" and row.per_billed < 100 and row.status not in ("Closed", "Cancelled"):
				facts.append({"doctype": doctype, "name": row.name, "kind": "document", "status": row.status})
	# Filter balances inside the database; the result contains no balance fields.
	today = getdate(nowdate())
	for start in range(0, len(invoice_names), PAGE_SIZE):
		names = invoice_names[start:start + PAGE_SIZE]
		placeholders = ",".join(["%s"] * len(names))
		# Financial predicates apply only to already-readable invoice names. SQL
		# selects no amounts, so this endpoint does not require amount-field read
		# privileges or disclose those fields to its dedicated caller.
		rows = frappe.db.sql(
			f"""SELECT name, due_date FROM `tabSales Invoice`
			WHERE company=%s AND name IN ({placeholders}) AND docstatus=1
			AND is_return=0 AND outstanding_amount>0 AND due_date<%s""",
			(company_name, *names, today), as_dict=True,
		)
		for row in rows:
			due = getdate(row.due_date)
			facts.append({"doctype": "Sales Invoice", "name": row.name, "kind": "overdue",
				"due_date": due.isoformat(), "days_overdue": (today - due).days})
	_stock(company_name, facts, refs)
	sequences = _sequences(refs)
	return {"company": company, "facts": sorted(facts, key=lambda row: (
		row["doctype"], row["name"], row["kind"], row.get("scope", ""))), "sequences": sequences}


def _stock(company_name, facts, refs):
	warehouses = [row.name for row in _pages("Warehouse", {"company": company_name}, ["name"])]
	items = [row.name for row in _pages("Item", {}, ["name"])]
	if not warehouses or not items:
		return
	levels = {}
	for start in range(0, len(items), PAGE_SIZE):
		allowed_items = items[start:start + PAGE_SIZE]
		# Child-table access is bounded by readable Item and Warehouse parents.
		rows = frappe.get_all("Item Reorder", filters={"parenttype": "Item", "parent": ["in", allowed_items],
			"warehouse": ["in", warehouses]}, fields=["parent", "warehouse", "warehouse_reorder_level"],
			order_by="idx asc", limit=MAX_ROWS + 1)
		if len(rows) > MAX_ROWS:
			frappe.throw("World stock projection exceeds its bounded read size", frappe.ValidationError)
		for row in rows:
			levels.setdefault((row.parent, row.warehouse), row.warehouse_reorder_level)
		quantities = {(row.item_code, row.warehouse): row.actual_qty for row in _pages("Bin", {
			"item_code": ["in", allowed_items], "warehouse": ["in", warehouses],
		}, ["name", "item_code", "warehouse", "actual_qty"])}
		for item in allowed_items:
			refs.add(("Item", item))
		for (item, warehouse), level in levels.items():
			if item not in allowed_items:
				continue
			quantity = quantities.get((item, warehouse), 0)
			if level > 0 and quantity < level:
				facts.append({"doctype": "Item", "name": item, "kind": "stock", "scope": warehouse,
					"fill_ratio": fill_ratio(quantity, level)})


def _sequences(refs):
	result = {ref: 0 for ref in refs}
	for doctype in sorted({doctype for doctype, _ in refs}):
		names = sorted(name for dt, name in refs if dt == doctype)
		for start in range(0, len(names), PAGE_SIZE):
			# Sequence rows can expose only already permission-filtered subjects.
			for row in frappe.get_all("Town Subject Seq", filters={"subject_doctype": doctype,
				"subject_name": ["in", names[start:start + PAGE_SIZE]]},
				fields=["subject_name", "last_seq"], limit=PAGE_SIZE):
				result[(doctype, row.subject_name)] = int(row.last_seq)
	return [{"doctype": dt, "name": name, "seq": seq} for (dt, name), seq in sorted(result.items())]
