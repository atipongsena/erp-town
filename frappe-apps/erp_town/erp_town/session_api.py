# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Session identity for the Go `town` service (Go platform plan, ruling R4).

`whoami` tells Go who a Frappe session (cookie `sid`) or API key belongs to: the user, their roles, the
codes (abbr) of the companies they may read and the session's CSRF token, so Go can act with the caller's
own credential and the browser never hands Frappe's CSRF token to Go. Guests get Frappe's 403.
"""

import frappe
from frappe.sessions import get_csrf_token


@frappe.whitelist(methods=["GET", "POST"])
def whoami() -> dict:
	"""`{"user", "roles", "companies", "csrf_token"}` of the current session (not for Guest)."""
	user = frappe.session.user
	if user == "Guest" or not frappe.db.exists("User", {"name": user, "enabled": 1}):
		raise frappe.PermissionError
	return {
		"user": user,
		"roles": frappe.get_roles(user),
		"companies": _readable_company_codes(),
		"csrf_token": get_csrf_token(),
	}


def _readable_company_codes() -> list[str]:
	"""Abbr codes of the companies the user can read; none (not an error) without Company read."""
	try:
		return frappe.get_list("Company", pluck="abbr", order_by="abbr asc", limit_page_length=0)
	except frappe.PermissionError:
		frappe.clear_last_message()  # drop the "Insufficient Permission" msgprint from the response
		return []


@frappe.whitelist(methods=["GET"])
def agent_access() -> dict:
	"""Fresh authority of this credential, with no caller-supplied identity.

	Only the current user's enabled agent key is returned. Ordinary users can
	query their own doctype permissions as a delegation check; this endpoint
	does not confer any new ERP permission or impersonate another user.
	"""
	user = frappe.session.user
	if user == "Guest" or not frappe.db.exists("User", {"name": user, "enabled": 1}):
		raise frappe.PermissionError
	return {
		"user": user,
		"agent_key": frappe.db.get_value(
			"Town Agent Identity", {"user": user, "enabled": 1}, "agent_key"
		) or "",
		"permissions": {
			doctype: {
				"read": bool(frappe.has_permission(doctype, "read", user=user)),
				"create": bool(frappe.has_permission(doctype, "create", user=user)),
			}
			for doctype in ("Sales Order", "Delivery Note", "Sales Invoice", "Payment Entry")
		},
	}


@frappe.whitelist(methods=["POST"])
def draft_access(doctype, company, customer="", item_codes=None, mode_of_payment="") -> dict:
	"""Read-only reference checks as the authenticated user, never impersonation."""
	user = frappe.session.user
	if user == "Guest" or not frappe.db.exists("User", {"name": user, "enabled": 1}):
		raise frappe.PermissionError
	if doctype not in ("Sales Order", "Delivery Note", "Sales Invoice", "Payment Entry"):
		raise frappe.PermissionError
	if not frappe.has_permission(doctype, "create", user=user):
		raise frappe.PermissionError
	if isinstance(item_codes, str):
		item_codes = frappe.parse_json(item_codes)
	if not isinstance(item_codes, list) or len(item_codes) > 100:
		raise frappe.PermissionError
	if any(not isinstance(code, str) or not code or len(code) > 140 for code in item_codes):
		raise frappe.PermissionError
	if not isinstance(company, str) or not company or len(company) > 140:
		raise frappe.PermissionError
	if not frappe.get_list("Company", filters={"abbr": company}, pluck="name", limit_page_length=1):
		raise frappe.PermissionError
	for dt, name in (("Customer", customer), ("Mode of Payment", mode_of_payment)):
		if not isinstance(name, str) or len(name) > 140:
			raise frappe.PermissionError
		if name and not frappe.get_list(dt, filters={"name": name}, pluck="name", limit_page_length=1):
			raise frappe.PermissionError
	codes = set(item_codes)
	if codes:
		visible = set(frappe.get_list("Item", filters={"name": ["in", sorted(codes)]}, pluck="name", limit_page_length=0))
		if visible != codes:
			raise frappe.PermissionError
	return {"allowed": True}
