# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Helpers for the TypeScript integration and e2e suites (`tests/integration`, `tests/e2e`).

Only for a site with `allow_tests` (`test.localhost`), called through
`bench --site test.localhost execute erp_town.setup.testing.<function>` (which commits):
- `issue_test_api_keys`: Administrator's API key pair; the suites keep it in their process environment
  only -- never in a file or a log.
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
