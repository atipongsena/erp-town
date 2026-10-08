# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

from erp_town.events.mappers import CANCELLED, return_fields, status_of, unique


class DeliveryNoteMapper:
	doctype = "Delivery Note"
	type_prefix = "erp.delivery_note"
	source = "erp/stock"
	lists_against = True

	def applies(self, doc) -> bool:
		return True

	def data(self, doc, action: str) -> dict:
		if action == CANCELLED:
			return {"status": status_of(doc, action)}
		items = doc.get("items") or []
		warehouse = (items[0].get("warehouse") if items else None) or doc.get("set_warehouse")
		return {
			"status": status_of(doc, action),
			"party": doc.customer,
			"item_count": len(items),
			"warehouse": warehouse,
			**return_fields(doc),
		}

	def upstream(self, doc) -> list[tuple[str, str]]:
		return unique(("Sales Order", row.get("against_sales_order")) for row in doc.get("items") or [])
