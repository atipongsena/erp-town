# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

import re

import frappe
from frappe.model.document import Document
from frappe.utils import cint

DEFAULTS = {
	"stream_name": "town:events",
	"stream_retention_days": 7,
	"outbox_retention_days": 400,
	"max_attempts": 10,
	"overdue_job_hour": 8,
	"approval_states": ["รออนุมัติ"],
}

_CACHE_KEY = "erp_town_settings"


class TownSettings(Document):
	# begin: auto-generated types
	# This code is auto-generated. Do not modify anything in this block.

	from typing import TYPE_CHECKING

	if TYPE_CHECKING:
		from frappe.types import DF

		from erp_town.erp_town.doctype.town_naming_rule.town_naming_rule import TownNamingRule

		approval_states: DF.SmallText | None
		max_attempts: DF.Int
		naming_rules: DF.Table[TownNamingRule]
		outbox_retention_days: DF.Int
		overdue_job_hour: DF.Int
		redis_town_url: DF.Data | None
		stream_name: DF.Data | None
		stream_retention_days: DF.Int
	# end: auto-generated types

	def on_update(self):
		clear_town_settings_cache()


def parse_approval_states(value: str | None) -> list[str]:
	"""Split on commas/newlines, trim, drop empties."""
	return [s.strip() for s in re.split(r"[,\n]", value or "") if s.strip()]


def get_town_settings() -> frappe._dict:
	"""Town Settings with defaults applied; cached for the current request on `frappe.local`."""
	cached = getattr(frappe.local, _CACHE_KEY, None)
	if cached is not None:
		return cached

	raw = frappe.db.get_singles_dict("Town Settings")
	hour = raw.get("overdue_job_hour")
	settings = frappe._dict(
		redis_town_url=raw.get("redis_town_url") or frappe.conf.get("town_redis_url"),
		stream_name=raw.get("stream_name") or DEFAULTS["stream_name"],
		stream_retention_days=cint(raw.get("stream_retention_days")) or DEFAULTS["stream_retention_days"],
		outbox_retention_days=cint(raw.get("outbox_retention_days")) or DEFAULTS["outbox_retention_days"],
		max_attempts=cint(raw.get("max_attempts")) or DEFAULTS["max_attempts"],
		# 0 (midnight) is a valid hour, so only fall back when the value was never stored
		overdue_job_hour=DEFAULTS["overdue_job_hour"] if hour in (None, "") else cint(hour),
		approval_states=parse_approval_states(raw.get("approval_states"))
		or list(DEFAULTS["approval_states"]),
		naming_rules=[
			{"document_type": r.document_type, "prefix": r.prefix}
			for r in frappe.get_all(
				"Town Naming Rule",
				filters={"parenttype": "Town Settings", "parentfield": "naming_rules"},
				fields=["document_type", "prefix"],
				order_by="idx asc",
			)
		],
	)
	setattr(frappe.local, _CACHE_KEY, settings)
	return settings


def clear_town_settings_cache() -> None:
	if hasattr(frappe.local, _CACHE_KEY):
		delattr(frappe.local, _CACHE_KEY)
