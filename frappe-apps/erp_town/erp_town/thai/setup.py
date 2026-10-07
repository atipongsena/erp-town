# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Thai localization setup (spec §5.1, §5.2, §5.3).

- `ensure_custom_fields` / `ensure_naming` / `ensure_default_print_formats` run after every migrate
  (hooks.py) and on install; `ensure_naming` also runs whenever Town Settings is saved.
- `validate_tax_id` is the Company / Customer `validate` hook.
- `create_thai_company` builds a Thai company: draft TFRS-for-NPAEs chart, default accounts, VAT templates.
"""

import json

import frappe
from frappe import _
from frappe.custom.doctype.custom_field.custom_field import create_custom_fields
from frappe.custom.doctype.property_setter.property_setter import make_property_setter
from frappe.model.naming import NamingSeries

from erp_town.erp_town.doctype.town_settings.town_settings import get_town_settings
from erp_town.setup.masters import ensure_base_masters
from erp_town.thai.taxid import is_valid_tax_id

# --- custom fields -------------------------------------------------------------------------------------

HEAD_OFFICE_BRANCH = "00000"
BRANCH_CODE_INSERT_AFTER = {"Company": "tax_id", "Customer": "tax_id", "Address": "address_type"}


def ensure_custom_fields() -> None:
	"""`th_branch_code` (5-character branch code, `00000` = head office) on Company, Customer, Address."""
	create_custom_fields(
		{
			doctype: [
				{
					"fieldname": "th_branch_code",
					"fieldtype": "Data",
					"label": "รหัสสาขา",
					"length": 5,
					"default": HEAD_OFFICE_BRANCH,
					"insert_after": insert_after,
					"description": "00000 = สำนักงานใหญ่",
				}
			]
			for doctype, insert_after in BRANCH_CODE_INSERT_AFTER.items()
		},
		update=True,
	)


# --- tax ID --------------------------------------------------------------------------------------------


def validate_tax_id(doc, method=None) -> None:
	"""Block saving a Company / Customer whose `tax_id` is set but fails the 13-digit mod-11 check.

	An empty tax ID is allowed (individual customers who do not ask for a full tax invoice).
	"""
	if doc.get("tax_id"):
		_check_tax_id(doc.tax_id)


def _check_tax_id(tax_id: str) -> None:
	if not is_valid_tax_id(tax_id):
		frappe.throw(_("เลขประจำตัวผู้เสียภาษีไม่ถูกต้อง: {0}").format(tax_id))


# --- Buddhist-Era numbering ----------------------------------------------------------------------------

DEFAULT_PREFIXES = {
	"Sales Order": "SO",
	"Delivery Note": "DN",
	"Sales Invoice": "IV",
	"Payment Entry": "RC",
}
_PREFIX_FORBIDDEN = frozenset(".#{} ")


def naming_series_for(prefix: str) -> str:
	"""`IV` -> `IV.BEYY.BEMM.-.####` (`BEYY`/`BEMM` are resolved by `erp_town.thai.naming`)."""
	return f"{prefix}.BEYY.BEMM.-.####"


def naming_prefixes() -> dict[str, str]:
	"""Doctype -> prefix: the defaults, overridden by Town Settings `naming_rules` rows."""
	prefixes = dict(DEFAULT_PREFIXES)
	for rule in get_town_settings().naming_rules:
		if rule["document_type"] and rule["prefix"]:
			prefixes[rule["document_type"]] = rule["prefix"].strip()
	return prefixes


def ensure_naming() -> None:
	"""Point each doctype's `naming_series` options and default at its BE series via Property Setters."""
	for doctype, prefix in naming_prefixes().items():
		if not frappe.get_meta(doctype).has_field("naming_series"):
			frappe.throw(_("{0} does not use a naming series").format(_(doctype)))
		if not prefix or set(prefix) & _PREFIX_FORBIDDEN:
			frappe.throw(_("Invalid document number prefix for {0}: {1}").format(_(doctype), prefix))
		series = naming_series_for(prefix)
		NamingSeries(series).validate()
		changed = False
		for prop in ("options", "default"):
			if _naming_property(doctype, prop) != series:
				make_property_setter(
					doctype, "naming_series", prop, series, "Text", validate_fields_for_doctype=False
				)
				changed = True
		if changed:
			frappe.clear_cache(doctype=doctype)
			# the new-document template (with the old default) is cached per process, not per doctype cache
			frappe.local.new_doc_templates.pop(doctype, None)


