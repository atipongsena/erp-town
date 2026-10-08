# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Dev web server of the compose dev stack (Procfile `web:`), in place of `bench serve`.

`bench serve` pins every request to the bench's `default_site` (`town.localhost`), so `test.localhost` is
unreachable over HTTP. This server routes like production instead: by header `X-Frappe-Site-Name`, else
by the Host name when it names a site, else to `default_site` -- what `serve_default_site` means for the
production proxy -- so http://localhost:8080 stays town.localhost's Desk while
`X-Frappe-Site-Name: test.localhost` or http://test.localhost:8080 reach the test site.

Run from the bench directory: `env/bin/python -m erp_town.devserver --port 8000` (dev only).
"""

import json
import os
import sys

SITE_HEADER = "HTTP_X_FRAPPE_SITE_NAME"


def is_site(sites_path: str, name: str) -> bool:
	"""`name` is a plain directory name of `sites_path` holding a site config."""
	if not name or name.startswith(".") or "/" in name or "\\" in name:
		return False
	return os.path.isfile(os.path.join(sites_path, name, "site_config.json"))


def default_site_fallback(app, sites_path: str, default_site: str | None):
	"""WSGI middleware: requests without a site header whose Host is not a site go to `default_site`."""

	def middleware(environ, start_response):
		if default_site and not environ.get(SITE_HEADER):
			host = environ.get("HTTP_HOST", "").split(":", 1)[0]
			if not is_site(sites_path, host):
				environ[SITE_HEADER] = default_site
		return app(environ, start_response)

	return middleware


def main(argv: list[str]) -> None:
	port = int(argv[argv.index("--port") + 1]) if "--port" in argv else 8000
	# like `bench serve`, run in sites/ (frappe's logs go to ../logs, so before importing it); the reloader's
	# child process starts there already
	if not os.path.isfile("common_site_config.json"):
		os.chdir("sites")
	import frappe.app

	with open("common_site_config.json") as f:
		default_site = json.load(f).get("default_site")
	frappe.app.application = default_site_fallback(frappe.app.application, ".", default_site)
	frappe.app.serve(port=port, site=None, sites_path=".")


if __name__ == "__main__":
	main(sys.argv[1:])
