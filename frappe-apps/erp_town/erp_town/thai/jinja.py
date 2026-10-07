"""Jinja filters registered via hooks.py ``jinja["filters"]`` (filter name = function name).

Thin wrappers: empty values (None / "") render as "" so print formats with blank fields don't fail.
"""

from erp_town.thai.bahttext import bahttext as _bahttext
from erp_town.thai.dates import be_date as _be_date


def be_date(value, fmt="long"):
	if value is None or value == "":
		return ""
	return _be_date(value, fmt)


def bahttext(amount):
	if amount is None or amount == "":
		return ""
	return _bahttext(amount)
