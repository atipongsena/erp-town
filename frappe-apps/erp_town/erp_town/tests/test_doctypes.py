import frappe
from frappe.tests import IntegrationTestCase

from erp_town.erp_town.doctype.town_settings.town_settings import (
	clear_town_settings_cache,
	get_town_settings,
)


def _outbox(event_id: str, seq: int = 1, subject_name: str = "SO-TEST-0001"):
	return frappe.get_doc(
		{
			"doctype": "Town Event Outbox",
			"event_id": event_id,
			"event_type": "erp.sales_order.submitted",
			"subject_doctype": "Sales Order",
			"subject_name": subject_name,
			"seq": seq,
			"correlation_id": subject_name,
			"payload": {"id": event_id},
		}
	)


class TestTownDocTypes(IntegrationTestCase):
	def setUp(self):
		frappe.db.savepoint("town_doctypes")
		clear_town_settings_cache()

	def tearDown(self):
		frappe.db.rollback(save_point="town_doctypes")
		clear_town_settings_cache()

	def test_outbox_subject_seq_is_unique(self):
		_outbox("01TESTOUTBOX0000000000000A").insert()
		with self.assertRaises(frappe.UniqueValidationError):
			_outbox("01TESTOUTBOX0000000000000B").insert()

	def test_outbox_defaults(self):
		row = _outbox("01TESTOUTBOX0000000000000C", subject_name="SO-TEST-0002").insert()
		self.assertEqual(row.name, "01TESTOUTBOX0000000000000C")
		self.assertEqual(row.status, "Pending")
		self.assertEqual(row.attempts, 0)

	def test_subject_seq_pair_is_unique(self):
		doc = {"doctype": "Town Subject Seq", "subject_doctype": "Sales Order", "subject_name": "SO-SEQ-1"}
		frappe.get_doc({**doc, "last_seq": 1}).insert()
		with self.assertRaises(frappe.UniqueValidationError):
			frappe.get_doc({**doc, "last_seq": 2}).insert()

	def test_agent_identity_user_is_unique(self):
		frappe.get_doc(
			{"doctype": "Town Agent Identity", "agent_key": "agent.test_one", "user": "Administrator"}
		).insert()
		with self.assertRaises(frappe.UniqueValidationError):
			frappe.get_doc(
				{"doctype": "Town Agent Identity", "agent_key": "agent.test_two", "user": "Administrator"}
			).insert()

	def test_agent_identity_named_by_key_and_enabled_by_default(self):
		doc = frappe.get_doc(
			{"doctype": "Town Agent Identity", "agent_key": "agent.test_three", "user": "Guest"}
		).insert()
		self.assertEqual(doc.name, "agent.test_three")
		self.assertEqual(doc.enabled, 1)

	def test_settings_defaults_when_never_saved(self):
		frappe.db.delete("Singles", {"doctype": "Town Settings"})
		frappe.db.delete("Town Naming Rule", {"parenttype": "Town Settings"})
		clear_town_settings_cache()
		settings = get_town_settings()
		self.assertEqual(settings.stream_name, "town:events")
		self.assertEqual(settings.max_attempts, 10)
		self.assertEqual(settings.stream_retention_days, 7)
		self.assertEqual(settings.outbox_retention_days, 400)
		self.assertEqual(settings.overdue_job_hour, 8)
		self.assertEqual(settings.approval_states, ["รออนุมัติ"])
		self.assertEqual(settings.naming_rules, [])
		self.assertEqual(settings.redis_town_url, frappe.conf.town_redis_url)

	def test_settings_saved_values_and_cache_cleared_on_update(self):
		self.assertEqual(get_town_settings().stream_name, "town:events")  # primes the cache
		doc = frappe.get_single("Town Settings")
		doc.stream_name = "town:test"
		doc.overdue_job_hour = 0
		doc.approval_states = "รออนุมัติ, รอผู้จัดการ\n"
		doc.set("naming_rules", [{"document_type": "Sales Order", "prefix": "SO"}])
		doc.save()
		settings = get_town_settings()
		self.assertEqual(settings.stream_name, "town:test")
		self.assertEqual(settings.overdue_job_hour, 0)
		self.assertEqual(settings.approval_states, ["รออนุมัติ", "รอผู้จัดการ"])
		self.assertEqual(settings.naming_rules, [{"document_type": "Sales Order", "prefix": "SO"}])

	def test_settings_cached_until_cleared(self):
		self.assertEqual(get_town_settings().stream_name, "town:events")  # primes the cache
		# a raw write bypasses every Frappe invalidation hook, so the cached value is still served
		frappe.db.delete("Singles", {"doctype": "Town Settings", "field": "stream_name"})
		frappe.db.sql(
			"insert into `tabSingles` (doctype, field, value) values ('Town Settings', 'stream_name', 'town:raw')"
		)
		self.assertEqual(get_town_settings().stream_name, "town:events")
		clear_town_settings_cache()
		self.assertEqual(get_town_settings().stream_name, "town:raw")

	def test_settings_cache_dropped_on_savepoint_rollback(self):
		self.assertEqual(get_town_settings().stream_name, "town:events")
		frappe.db.savepoint("town_settings_inner")
		doc = frappe.get_single("Town Settings")
		doc.stream_name = "town:inner"
		doc.redis_town_url = "redis://127.0.0.1:1/0"
		doc.save()
		self.assertEqual(get_town_settings().stream_name, "town:inner")
		frappe.db.rollback(save_point="town_settings_inner")
		settings = get_town_settings()
		self.assertEqual(settings.stream_name, "town:events")
		self.assertEqual(settings.redis_town_url, frappe.conf.town_redis_url)

	def test_settings_reflect_set_single_value(self):
		self.assertEqual(get_town_settings().stream_name, "town:events")
		frappe.db.set_single_value("Town Settings", "stream_name", "town:ssv")
		self.assertEqual(get_town_settings().stream_name, "town:ssv")

	def test_settings_returned_copy_is_isolated(self):
		doc = frappe.get_single("Town Settings")
		doc.set("naming_rules", [{"document_type": "Sales Order", "prefix": "SO"}])
		doc.save()
		first = get_town_settings()
		first.stream_name = "mutated"
		first.approval_states.append("mutated")
		first.naming_rules.append({"document_type": "Delivery Note", "prefix": "DN"})
		first.naming_rules[0]["prefix"] = "XX"
		second = get_town_settings()
		self.assertEqual(second.stream_name, "town:events")
		self.assertEqual(second.approval_states, ["รออนุมัติ"])
		self.assertEqual(second.naming_rules, [{"document_type": "Sales Order", "prefix": "SO"}])
