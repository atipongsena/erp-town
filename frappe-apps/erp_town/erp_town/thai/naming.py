# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Buddhist-Era document numbering (spec §5.2): `<prefix><BE yy><mm>-<####>`, e.g. `IV6910-0001`.

`BEYY` / `BEMM` are naming-series variables registered in hooks.py (`naming_series_variables`), so a
series `IV.BEYY.BEMM.-.####` keeps Frappe's counter per `IV6910-` key: numbering restarts every month per
prefix. Year and month come from the document's own date, so back-dated documents land in their month.
"""

from datetime import date

import frappe
from frappe.utils import getdate, nowdate

from erp_town.thai.dates import BE_OFFSET


def be_yy(doc, part: str) -> str:
	"""Last two digits of the Buddhist-Era year of the document date (2026 -> `69`)."""
	return f"{(naming_date(doc).year + BE_OFFSET) % 100:02d}"


def be_mm(doc, part: str) -> str:
	"""Two-digit month of the document date."""
	return f"{naming_date(doc).month:02d}"


def naming_date(doc) -> date:
	"""`posting_date`, else `transaction_date`, else today.

	A document with an unticked `set_posting_time` (SI, DN, ...) is numbered by today: ERPNext's
	`validate_posting_time` resets its posting date to today right after naming, unless it is imported.
	`doc` may be None (Frappe evaluates series prefixes without a document, e.g. naming settings preview).
	"""
	if doc is not None:
		if _posting_date_reset_to_today(doc):
			return getdate(nowdate())
		value = _get(doc, "posting_date") or _get(doc, "transaction_date")
		if value:
			return getdate(value)
	return getdate(nowdate())


def _get(doc, fieldname: str):
	return doc.get(fieldname) if hasattr(doc, "get") else getattr(doc, fieldname, None)


def _posting_date_reset_to_today(doc) -> bool:
	meta = getattr(doc, "meta", None)
	if meta is None or not meta.has_field("set_posting_time"):
		return False
	return not doc.get("set_posting_time") and not doc.get("amended_from") and not frappe.flags.in_import