def _naming_property(doctype: str, prop: str) -> str | None:
	return frappe.db.get_value(
		"Property Setter",
		{"doc_type": doctype, "field_name": "naming_series", "property": prop},
		"value",
	)


# --- print formats -------------------------------------------------------------------------------------

DEFAULT_PRINT_FORMATS = {"Sales Invoice": "ใบกำกับภาษี/ใบแจ้งหนี้"}
# defaults ERPNext's installer sets (erpnext.setup.install.set_default_print_formats); replaced by ours,
# while any other default someone chose in Customize Form is kept
REPLACEABLE_PRINT_FORMATS = {
	"Sales Invoice": {"Sales Invoice Modern with Images", "Sales Invoice with Item Image", "Standard"},
}


def ensure_default_print_formats() -> None:
	"""Make the Thai tax invoice the Sales Invoice default print format (DocType Property Setter)."""
	for doctype, print_format in DEFAULT_PRINT_FORMATS.items():
		if not frappe.db.exists("Print Format", print_format):
			continue  # not synced yet (the standard formats are imported before after_migrate runs)
		current = frappe.get_meta(doctype).default_print_format
		if current == print_format:
			continue
		chosen_by_someone = current and current not in REPLACEABLE_PRINT_FORMATS.get(doctype, set())
		if chosen_by_someone and frappe.db.exists("Print Format", current):
			continue
		make_property_setter(
			doctype,
			None,
			"default_print_format",
			print_format,
			"Data",
			for_doctype=True,
			validate_fields_for_doctype=False,
		)
		frappe.clear_cache(doctype=doctype)


# --- company -------------------------------------------------------------------------------------------

CHART_PATH = ("setup", "charts", "th_tfrs_npaes.json")
CURRENCY = "THB"
COUNTRY = "Thailand"
VALUATION_METHOD = "Moving Average"
VAT_RATE = 7
VAT_OUTPUT_ACCOUNT = "ภาษีขาย"
SALES_TAX_TEMPLATES = (("ภาษีขาย 7%", 0), ("ภาษีขาย 7% (ราคารวม VAT)", 1))
ITEM_TAX_TEMPLATES = (("ยกเว้น VAT", 1), ("VAT 0%", 0))  # (title, VAT row not applicable)
DEFAULT_ACCOUNTS = {
	"default_cash_account": "เงินสด",
	"default_bank_account": "เงินฝากธนาคาร",
	"default_receivable_account": "ลูกหนี้การค้า",
	"default_payable_account": "เจ้าหนี้การค้า",
	"default_income_account": "รายได้จากการขาย",
	"default_expense_account": "ต้นทุนขาย",
	"default_inventory_account": "สินค้าคงเหลือ",
	"stock_received_but_not_billed": "สินค้ารับแล้วยังไม่ได้รับใบแจ้งหนี้",
	"stock_adjustment_account": "ผลต่างจากการปรับปรุงสต็อก",
	"round_off_account": "ปัดเศษ",
	"depreciation_expense_account": "ค่าเสื่อมราคา",
	"accumulated_depreciation_account": "ค่าเสื่อมราคาสะสม",
	"default_discount_account": "ส่วนลดจ่าย",
}


def load_chart() -> dict:
	"""The draft chart file (ERPNext chart format: `country_code`, `name`, `tree`; plus `_comment`)."""
	with open(frappe.get_app_path("erp_town", *CHART_PATH), encoding="utf-8") as f:
		return json.load(f)


