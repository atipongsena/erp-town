# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Dev web server of the compose dev stack (Procfile `web:`), in place of `bench serve`.

`bench serve` pins every request to the bench's `default_site` (`town.localhost`), so `test.localhost` is
unreachable over HTTP. This server routes like production instead: by header `X-Frappe-Site-Name`, else
by the Host name when it names a site, else to `default_site` -- what `serve_default_site` means for the
production proxy -- so http://localhost:8080 stays town.localhost's Desk (and its public `/files/`) while
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


def build_application(app, sites_path: str, default_site: str | None):
	"""`app` behind the dev statics (`/assets`, per-site public `/files`) behind the site fallback.

	Frappe's own statics (`frappe.app.application_with_statics`) resolve the `/files` site from the Host
	only, so they must not wrap the fallback: `localhost:8080` would look in `sites/localhost`. Here the
	fallback is outermost and `/files` uses the routed site (header, else Host). A file that does not
	exist falls through to Frappe instead of raising.
	"""
	import threading
	from pathlib import Path

	from frappe.middlewares import StaticDataMiddleware
	from frappe.utils import cstr
	from werkzeug.middleware.shared_data import SharedDataMiddleware

	class SiteFiles(StaticDataMiddleware):
		def __init__(self, *args, **kwargs):
			super().__init__(*args, **kwargs)
			self._request = threading.local()  # the dev server is threaded: no per-request state on self

		def __call__(self, environ, start_response):
			site = environ.get(SITE_HEADER) or environ.get("HTTP_HOST", "").split(":", 1)[0]
			self._request.site = site if is_site(sites_path, site) else None
			return super().__call__(environ, start_response)

		def get_directory_loader(self, directory):
			def loader(path):
				site = getattr(self._request, "site", None)
				if not site or path is None:
					return None, None
				files_path = (Path(directory) / site / "public" / "files").resolve()
				file = (files_path / cstr(path)).resolve()
				if not file.is_relative_to(files_path) or not file.is_file():
					return None, None
				return file.name, self._opener(str(file))

			return loader

	sites_path = os.path.abspath(sites_path)
	app = SharedDataMiddleware(app, {"/assets": os.path.join(sites_path, "assets")})
	app = SiteFiles(app, {"/files": sites_path})
	return default_site_fallback(app, sites_path, default_site)


def main(argv: list[str]) -> None:
	port = int(argv[argv.index("--port") + 1]) if "--port" in argv else 8000
	# like `bench serve`, run in sites/ (frappe's logs go to ../logs, so before importing it); the reloader's
	# child process starts there already
	if not os.path.isfile("common_site_config.json"):
		os.chdir("sites")
	import frappe.app

	with open("common_site_config.json") as f:
		default_site = json.load(f).get("default_site")
	os.environ["NO_STATICS"] = "1"  # serve() must not wrap the statics outside the fallback
	frappe.app.application = build_application(frappe.app.application, ".", default_site)
	frappe.app.serve(port=port, site=None, sites_path=".")


if __name__ == "__main__":
	main(sys.argv[1:])
