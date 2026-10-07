# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""`after_install` hook: Thai custom fields and Buddhist-Era numbering (also re-applied after every migrate)."""

from erp_town.thai.setup import ensure_custom_fields, ensure_naming


def after_install() -> None:
	ensure_custom_fields()
	ensure_naming()
