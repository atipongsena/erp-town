# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Dev web server site routing (`erp_town.devserver`): every site by header/Host, else the default site."""

import tempfile
from pathlib import Path

from frappe.tests import UnitTestCase
from werkzeug.test import Client
from werkzeug.wrappers import Response

from erp_town.devserver import build_application, default_site_fallback

HEADER = "HTTP_X_FRAPPE_SITE_NAME"


class TestDefaultSiteFallback(UnitTestCase):
	def setUp(self):
		tmp = tempfile.TemporaryDirectory()
		self.addCleanup(tmp.cleanup)
		self.sites = tmp.name
		for site in ("town.localhost", "test.localhost"):
			(Path(self.sites) / site).mkdir()
			(Path(self.sites) / site / "site_config.json").write_text("{}")
		self.seen = []

		def app(environ, start_response):
			self.seen.append(environ.get(HEADER))
			return [b"ok"]

		self.app = default_site_fallback(app, self.sites, "town.localhost")

	def _request(self, **environ):
		self.assertEqual(self.app(environ, lambda *a: None), [b"ok"])
		return self.seen[-1]

	def test_unknown_host_goes_to_the_default_site(self):
		self.assertEqual(self._request(HTTP_HOST="localhost:8080"), "town.localhost")
		self.assertEqual(self._request(HTTP_HOST="127.0.0.1:8080"), "town.localhost")
		self.assertEqual(self._request(), "town.localhost")

	def test_host_naming_a_site_is_served_by_that_site(self):
		self.assertIsNone(self._request(HTTP_HOST="test.localhost:8080"))

	def test_explicit_site_header_wins(self):
		self.assertEqual(
			self._request(HTTP_HOST="localhost:8080", **{HEADER: "test.localhost"}), "test.localhost"
		)

	def test_host_outside_the_sites_dir_is_not_a_site(self):
		self.assertEqual(self._request(HTTP_HOST="..:8080"), "town.localhost")
		self.assertEqual(self._request(HTTP_HOST="test.localhost/..:8080"), "town.localhost")

	def test_no_default_site_leaves_routing_to_frappe(self):
		app = default_site_fallback(lambda environ, sr: [environ.get(HEADER)], self.sites, None)
		self.assertEqual(app({"HTTP_HOST": "localhost:8080"}, None), [None])


class TestStaticFilesPerSite(UnitTestCase):
	"""`/files/` and `/assets/` sit behind the site fallback: public files come from the routed site."""

	def setUp(self):
		tmp = tempfile.TemporaryDirectory()
		self.addCleanup(tmp.cleanup)
		sites = Path(tmp.name)
		for site, content in (("town.localhost", b"town file"), ("test.localhost", b"test file")):
			files = sites / site / "public" / "files"
			files.mkdir(parents=True)
			(sites / site / "site_config.json").write_text("{}")
			(files / f"{site.split('.')[0]}.txt").write_bytes(content)
		(sites / "assets").mkdir()
		(sites / "assets" / "app.js").write_bytes(b"asset")
		self.routed = []

		def app(environ, start_response):
			self.routed.append(environ.get(HEADER))
			return Response("from frappe", status=404)(environ, start_response)

		self.client = Client(build_application(app, str(sites), "town.localhost"))

	def _get(self, path, host="localhost:8080", **headers):
		res = self.client.get(path, headers={"Host": host, **headers})
		return res.status_code, res.get_data()

	def test_default_site_files_via_localhost(self):
		self.assertEqual(self._get("/files/town.txt"), (200, b"town file"))
		self.assertEqual(self.routed, [])  # served without reaching frappe

	def test_test_site_files_via_header_or_host(self):
		self.assertEqual(
			self._get("/files/test.txt", **{"X-Frappe-Site-Name": "test.localhost"}), (200, b"test file")
		)
		self.assertEqual(self._get("/files/test.txt", host="test.localhost:8080"), (200, b"test file"))

	def test_a_sites_file_is_not_served_for_another_site(self):
		self.assertEqual(self._get("/files/test.txt"), (404, b"from frappe"))
		self.assertEqual(self.routed, ["town.localhost"])

	def test_missing_file_and_traversal_fall_through_to_frappe(self):
		self.assertEqual(self._get("/files/nope.png")[0], 404)
		self.assertEqual(self._get("/files/../../test.localhost/public/files/test.txt")[0], 404)
		self.assertEqual(self._get("/files/x", **{"X-Frappe-Site-Name": "../town.localhost"})[0], 404)

	def test_assets_are_shared(self):
		self.assertEqual(self._get("/assets/app.js"), (200, b"asset"))
		self.assertEqual(self._get("/assets/app.js", host="test.localhost:8080"), (200, b"asset"))
