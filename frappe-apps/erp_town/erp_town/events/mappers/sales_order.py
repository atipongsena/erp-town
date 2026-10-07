# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

from erp_town.events.mappers import CANCELLED, iso_date, status_of


class SalesOrderMapper:
	doctype = "Sales Order"
	type_prefix = "erp.sales_order"
	source = "erp/selling"
	lists_against = False

	def applies(self, doc) -> bool:
		return True

	def data(self, doc, action: str) -> dict:
		if action == CANCELLED:
			return {"status": status_of(doc, action)}
		items = doc.get("items") or []
		delivery_date = doc.get("delivery_date") or min(
			(row.get("delivery_date") for row in items if row.get("delivery_date")), default=None
		)
		return {
			"status": status_of(doc, action),
			"party": doc.customer,
			"item_count": len(items),
			"delivery_date": iso_date(delivery_date),
		}

	def upstream(self, doc) -> list[tuple[str, str]]:
		return []  # a Sales Order is the root of the sales flow in slice 1
