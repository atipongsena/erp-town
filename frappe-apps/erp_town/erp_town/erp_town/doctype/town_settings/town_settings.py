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

_CACHE_KEY = "__town_settings__"


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
	"""Town Settings with defaults applied, as a fresh copy the caller may mutate.

	Cached in `frappe.db.value_cache["Town Settings"]`, so the cache is transaction-scoped: Frappe clears
	it on commit, rollback and savepoint rollback, and pops the doctype on single save and
	`frappe.db.set_single_value` (via `frappe.clear_document_cache`). Call `clear_town_settings_cache()`
	after writing `tabSingles` with raw SQL.
	"""
	cache = frappe.db.value_cache["Town Settings"]
	if _CACHE_KEY not in cache:
		cache[_CACHE_KEY] = _load_town_settings()
	return _copy(cache[_CACHE_KEY])


def clear_town_settings_cache() -> None:
	frappe.db.value_cache.get("Town Settings", {}).pop(_CACHE_KEY, None)


def _copy(settings: frappe._dict) -> frappe._dict:
	return frappe._dict(
		settings,
		approval_states=list(settings.approval_states),
		naming_rules=[dict(r) for r in settings.naming_rules],
	)


def _load_town_settings() -> frappe._dict:
	raw = frappe.db.get_singles_dict("Town Settings")
	hour = raw.get("overdue_job_hour")
	return frappe._dict(
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
