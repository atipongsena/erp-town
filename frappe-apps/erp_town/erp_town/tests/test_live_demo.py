# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

from unittest.mock import patch

import frappe
from frappe.tests import UnitTestCase

from erp_town.setup.live_demo import provision, publish_pending


class TestLiveDemoSetup(UnitTestCase):
	def test_other_site_rejected_before_identity_access(self):
		with patch.object(frappe.local, "site", "town.localhost"), patch.object(frappe, "get_doc") as get_doc:
			for action in (provision, publish_pending):
				with self.assertRaises(frappe.PermissionError):
					action()
			get_doc.assert_not_called()

	def test_test_flag_required(self):
		with patch.object(frappe.local, "site", "test.localhost"), patch.dict(frappe.conf, {"allow_tests": False}), patch.object(frappe, "get_doc") as get_doc:
			for action in (provision, publish_pending):
				with self.assertRaises(frappe.PermissionError):
					action()
			get_doc.assert_not_called()

	def test_human_console_required(self):
		with patch.object(frappe.local, "site", "test.localhost"), patch.dict(frappe.conf, {"allow_tests": True}), patch.dict(frappe.session, {"user": "Guest"}), patch.object(frappe, "get_doc") as get_doc:
			for action in (provision, publish_pending):
				with self.assertRaises(frappe.PermissionError):
					action()
			get_doc.assert_not_called()

	def test_pump_uses_due_pending_rows_with_a_bounded_batch(self):
		with patch("erp_town.setup.live_demo._require_test_console"), patch("erp_town.erp_town.doctype.town_settings.town_settings.get_town_settings", return_value=frappe._dict(stream_name="town:test:events")), patch.object(frappe.db, "sql_list", return_value=["EVENT-1"]) as query, patch("erp_town.events.publisher.publish", return_value=1) as publish:
			self.assertEqual(publish_pending(), {"selected": 1, "published": 1})
			publish.assert_called_once_with(["EVENT-1"])
			statement, params = query.call_args.args
			self.assertIn("next_attempt_at <= %(now)s", statement)
			self.assertIn("limit 500", statement)
			self.assertIn("order by creation, name", statement)
			self.assertEqual(params["pending"], "Pending")
			self.assertNotIn("created_before", params)

	def test_pump_rejects_a_non_test_stream_before_querying(self):
		with patch("erp_town.setup.live_demo._require_test_console"), patch("erp_town.erp_town.doctype.town_settings.town_settings.get_town_settings", return_value=frappe._dict(stream_name="town:events")), patch.object(frappe.db, "sql_list") as query:
			with self.assertRaises(frappe.PermissionError):
				publish_pending()
			query.assert_not_called()

	def test_pump_stops_when_publication_remains_pending(self):
		with patch("erp_town.setup.live_demo._require_test_console"), patch("erp_town.erp_town.doctype.town_settings.town_settings.get_town_settings", return_value=frappe._dict(stream_name="town:test:events")), patch.object(frappe.db, "sql_list", return_value=["EVENT-1"]), patch("erp_town.events.publisher.publish", return_value=0) as publish, patch.object(frappe, "get_all", return_value=["EVENT-1"]):
			with self.assertRaises(frappe.ValidationError):
				publish_pending()
			publish.assert_called_once_with(["EVENT-1"])

	def test_pump_accepts_rows_published_by_the_normal_worker(self):
		with patch("erp_town.setup.live_demo._require_test_console"), patch("erp_town.erp_town.doctype.town_settings.town_settings.get_town_settings", return_value=frappe._dict(stream_name="town:test:events")), patch.object(frappe.db, "sql_list", return_value=["EVENT-1"]), patch("erp_town.events.publisher.publish", return_value=0), patch.object(frappe, "get_all", return_value=[]):
			self.assertEqual(publish_pending(), {"selected": 1, "published": 0})
