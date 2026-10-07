# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

from erp_town.events.mappers import CANCELLED, status_of, unique


class PaymentEntryMapper:
	doctype = "Payment Entry"
	type_prefix = "erp.payment_entry"
	source = "erp/accounts"
	lists_against = True

	def applies(self, doc) -> bool:
		"""Customer payments only; supplier/employee payments and internal transfers are not in slice 1."""
		return doc.get("party_type") == "Customer"

	def data(self, doc, action: str) -> dict:
		if action == CANCELLED:
			return {"status": status_of(doc, action)}
		return {"party": doc.party, "mode_of_payment": doc.get("mode_of_payment") or None}

	def upstream(self, doc) -> list[tuple[str, str]]:
		return unique(
			("Sales Invoice", row.get("reference_name"))
			for row in doc.get("references") or []
			if row.get("reference_doctype") == "Sales Invoice"
		)
