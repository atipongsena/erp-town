# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""`after_install` hook: Thai custom fields, Buddhist-Era numbering and the default tax-invoice print format
(also re-applied after every migrate)."""

from erp_town.thai.setup import ensure_custom_fields, ensure_default_print_formats, ensure_naming


def after_install() -> None:
	ensure_custom_fields()
	ensure_naming()
	ensure_default_print_formats()
