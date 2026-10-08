# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Dev web server site routing (`erp_town.devserver`): every site by header/Host, else the default site."""

import tempfile
from pathlib import Path

from frappe.tests import UnitTestCase

from erp_town.devserver import default_site_fallback

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
