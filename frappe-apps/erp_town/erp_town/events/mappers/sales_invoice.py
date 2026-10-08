# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

from erp_town.events.mappers import CANCELLED, iso_date, return_fields, status_of, unique


class SalesInvoiceMapper:
	doctype = "Sales Invoice"
	type_prefix = "erp.sales_invoice"
	source = "erp/accounts"
	lists_against = True

	def applies(self, doc) -> bool:
		return True

	def data(self, doc, action: str) -> dict:
		if action == CANCELLED:
			return {"status": status_of(doc, action)}
		return {
			"status": status_of(doc, action),
			"party": doc.customer,
			"due_date": iso_date(doc.get("due_date") or doc.get("posting_date")),
			**return_fields(doc),
		}

	def upstream(self, doc) -> list[tuple[str, str]]:
		"""Per item row the Delivery Note, then the Sales Order (nearest upstream first)."""
		pairs = []
		for row in doc.get("items") or []:
			pairs.append(("Delivery Note", row.get("delivery_note")))
			pairs.append(("Sales Order", row.get("sales_order")))
		return unique(pairs)
