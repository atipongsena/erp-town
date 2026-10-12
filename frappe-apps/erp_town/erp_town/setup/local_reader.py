# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Explicit local reader provisioning. Capture the returned credential; never log it.

Not whitelisted and never called during app install or normal startup. Run only
after operator approval using bench execute. Reader authority is never widened.
"""

import frappe
from frappe.utils.password import get_decrypted_password

from erp_town.projection_api import PROJECTION_ROLE

USER = "world-reader@erp-town.local"
DOCTYPES = ("Company", "Sales Order", "Delivery Note", "Sales Invoice", "Warehouse", "Item", "Bin")
FORBIDDEN = ("write", "create", "delete", "submit", "cancel", "amend", "share", "export", "report", "email", "print", "if_owner", "import", "impersonate")
ROW_METADATA = frozenset(("name", "creation", "modified", "modified_by", "owner", "idx", "docstatus"))


def _copy_standard_permissions(doctype):
	rows = frappe.get_all("DocPerm", filters={"parent": doctype}, fields=["*"], order_by="idx asc")
	for row in rows:
		values = {key: value for key, value in row.items() if key not in ROW_METADATA}
		values.update(doctype="Custom DocPerm", parent=doctype, parenttype="DocType", parentfield="permissions")
		frappe.get_doc(values).insert(ignore_permissions=True)
	return len(rows)


def _preserve_standard_permissions(doctype):
	# A single custom row replaces the ENTIRE standard permission table in Frappe.
	# Copy it first so adding a reader never removes normal staff authority.
	if not frappe.db.exists("Custom DocPerm", {"parent": doctype}):
		_copy_standard_permissions(doctype)


def repair_existing_permissions(dry_run=True):
	"""Restore only the recognizable reader-only shadow created by old provisioning.

	Other customized permission tables are preserved. No reader key or identity is read.
	"""
	if frappe.local.site != "town.localhost" or not frappe.conf.developer_mode or frappe.session.user != "Administrator":
		frappe.throw("Reader permission repair requires the Administrator dev console on town.localhost", frappe.PermissionError)
	if not isinstance(dry_run, bool):
		frappe.throw("dry_run must be a boolean", frappe.ValidationError)
	validate_permissions(frappe.get_all("Custom DocPerm", filters={"role": PROJECTION_ROLE}, fields=["*"]))
	restored = {}
	preserved_custom = []
	for doctype in DOCTYPES:
		custom = frappe.get_all("Custom DocPerm", filters={"parent": doctype}, fields=["role"])
		if any(row.role != PROJECTION_ROLE for row in custom):
			preserved_custom.append(doctype)
			continue
		rows = frappe.get_all("DocPerm", filters={"parent": doctype}, fields=["name"])
		if rows:
			restored[doctype] = len(rows)
			if not dry_run:
				_copy_standard_permissions(doctype)
	if not dry_run and restored:
		frappe.clear_cache()
		frappe.db.commit()
	return {"dry_run": dry_run, "restored": restored, "preserved_custom": preserved_custom}


def validate_permissions(rows):
	if len(rows) != len(DOCTYPES) or {r.get("parent") for r in rows} != set(DOCTYPES):
		frappe.throw("Projection reader requires the exact approved read profile", frappe.PermissionError)
	for row in rows:
		if not row.get("read") or row.get("permlevel") or any(row.get(field) for field in FORBIDDEN):
			frappe.throw("Projection reader requires the exact approved read profile", frappe.PermissionError)


def provision():
	"""Create/reuse only the approved ACME-TH dev reader; never rotate existing keys."""
	if frappe.local.site != "town.localhost" or not frappe.conf.developer_mode or frappe.session.user != "Administrator":
		frappe.throw("Local reader provisioning requires the Administrator dev console on town.localhost", frappe.PermissionError)
	company = frappe.db.get_value("Company", {"abbr": "ACME-TH"}, "name")
	if not company:
		frappe.throw("Existing ACME-TH company required", frappe.ValidationError)
	if not frappe.db.exists("Role", PROJECTION_ROLE):
		frappe.get_doc({"doctype": "Role", "role_name": PROJECTION_ROLE, "desk_access": 1}).insert()
	if not frappe.db.get_value("Role", PROJECTION_ROLE, "desk_access"):
		frappe.throw("Projection role must support the approved System User", frappe.PermissionError)
	# Never silently repair an existing role; its grants may serve another operator.
	rows = frappe.get_all("Custom DocPerm", filters={"role": PROJECTION_ROLE}, fields=["*"])
	standard = frappe.get_all("DocPerm", filters={"role": PROJECTION_ROLE}, fields=["*"])
	if standard:
		frappe.throw("Projection role has unexpected standard permissions", frappe.PermissionError)
	if rows:
		validate_permissions(rows)
	else:
		for doctype in DOCTYPES:
			_preserve_standard_permissions(doctype)
			frappe.get_doc({"doctype": "Custom DocPerm", "parent": doctype, "parenttype": "DocType", "parentfield": "permissions", "role": PROJECTION_ROLE, "permlevel": 0, "read": 1, **{field: 0 for field in FORBIDDEN}}).insert(ignore_permissions=True)
		validate_permissions(frappe.get_all("Custom DocPerm", filters={"role": PROJECTION_ROLE}, fields=["*"]))
	if frappe.db.exists("User", USER):
		user = frappe.get_doc("User", USER)
		roles = {row.role for row in user.roles}
		permissions = frappe.get_all("User Permission", filters={"user": USER}, fields=["allow", "for_value", "apply_to_all_doctypes"])
		if not user.enabled or user.user_type != "System User" or roles - {PROJECTION_ROLE, "All", "Guest", "Desk User"} or PROJECTION_ROLE not in roles or len(permissions) != 1 or permissions[0].allow != "Company" or permissions[0].for_value != company or not permissions[0].apply_to_all_doctypes:
			frappe.throw("Existing reader differs from the approved profile; no changes made", frappe.PermissionError)
		secret = get_decrypted_password("User", USER, "api_secret", raise_exception=False)
		if not user.api_key or not secret:
			frappe.throw("Existing reader credential is incomplete; no rotation performed", frappe.PermissionError)
	else:
		secret = frappe.generate_hash(length=32)
		user = frappe.get_doc({"doctype": "User", "email": USER, "first_name": "World projection reader", "enabled": 1, "user_type": "System User", "send_welcome_email": 0, "roles": [{"role": PROJECTION_ROLE}], "api_key": frappe.generate_hash(length=32), "api_secret": secret}).insert(ignore_permissions=True)
		frappe.get_doc({"doctype": "User Permission", "user": USER, "allow": "Company", "for_value": company, "apply_to_all_doctypes": 1}).insert(ignore_permissions=True)
	frappe.clear_cache(user=USER)
	return {"user": USER, "company": "ACME-TH", "key": user.api_key, "secret": secret}
