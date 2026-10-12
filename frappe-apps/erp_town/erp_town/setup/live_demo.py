# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Ephemeral identities for the loopback, snapshot-restored ERP live demo.

Not whitelisted. The launcher owns the test-site snapshot and keeps these
credentials in memory. Production/development companies are never modified.
"""

import frappe


def _require_test_console():
	if frappe.local.site != "test.localhost" or not frappe.conf.allow_tests or frappe.session.user != "Administrator":
		frappe.throw("Live demo setup requires the Administrator test.localhost console", frappe.PermissionError)


def provision():
	_require_test_console()
	from erp_town.setup.local_operator import select_roles
	from erp_town.setup.seed import SALES_USER, STOCK_USER, ACCOUNTS_USER
	company = frappe.db.get_value("Company", {"abbr": "ACME-TH"}, "name")
	if not company:
		frappe.throw("Seed the isolated test company first", frappe.ValidationError)
	staff = {}
	for department, name in (("shop", SALES_USER), ("warehouse", STOCK_USER), ("bank", ACCOUNTS_USER)):
		user = frappe.get_doc("User", name)
		if not user.enabled or user.user_type != "System User":
			frappe.throw("Enabled seeded test staff required", frappe.ValidationError)
		key, secret = frappe.generate_hash(length=15), frappe.generate_hash(length=15)
		user.api_key, user.api_secret = key, secret
		user.save(ignore_permissions=True)
		staff[department] = {"user": name, "key": key, "secret": secret}
	viewer_name = "live-demo-owner@acme-th.test"
	if frappe.db.exists("User", viewer_name):
		frappe.throw("Restore the previous live demo snapshot first", frappe.ValidationError)
	password = frappe.generate_hash(length=32)
	roles = select_roles(frappe.get_all("Role", fields=["name", "desk_access", "disabled"]))
	frappe.get_doc({
		"doctype": "User", "email": viewer_name, "first_name": "Live demo operator",
		"enabled": 1, "user_type": "System User", "send_welcome_email": 0,
		"roles": [{"role": role} for role in roles], "new_password": password,
	}).insert(ignore_permissions=True)
	frappe.get_doc({"doctype": "User Permission", "user": viewer_name, "allow": "Company",
		"for_value": company, "apply_to_all_doctypes": 1}).insert(ignore_permissions=True)
	frappe.clear_cache()
	return {"staff": staff, "viewer": {"user": viewer_name, "password": password}}


def publish_pending():
	"""Bounded local pump, honoring publisher locking, deduplication and backoff.

	The loopback launcher calls this serially, then awaits its exit before snapshot
	restore. This bypasses only the scheduled sweeper's minimum row age, leaving
	all normal publisher failure/retry bookkeeping and sequence ordering intact.
	"""
	_require_test_console()
	from frappe.utils import now_datetime
	from erp_town.erp_town.doctype.town_settings.town_settings import get_town_settings
	from erp_town.events.publisher import publish
	if get_town_settings().stream_name != "town:test:events":
		frappe.throw("Live demo publication requires the isolated test stream", frappe.PermissionError)
	ids = frappe.db.sql_list("""
		select name from `tabTown Event Outbox`
		where status = %(pending)s
			and (next_attempt_at is null or next_attempt_at <= %(now)s)
		order by creation, name
		limit 500
	""", {"pending": "Pending", "now": now_datetime()})
	if not ids:
		return {"selected": 0, "published": 0}
	published = publish(ids)
	if published != len(ids):
		# A normal background publisher may have won the row lock. Already
		# Published rows are healthy; pending/failed rows stop the demo instead
		# of hiding an unavailable stream or retrying a possibly ambiguous write.
		remaining = frappe.get_all("Town Event Outbox", filters={
			"name": ["in", ids], "status": ["!=", "Published"],
		}, limit_page_length=500, pluck="name")
		if remaining:
			frappe.throw("Live demo event delivery failed", frappe.ValidationError)
	return {"selected": len(ids), "published": published}
