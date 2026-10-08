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
	return {
		"user": user,
		"roles": frappe.get_roles(user),
		"companies": frappe.get_list("Company", pluck="abbr", order_by="abbr asc", limit_page_length=0),
		"csrf_token": get_csrf_token(),
	}
