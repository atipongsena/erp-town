# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Helpers for the TypeScript integration and e2e suites (`tests/integration`, `tests/e2e`).

Only for a site with `allow_tests` (`test.localhost`), called through
`bench --site test.localhost execute erp_town.setup.testing.<function>` (which commits):
- `issue_test_api_keys`: Administrator's API key pair; the suites keep it in their process environment
  only -- never in a file or a log.
- `issue_client_projection_keys`: company-restricted read-only reader, exclusively on test.localhost;
  the browser acceptance suite restores the site's snapshot afterwards.
- `enable_town_scheduler`: run the scheduler for the erp_town jobs only (the publish sweeper), so a site
  that never ran its scheduler does not first work through ~100 overdue ERPNext jobs on the single dev
  worker while the suites measure publish latency. The suites restore the site's database afterwards.
"""

import frappe
from frappe import _
from frappe.utils.password import get_decrypted_password

USER = "Administrator"
APP_JOB_PREFIX = "erp_town."


def _require_allow_tests(name: str) -> None:
	if not frappe.conf.allow_tests:
		frappe.throw(_("{0} is only allowed on a site with allow_tests").format(name), frappe.PermissionError)


def issue_test_api_keys() -> str:
	"""`key:secret` of Administrator's API credentials, generated when missing (`bench execute` commits).

	Raises `frappe.PermissionError` unless the site config has `allow_tests`.
	"""
	_require_allow_tests("issue_test_api_keys")
	user = frappe.get_doc("User", USER)
	secret = get_decrypted_password("User", USER, "api_secret", raise_exception=False)
	if not (user.api_key and secret):
		user.api_key = user.api_key or frappe.generate_hash(length=15)
		secret = frappe.generate_hash(length=15)
		user.api_secret = secret
		user.save(ignore_permissions=True)
	return f"{user.api_key}:{secret}"


def issue_client_projection_keys() -> dict:
	"""Ephemeral read-only reader for the snapshot/restore browser acceptance suite.

	Only test.localhost is permitted. The caller must capture this return value;
	keys never belong in logs or files. The suite restores all database changes.
	"""
	_require_allow_tests("issue_client_projection_keys")
	if frappe.local.site != "test.localhost":
		frappe.throw("Client acceptance setup requires test.localhost", frappe.PermissionError)

	from frappe.permissions import add_permission
	from erp_town.projection_api import PROJECTION_ROLE

	user_name = "projection-client@acme-th.test"
	company = frappe.db.get_value("Company", {"abbr": "ACME-TH"}, "name")
	if not company:
		frappe.throw("Seed the test company before client acceptance setup", frappe.ValidationError)
	if frappe.db.exists("User", user_name):
		frappe.throw("Client acceptance reader already exists; restore the suite snapshot first", frappe.ValidationError)
	if not frappe.db.exists("Role", PROJECTION_ROLE):
		frappe.get_doc({"doctype": "Role", "role_name": PROJECTION_ROLE, "desk_access": 0}).insert()
	for doctype in ("Company", "Sales Order", "Delivery Note", "Sales Invoice", "Warehouse", "Item", "Bin"):
		if not frappe.db.exists("Custom DocPerm", {"parent": doctype, "role": PROJECTION_ROLE, "permlevel": 0}):
			add_permission(doctype, PROJECTION_ROLE, 0, "read")
	# Fail closed if an existing role configuration grants any write capability.
	for permission in frappe.get_all("Custom DocPerm", filters={"role": PROJECTION_ROLE}, fields=["*"]):
		if any(permission.get(field) for field in ("write", "create", "delete", "submit", "cancel", "amend", "share")):
			frappe.throw("Projection reader role must be read-only", frappe.PermissionError)
	# Existing seed staff otherwise have ERP access to every company. Narrow the
	# temporary browser identities so cross-company denial tests are meaningful.
	from erp_town.setup.seed import SALES_USER, STOCK_USER, ACCOUNTS_USER
	for staff in (SALES_USER, STOCK_USER, ACCOUNTS_USER):
		if not frappe.db.exists("User Permission", {"user": staff, "allow": "Company", "for_value": company}):
			frappe.get_doc({
				"doctype": "User Permission", "user": staff, "allow": "Company",
				"for_value": company, "apply_to_all_doctypes": 1,
			}).insert(ignore_permissions=True)
		frappe.clear_cache(user=staff)
	key, secret = frappe.generate_hash(length=15), frappe.generate_hash(length=15)
	frappe.get_doc({
		"doctype": "User", "email": user_name, "first_name": "Client projection reader",
		"enabled": 1, "user_type": "System User", "send_welcome_email": 0,
		"roles": [{"role": PROJECTION_ROLE}], "api_key": key, "api_secret": secret,
	}).insert(ignore_permissions=True)
	frappe.get_doc({
		"doctype": "User Permission", "user": user_name, "allow": "Company",
		"for_value": company, "apply_to_all_doctypes": 1,
	}).insert(ignore_permissions=True)
	frappe.clear_cache(user=user_name)
	return {"user": user_name, "key": key, "secret": secret}


def enable_town_scheduler() -> list[str]:
	"""Stop every Scheduled Job Type but erp_town's, enable the scheduler; return the methods left running.

	Raises `frappe.PermissionError` unless the site config has `allow_tests`.
	"""
	from frappe.utils.scheduler import enable_scheduler

	_require_allow_tests("enable_town_scheduler")
	running = []
	for job in frappe.get_all("Scheduled Job Type", fields=["name", "method", "stopped"]):
		ours = job.method.startswith(APP_JOB_PREFIX)
		if ours:
			running.append(job.method)
		if job.stopped != (not ours):
			frappe.db.set_value("Scheduled Job Type", job.name, "stopped", int(not ours))
	enable_scheduler()
	return running


def issue_agent_acceptance_keys() -> dict:
	"""Temporary narrow agent/delegator keys; capture in memory and restore the site."""
	_require_allow_tests("issue_agent_acceptance_keys")
	if frappe.local.site != "test.localhost":
		raise frappe.PermissionError
	from erp_town.setup.seed import AGENTS, SALES_USER
	result = {"agent_key": AGENTS[0].key}
	company = frappe.db.get_value("Company", {"abbr": "ACME-TH"}, "name")
	if not company:
		raise frappe.ValidationError
	for kind, name in (("agent", AGENTS[0].user), ("delegator", SALES_USER)):
		user = frappe.get_doc("User", name)
		if not user.enabled or not frappe.has_permission("Sales Order", "create", user=name):
			raise frappe.PermissionError
		key, secret = frappe.generate_hash(length=15), frappe.generate_hash(length=15)
		user.api_key, user.api_secret = key, secret
		user.save(ignore_permissions=True)
		if not frappe.db.exists("User Permission", {"user": name, "allow": "Company", "for_value": company}):
			frappe.get_doc({"doctype": "User Permission", "user": name, "allow": "Company", "for_value": company, "apply_to_all_doctypes": 1}).insert(ignore_permissions=True)
		frappe.clear_cache(user=name)
		result[kind] = {"user": name, "key": key, "secret": secret}
	return result
