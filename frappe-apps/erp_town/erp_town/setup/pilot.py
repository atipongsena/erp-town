# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

r"""Pilot site bootstrap (spec §6.3, infra/docker/pilot/RESTORE.md pre-pilot checklist).

`bootstrap(company_name, abbr, tax_id)` prepares a fresh pilot site for the real Thai company, doing what
ERPNext's setup wizard would do: System Settings (Thai language, Asia/Bangkok, Thailand, THB), base masters
(tree roots, UOM, fiscal years), the company with the draft Thai chart, default accounts and VAT templates,
and marks setup complete. No demo data, no users, no agents (those are `erp_town.setup.seed`, dev only).

Idempotent (a second run adds nothing). Does not commit itself: run it through `bench execute`, which
commits at the end:

    pnpm erp:pilot:bench -- --site <site> execute erp_town.setup.pilot.bootstrap \
      --kwargs '{"company_name": "บริษัท ... จำกัด", "abbr": "ABC-TH", "tax_id": "0105..."}'
"""

import frappe
from frappe import _

from erp_town.setup.masters import ensure_base_masters, ensure_system_settings, mark_setup_complete
from erp_town.thai.setup import create_thai_company

LANGUAGE = "th"
TIME_ZONE = "Asia/Bangkok"
COUNTRY = "Thailand"
CURRENCY = "THB"


def bootstrap(company_name: str, abbr: str, tax_id: str) -> str:
	"""Thai settings + the pilot company (see module docstring); returns the company name."""
	company_name, abbr, tax_id = (str(v or "").strip() for v in (company_name, abbr, tax_id))
	if not (company_name and abbr and tax_id):
		frappe.throw(_("company_name, abbr and tax_id are required"))
	ensure_system_settings(language=LANGUAGE, time_zone=TIME_ZONE, country=COUNTRY, currency=CURRENCY)
	ensure_base_masters()
	create_thai_company(company_name, abbr, tax_id)
	_set_global_defaults_if_empty(company_name)
	mark_setup_complete()
	return company_name


def _set_global_defaults_if_empty(company_name: str) -> None:
	for field, value in (
		("default_company", company_name),
		("default_currency", CURRENCY),
		("country", COUNTRY),
	):
		if not frappe.db.get_single_value("Global Defaults", field):
			frappe.db.set_single_value("Global Defaults", field, value)
