import frappe
from frappe.tests import IntegrationTestCase


class TestSmoke(IntegrationTestCase):
	def test_apps_installed(self):
		self.assertTrue({"frappe", "erpnext", "hrms", "erp_town"} <= set(frappe.get_installed_apps()))

	def test_town_redis_url_configured(self):
		self.assertEqual(frappe.conf.town_redis_url, "redis://redis-town:6379/0")
