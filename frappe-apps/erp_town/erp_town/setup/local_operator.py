# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Approved human operator access for the local development site only.

Not whitelisted or run at install/startup. No password, API key, role definition,
company restriction, service identity or production account is changed.
"""

import frappe

USER = "sales@acme-th.test"
HUMAN_ROLES = frozenset((
	"System Manager", "Accounts User", "Accounts Manager", "Sales User", "Sales Manager",
	"Sales Master Manager", "Stock User", "Stock Manager", "Item Manager",
	"Purchase User", "Purchase Manager", "Purchase Master Manager", "HR User", "HR Manager",
	"Employee", "Employee Self Service", "Leave Approver", "Expense Approver", "Interviewer",
	"Manufacturing User", "Manufacturing Manager", "Quality Manager", "Projects User",
	"Projects Manager", "Maintenance User", "Maintenance Manager", "Delivery User",
	"Delivery Manager", "Fulfillment User", "Fleet Manager", "Support Team", "Academics User",
	"Analytics", "Auditor", "Marketing Manager", "Newsletter Manager", "Knowledge Base Editor",
	"Knowledge Base Contributor", "Inbox User", "Translator", "Prepared Report User",
	"Script Manager", "Report Manager", "Workspace Manager", "Dashboard Manager", "Website Manager",
))


def select_roles(rows):
	return sorted({row.name for row in rows if row.name in HUMAN_ROLES and row.desk_access and not row.disabled})


def provision(dry_run=True):
	"""Review/apply the explicit operator profile; callable only from the dev console."""
	if frappe.local.site != "town.localhost" or not frappe.conf.developer_mode or frappe.session.user != "Administrator":
		frappe.throw("Operator access requires the Administrator dev console on town.localhost", frappe.PermissionError)
	if not isinstance(dry_run, bool):
		frappe.throw("dry_run must be a boolean", frappe.ValidationError)
	user = frappe.get_doc("User", USER)
	if not user.enabled or user.user_type != "System User":
		frappe.throw("Existing enabled human System User required", frappe.ValidationError)
	roles = select_roles(frappe.get_all("Role", fields=["name", "desk_access", "disabled"]))
	if "System Manager" not in roles:
		frappe.throw("System Manager role must already exist", frappe.ValidationError)
	before = sorted(frappe.get_roles(USER))
	added = [role for role in roles if role not in before]
	if not dry_run and added:
		user.add_roles(*added)
		frappe.clear_cache(user=USER)
		frappe.db.commit()
	return {"site": frappe.local.site, "user": USER, "dry_run": dry_run, "added_roles": added, "roles": roles}