def company_account(company: str, account_name: str) -> str | None:
	"""The company's leaf account named `account_name` (e.g. `ภาษีขาย`), as its record name."""
	return frappe.db.get_value("Account", {"company": company, "account_name": account_name, "is_group": 0})


def create_thai_company(company_name: str, abbr: str, tax_id: str) -> str:
	"""Create (once) a Thai company with the draft chart, default accounts and VAT templates.

	Idempotent: for an existing company only the missing templates/settings are added. Works on a site
	where ERPNext's setup wizard never ran (base masters and fiscal years are created first). Does not
	commit. Returns the company name.
	"""
	_check_tax_id(tax_id)
	ensure_base_masters()
	if not frappe.db.exists("Company", company_name):
		_create_company(company_name, abbr, tax_id)
	vat_account = company_account(company_name, VAT_OUTPUT_ACCOUNT)
	if not vat_account:
		frappe.throw(_("Account {0} not found for company {1}").format(VAT_OUTPUT_ACCOUNT, company_name))
	_ensure_sales_tax_templates(company_name, vat_account)
	_ensure_item_tax_templates(company_name, vat_account)
	if not frappe.db.get_single_value("Stock Settings", "valuation_method"):
		frappe.db.set_single_value("Stock Settings", "valuation_method", VALUATION_METHOD)
	return company_name


def _create_company(company_name: str, abbr: str, tax_id: str) -> None:
	from erpnext.accounts.doctype.account.chart_of_accounts.chart_of_accounts import create_charts

	flags = frappe.local.flags
	saved = (flags.ignore_chart_of_accounts, flags.ignore_root_company_validation)
	flags.ignore_chart_of_accounts = True  # Company.on_update must not install ERPNext's standard chart
	try:
		company = frappe.get_doc(
			{
				"doctype": "Company",
				"company_name": company_name,
				"abbr": abbr,
				"tax_id": tax_id,
				"default_currency": CURRENCY,
				"country": COUNTRY,
				"valuation_method": VALUATION_METHOD,
			}
		).insert(ignore_permissions=True)
		flags.ignore_root_company_validation = True
		create_charts(company.name, custom_chart=load_chart()["tree"])
	finally:
		flags.ignore_chart_of_accounts, flags.ignore_root_company_validation = saved

	defaults = {}
	for fieldname, account_name in DEFAULT_ACCOUNTS.items():
		account = company_account(company.name, account_name)
		if not account:
			frappe.throw(_("Account {0} not found for company {1}").format(account_name, company.name))
		defaults[fieldname] = account
	frappe.db.set_value("Company", company.name, defaults)

	company.reload()
	company.create_default_warehouses()  # skipped by on_update while ignore_chart_of_accounts was set
	company.set_mode_of_payment_account()


def _ensure_sales_tax_templates(company: str, vat_account: str) -> None:
	for title, included_in_print_rate in SALES_TAX_TEMPLATES:
		if frappe.db.exists("Sales Taxes and Charges Template", {"company": company, "title": title}):
			continue
		frappe.get_doc(
			{
				"doctype": "Sales Taxes and Charges Template",
				"title": title,
				"company": company,
				"taxes": [
					{
						"charge_type": "On Net Total",
						"account_head": vat_account,
						"rate": VAT_RATE,
						"description": title,
						"included_in_print_rate": included_in_print_rate,
					}
				],
			}
		).insert(ignore_permissions=True)


def _ensure_item_tax_templates(company: str, vat_account: str) -> None:
	for title, not_applicable in ITEM_TAX_TEMPLATES:
		if frappe.db.exists("Item Tax Template", {"company": company, "title": title}):
			continue
		frappe.get_doc(
			{
				"doctype": "Item Tax Template",
				"title": title,
				"company": company,
				"taxes": [{"tax_type": vat_account, "tax_rate": 0, "not_applicable": not_applicable}],
			}
		).insert(ignore_permissions=True)
