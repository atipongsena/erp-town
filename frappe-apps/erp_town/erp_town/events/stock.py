# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""`erp.stock.below_reorder` (spec §3.3, §4.2): checked when a Delivery Note is submitted.

Runs after `doc_hooks.on_doc_event` in the DN `on_submit` hook list (Ruling C), when the DN's stock ledger
entries are posted and `Bin.actual_qty` already includes them. Per (item, warehouse) of the DN — quantities
in stock UOM summed over its rows — with an `Item Reorder` row for that warehouse: `after = Bin.actual_qty`,
`before = after + delivered`; an event is emitted only when `before >= level > after` (crossed down).
Subject is the Item; company, correlation and causation come from the DN and its latest (`submitted`) event.
Events are emitted in (item_code, warehouse) order, so concurrent DNs take the Item seq locks in the same
order. Return DNs add stock and are skipped. Town-layer failures never block the submit.
"""

from collections import defaultdict

import frappe
from frappe.utils import flt

from erp_town.events.actor import resolve_actor
from erp_town.events.emit import emit_raw
from erp_town.events.lineage import causation_of, latest_event
from erp_town.events.system import report_build_failure

SOURCE = "erp/stock"
BELOW_REORDER = "erp.stock.below_reorder"


def fill_ratio(actual_qty: float, reorder_level: float) -> float:
	"""`actual_qty / reorder_level` rounded to 2 decimals, floored at 0 (Ruling B)."""
	ratio = round(flt(actual_qty) / flt(reorder_level), 2)
	return ratio if ratio > 0 else 0.0


def on_dn_submit(doc, method: str | None = None) -> None:
	if doc.doctype != "Delivery Note" or doc.get("is_return"):
		return
	try:
		crossings = _crossings(doc)
		if not crossings:
			return
		own = latest_event(doc.doctype, doc.name)
		actor = resolve_actor(default_kind="system")
	except Exception as e:  # noqa: BLE001 -- town-layer failures must never block the document
		report_build_failure(
			BELOW_REORDER, e, subject_doctype=doc.doctype, subject_name=doc.name, company=doc.get("company")
		)
		return

	for item_code, warehouse, ratio in crossings:
		emit_raw(
			BELOW_REORDER,
			source=SOURCE,
			subject_doctype="Item",
			subject_name=item_code,
			company=doc.get("company"),
			data={"warehouse": warehouse, "fill_ratio": ratio},
			actor=actor,
			correlation_id=own["correlation_id"] if own else doc.name,
			causation_id=causation_of(own),
		)


def _crossings(doc) -> list[tuple[str, str, float]]:
	"""(item_code, warehouse, fill_ratio) for each pair this DN took from `>= level` to `< level`, sorted by
	(item_code, warehouse): the events lock their Item seq rows in one consistent order."""
	delivered: dict[tuple[str, str], float] = defaultdict(float)
	for row in doc.get("items") or []:
		if row.get("item_code") and row.get("warehouse"):
			delivered[(row.item_code, row.warehouse)] += flt(row.get("stock_qty"))
	if not delivered:
		return []

	levels = _reorder_levels(delivered.keys())
	crossings = []
	for (item_code, warehouse), qty in delivered.items():
		level = flt(levels.get((item_code, warehouse)))
		if level <= 0:
			continue
		after = flt(
			frappe.db.get_value("Bin", {"item_code": item_code, "warehouse": warehouse}, "actual_qty")
		)
		before = after + qty
		if before >= level > after:
			crossings.append((item_code, warehouse, fill_ratio(after, level)))
	return sorted(crossings)


def _reorder_levels(pairs) -> dict[tuple[str, str], float]:
	"""Reorder level per (item_code, warehouse) from the Items' `Item Reorder` rows (first row wins)."""
	pairs = set(pairs)
	rows = frappe.get_all(
		"Item Reorder",
		filters={
			"parenttype": "Item",
			"parent": ["in", sorted({item for item, _ in pairs})],
			"warehouse": ["in", sorted({warehouse for _, warehouse in pairs})],
		},
		fields=["parent", "warehouse", "warehouse_reorder_level"],
		order_by="idx asc",
	)
	levels: dict[tuple[str, str], float] = {}
	for row in rows:
		key = (row.parent, row.warehouse)
		if key in pairs:
			levels.setdefault(key, flt(row.warehouse_reorder_level))
	return levels
