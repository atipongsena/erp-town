# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Document → event mappers for the sales slice (spec §3.3, §4.2): one module per doctype.

A mapper turns a submitted/cancelled document into the event's `data` and names the document's upstream
documents (for lineage and `data.against`). `data` never carries money amounts or personal names;
parties are referenced by code.
"""

from typing import Protocol

from frappe.utils import getdate

SUBMITTED = "submitted"
CANCELLED = "cancelled"


class Mapper(Protocol):
	doctype: str
	type_prefix: str  # e.g. "erp.sales_order"; the event type is f"{type_prefix}.{action}"
	source: str  # "erp/selling" | "erp/stock" | "erp/accounts"
	lists_against: bool  # `submitted` data carries `against` (the upstream document names)

	def applies(self, doc) -> bool:
		"""False for documents outside the slice (no event at all)."""
		...

	def data(self, doc, action: str) -> dict:
		"""Event `data` without `against` (added by the hook from the lineage)."""
		...

	def upstream(self, doc) -> list[tuple[str, str]]:
		"""(doctype, name) of the documents this one was made against, nearest first, in row order."""
		...


def status_of(doc, action: str) -> str:
	"""ERPNext `status` after the action, or "Submitted"/"Cancelled" when the doctype keeps none."""
	return doc.get("status") or ("Cancelled" if action == CANCELLED else "Submitted")


def return_fields(doc) -> dict:
	"""`is_return` / `return_against` of DN / SI `submitted` data (Ruling AF; always sent)."""
	is_return = bool(doc.get("is_return"))
	return {
		"is_return": is_return,
		"return_against": (doc.get("return_against") or None) if is_return else None,
	}


def iso_date(value) -> str | None:
	return getdate(value).isoformat() if value else None


def unique(pairs) -> list[tuple[str, str]]:
	"""Drop empty names and repeats, keeping the first appearance."""
	seen: set[tuple[str, str]] = set()
	result = []
	for doctype, name in pairs:
		if name and (doctype, name) not in seen:
			seen.add((doctype, name))
			result.append((doctype, name))
	return result


def _registry() -> dict[str, Mapper]:
	from erp_town.events.mappers.delivery_note import DeliveryNoteMapper
	from erp_town.events.mappers.payment_entry import PaymentEntryMapper
	from erp_town.events.mappers.sales_invoice import SalesInvoiceMapper
	from erp_town.events.mappers.sales_order import SalesOrderMapper

	mappers: list[Mapper] = [
		SalesOrderMapper(),
		DeliveryNoteMapper(),
		SalesInvoiceMapper(),
		PaymentEntryMapper(),
	]
	return {m.doctype: m for m in mappers}


REGISTRY: dict[str, Mapper] = _registry()
