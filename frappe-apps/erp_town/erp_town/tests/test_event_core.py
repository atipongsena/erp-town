import json
from contextlib import contextmanager
from datetime import UTC, datetime
from pathlib import Path
from unittest.mock import patch

import frappe
from frappe.core.doctype.system_settings.system_settings import clear_system_settings_cache
from frappe.tests import IntegrationTestCase
from werkzeug.test import EnvironBuilder
from werkzeug.wrappers import Request

from erp_town.events.actor import resolve_actor
from erp_town.events.emit import emit_raw
from erp_town.events.schema import SCHEMA_DIR, EventSchemaError, validate_event
from erp_town.events.seq import next_seq
from erp_town.events.system import emit_system_error
from erp_town.events.ulid import new_ulid

PUBLISH = "erp_town.events.publisher.publish"
HEADER = "X-Town-On-Behalf-Of"
ULID_CHARS = set("0123456789ABCDEFGHJKMNPQRSTVWXYZ")


def _emit_item(subject_name: str = "ITEM-T1", **overrides):
	kwargs = {
		"source": "erp/stock",
		"subject_doctype": "Item",
		"subject_name": subject_name,
		"company": None,
		"data": {"warehouse": "Stores - T", "fill_ratio": 0.5},
	}
	kwargs.update(overrides)
	return emit_raw("erp.stock.below_reorder", **kwargs)


def _rows(**filters):
	return frappe.get_all(
		"Town Event Outbox",
		filters=filters,
		fields=["name", "event_type", "status", "seq", "payload", "last_error", "subject_name"],
		order_by="seq asc",
	)


def _system_errors(subject_name: str):
	return [json.loads(r.payload) for r in _rows(event_type="system.error", subject_name=subject_name)]


def _error_log_count() -> int:
	return frappe.db.count("Error Log", {"method": ["like", "ERP Town:%"]})


def _ensure_user(email: str) -> str:
	if not frappe.db.exists("User", email):
		frappe.get_doc(
			{"doctype": "User", "email": email, "first_name": "Town Test", "send_welcome_email": 0}
		).insert(ignore_permissions=True)
	return email


@contextmanager
def _request_headers(headers: dict):
	previous = getattr(frappe.local, "request", None)
	frappe.local.request = Request(EnvironBuilder(path="/api/method/test", headers=headers).get_environ())
	try:
		yield
	finally:
		if previous is None:
			del frappe.local.request
		else:
			frappe.local.request = previous


class TestEventCore(IntegrationTestCase):
	def setUp(self):
		frappe.db.savepoint("town_event_core")
		self.enqueue = patch("frappe.enqueue").start()
		self.addCleanup(patch.stopall)

	def tearDown(self):
		frappe.db.rollback(save_point="town_event_core")
		frappe.flags.town_system_actor = False
		clear_system_settings_cache()
		frappe.local.system_settings = None

	def _set_time_zone(self, tz: str):
		frappe.db.set_single_value("System Settings", "time_zone", tz)
		clear_system_settings_cache()
		frappe.local.system_settings = None

	# --- emit_raw: happy path -------------------------------------------------------------------

	def test_emit_raw_writes_pending_valid_row(self):
		eid = _emit_item()
		row = frappe.get_doc("Town Event Outbox", eid)
		self.assertEqual(row.status, "Pending")
		self.assertEqual(row.seq, 1)
		self.assertEqual(row.event_type, "erp.stock.below_reorder")
		self.assertEqual((row.subject_doctype, row.subject_name), ("Item", "ITEM-T1"))
		self.assertEqual(row.correlation_id, "ITEM-T1")
		self.assertIsNotNone(row.next_attempt_at)
		ev = json.loads(row.payload)
		self.assertEqual(ev["specversion"], "1.0")
		self.assertEqual(ev["schema_version"], 1)
		self.assertEqual(len(ev["id"]), 26)
		self.assertEqual(ev["id"], eid)
		self.assertEqual(ev["seq"], 1)
		self.assertEqual(ev["subject"], {"doctype": "Item", "name": "ITEM-T1"})
		self.assertEqual(ev["correlation_id"], "ITEM-T1")
		self.assertIsNone(ev["causation_id"])
		self.assertIsNone(ev["company"])
		self.assertEqual(ev["visibility"], {"level": "public", "roles": []})
		self.assertEqual(ev["actor"], {"kind": "user", "id": "user.Administrator", "on_behalf_of": None})
		self.assertEqual(ev["data"], {"warehouse": "Stores - T", "fill_ratio": 0.5})
		validate_event(ev)

	def test_emit_raw_enqueues_publish_after_commit(self):
		eid = _emit_item()
		self.enqueue.assert_called_once_with(
			PUBLISH, queue="short", event_ids=[eid], enqueue_after_commit=True
		)

	def test_explicit_lineage_and_visibility_are_kept(self):
		cause = new_ulid()
		eid = _emit_item(
			correlation_id="DN6910-0001",
			causation_id=cause,
			company="ACME-TH",
			visibility={"level": "roles", "roles": ["Stock Manager"]},
		)
		ev = json.loads(frappe.db.get_value("Town Event Outbox", eid, "payload"))
		self.assertEqual(ev["correlation_id"], "DN6910-0001")
		self.assertEqual(ev["causation_id"], cause)
		self.assertEqual(ev["company"], "ACME-TH")
		self.assertEqual(ev["visibility"], {"level": "roles", "roles": ["Stock Manager"]})
		self.assertEqual(frappe.db.get_value("Town Event Outbox", eid, "correlation_id"), "DN6910-0001")

	def test_thai_text_stored_unescaped(self):
		eid = _emit_item(data={"warehouse": "คลังสินค้า - T", "fill_ratio": 0.25})
		payload = frappe.db.get_value("Town Event Outbox", eid, "payload")
		self.assertIn("คลังสินค้า - T", payload)
		self.assertNotIn("\\u0e04", payload)
		self.assertEqual(json.loads(payload)["data"]["warehouse"], "คลังสินค้า - T")

	def test_new_ulid_is_crockford_and_unique(self):
		ids = {new_ulid() for _ in range(50)}
		self.assertEqual(len(ids), 50)
		for value in ids:
			self.assertEqual(len(value), 26)
			self.assertLessEqual(set(value), ULID_CHARS)

	# --- seq ------------------------------------------------------------------------------------

	def test_seq_increments_per_subject(self):
		seqs = [frappe.db.get_value("Town Event Outbox", _emit_item("ITEM-SEQ-A"), "seq") for _ in range(3)]
		self.assertEqual(seqs, [1, 2, 3])
		self.assertEqual(frappe.db.get_value("Town Event Outbox", _emit_item("ITEM-SEQ-B"), "seq"), 1)
		self.assertEqual(
			frappe.db.get_value(
				"Town Subject Seq", {"subject_doctype": "Item", "subject_name": "ITEM-SEQ-A"}, "last_seq"
			),
			3,
		)

	def test_next_seq_creates_then_increments_row(self):
		self.assertEqual(next_seq("Sales Order", "SO-SEQ-T1"), 1)
		self.assertEqual(next_seq("Sales Order", "SO-SEQ-T1"), 2)
		self.assertEqual(next_seq("Delivery Note", "SO-SEQ-T1"), 1)
		self.assertEqual(frappe.db.count("Town Subject Seq", {"subject_name": "SO-SEQ-T1"}), 2)

	def test_failed_emit_on_existing_subject_restores_last_seq(self):
		_emit_item("ITEM-RESTORE")
		with patch("erp_town.events.emit.validate_event", side_effect=RuntimeError("town bug")):
			self.assertIsNone(_emit_item("ITEM-RESTORE"))
		self.assertEqual(
			frappe.db.get_value("Town Subject Seq", {"subject_name": "ITEM-RESTORE"}, "last_seq"), 1
		)
		self.assertEqual(frappe.db.get_value("Town Event Outbox", _emit_item("ITEM-RESTORE"), "seq"), 2)

	# --- invalid events -------------------------------------------------------------------------

	def test_invalid_event_is_stored_invalid_and_raises_nothing(self):
		eid = _emit_item("ITEM-BAD", data={"bogus": 1})
		row = frappe.get_doc("Town Event Outbox", eid)
		self.assertEqual(row.status, "Invalid")
		self.assertEqual(row.seq, 1)
		self.assertTrue(row.last_error)
		self.assertIn("bogus", row.last_error)
		errors = _system_errors("ITEM-BAD")
		self.assertEqual(len(errors), 1)
		err = errors[0]
		self.assertEqual(err["data"]["code"], "EVENT_INVALID")
		self.assertIn(eid, err["data"]["message"])
		self.assertEqual(err["subject"], {"doctype": "Item", "name": "ITEM-BAD"})
		self.assertEqual(err["seq"], 2)  # the Invalid row keeps seq 1
		self.assertEqual(err["causation_id"], eid)
		self.assertEqual(err["actor"], {"kind": "system", "id": "system.town", "on_behalf_of": None})
		self.assertEqual(err["visibility"], {"level": "roles", "roles": ["System Manager"]})
		validate_event(err)
		# only the system.error row is published; the Invalid row never is
		self.enqueue.assert_called_once_with(
			PUBLISH, queue="short", event_ids=[err["id"]], enqueue_after_commit=True
		)

	def test_invalid_reason_stores_invalid_row_without_validation(self):
		# e.g. the mapper raised: the event is recorded as Invalid even though its (empty) data is not checked
		eid = _emit_item("ITEM-REASON", invalid_reason="mapper failed: RuntimeError('boom')")
		row = frappe.get_doc("Town Event Outbox", eid)
		self.assertEqual(row.status, "Invalid")
		self.assertEqual(row.seq, 1)
		self.assertEqual(row.last_error, "mapper failed: RuntimeError('boom')")
		self.assertIsNone(row.next_attempt_at)
		self.assertEqual(json.loads(row.payload)["id"], eid)
		errors = _system_errors("ITEM-REASON")
		self.assertEqual([e["data"]["code"] for e in errors], ["EVENT_INVALID"])
		self.assertIn("boom", errors[0]["data"]["message"])
		self.assertEqual(errors[0]["causation_id"], eid)
		self.enqueue.assert_called_once_with(
			PUBLISH, queue="short", event_ids=[errors[0]["id"]], enqueue_after_commit=True
		)

	def test_invalid_reason_is_truncated(self):
		eid = _emit_item("ITEM-REASON-LONG", data={}, invalid_reason="x" * 5000)
		self.assertEqual(len(frappe.db.get_value("Town Event Outbox", eid, "last_error")), 1000)
		self.assertEqual(frappe.db.get_value("Town Event Outbox", eid, "status"), "Invalid")

	def test_non_json_data_is_stored_invalid(self):
		eid = _emit_item("ITEM-NOJSON", data={"warehouse": object(), "fill_ratio": 0.5})
		row = frappe.get_doc("Town Event Outbox", eid)
		self.assertEqual(row.status, "Invalid")
		self.assertIn("JSON", row.last_error)
		json.loads(row.payload)  # still stored as valid JSON for inspection
		self.assertEqual([e["data"]["code"] for e in _system_errors("ITEM-NOJSON")], ["EVENT_INVALID"])

	def test_invalid_system_error_does_not_recurse(self):
		before = _error_log_count()
		result = emit_system_error("not a code", "boom", subject_doctype="Item", subject_name="ITEM-SYS")
		self.assertIsNone(result)
		self.assertEqual(_rows(subject_name="ITEM-SYS"), [])
		self.assertFalse(frappe.db.exists("Town Subject Seq", {"subject_name": "ITEM-SYS"}))
		self.assertEqual(_error_log_count(), before + 1)
		self.enqueue.assert_not_called()

	def test_emit_system_error_writes_pending_row(self):
		eid = emit_system_error(
			"OUTBOX_PUBLISH_FAILED",
			"redis unreachable",
			subject_doctype="Town Event Outbox",
			subject_name="01JA0K6Y8Z9P3R4S5T6V7W8XA1",
		)
		ev = json.loads(frappe.db.get_value("Town Event Outbox", eid, "payload"))
		self.assertEqual(ev["type"], "system.error")
		self.assertEqual(ev["source"], "erp/town")
		self.assertEqual(ev["data"], {"code": "OUTBOX_PUBLISH_FAILED", "message": "redis unreachable"})
		self.assertEqual(
			ev["subject"], {"doctype": "Town Event Outbox", "name": "01JA0K6Y8Z9P3R4S5T6V7W8XA1"}
		)
		self.assertEqual(frappe.db.get_value("Town Event Outbox", eid, "status"), "Pending")

	# --- town-layer failures never propagate, and never leave partial writes -----------------------

	def test_unknown_subject_doctype_is_logged_not_raised(self):
		before = _error_log_count()
		frappe.local.message_log = []
		result = emit_raw(
			"erp.stock.below_reorder",
			source="erp/stock",
			subject_doctype="No Such DocType",
			subject_name="X-1",
			company=None,
			data={"warehouse": "Stores - T", "fill_ratio": 0.5},
		)
		self.assertIsNone(result)
		self.assertEqual(_rows(subject_name="X-1"), [])
		self.assertFalse(frappe.db.exists("Town Subject Seq", {"subject_name": "X-1"}))
		self.assertEqual(_error_log_count(), before + 1)
		self.assertEqual(frappe.local.message_log, [])  # the swallowed link error is not shown to the user
		self.enqueue.assert_not_called()

	def test_failure_after_seq_rolls_back_only_town_writes(self):
		todo = frappe.get_doc({"doctype": "ToDo", "description": "business write before emit"}).insert()
		before = _error_log_count()
		with patch("erp_town.events.emit.validate_event", side_effect=RuntimeError("town bug")):
			result = _emit_item("ITEM-BOOM")
		self.assertIsNone(result)
		self.assertTrue(frappe.db.exists("ToDo", todo.name))  # the surrounding transaction continues
		self.assertEqual(_rows(subject_name="ITEM-BOOM"), [])
		self.assertFalse(frappe.db.exists("Town Subject Seq", {"subject_name": "ITEM-BOOM"}))
		self.assertEqual(_error_log_count(), before + 1)
		self.enqueue.assert_not_called()
		# the subject is still usable afterwards, starting from seq 1
		self.assertEqual(frappe.db.get_value("Town Event Outbox", _emit_item("ITEM-BOOM"), "seq"), 1)

	def test_failure_reraised_when_savepoint_is_lost(self):
		# e.g. a deadlock: the server already rolled back the whole transaction, so the savepoint is gone and
		# swallowing the error would let the caller keep writing into a transaction that no longer exists.
		real_rollback = frappe.db.rollback

		def rollback(*, save_point=None, chain=False):
			if save_point and save_point.startswith("town_emit_"):
				raise frappe.db.InternalError("SAVEPOINT does not exist")
			return real_rollback(save_point=save_point, chain=chain)

		with (
			patch("erp_town.events.emit.next_seq", side_effect=RuntimeError("deadlock")),
			patch.object(frappe.db, "rollback", side_effect=rollback),
			self.assertRaises(RuntimeError),
		):
			_emit_item("ITEM-LOST")

	def test_enqueue_failure_keeps_pending_row(self):
		self.enqueue.side_effect = ConnectionError("redis-queue down")
		before = _error_log_count()
		eid = _emit_item("ITEM-NOQ")
		self.assertEqual(frappe.db.get_value("Town Event Outbox", eid, "status"), "Pending")
		self.assertEqual(_error_log_count(), before + 1)

	def test_rollback_leaves_no_row_and_no_seq(self):
		frappe.db.savepoint("town_doc_tx")
		eid = _emit_item("ITEM-RB")
		self.assertTrue(frappe.db.exists("Town Event Outbox", eid))
		frappe.db.rollback(save_point="town_doc_tx")
		self.assertFalse(frappe.db.exists("Town Event Outbox", eid))
		self.assertFalse(
			frappe.db.exists("Town Subject Seq", {"subject_doctype": "Item", "subject_name": "ITEM-RB"})
		)

	# --- time -----------------------------------------------------------------------------------

	def test_time_has_system_tz_offset(self):
		self._set_time_zone("Asia/Bangkok")
		ev = json.loads(frappe.db.get_value("Town Event Outbox", _emit_item(), "payload"))
		self.assertTrue(ev["time"].endswith("+07:00"), ev["time"])
		self.assertRegex(ev["time"], r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\+07:00$")
		# the instant is "now" regardless of the container clock's own zone
		delta = abs((datetime.fromisoformat(ev["time"]) - datetime.now(UTC)).total_seconds())
		self.assertLess(delta, 120)

	def test_time_follows_system_tz_setting(self):
		self._set_time_zone("UTC")
		ev = json.loads(frappe.db.get_value("Town Event Outbox", _emit_item(), "payload"))
		self.assertTrue(ev["time"].endswith("+00:00"), ev["time"])

	# --- actor ----------------------------------------------------------------------------------

	def test_actor_user_and_rejected_header(self):
		with _request_headers({HEADER: "Administrator"}):
			eid = _emit_item("ITEM-ACT-U")
		ev = json.loads(frappe.db.get_value("Town Event Outbox", eid, "payload"))
		self.assertEqual(ev["actor"], {"kind": "user", "id": "user.Administrator", "on_behalf_of": None})
		self.assertEqual(frappe.db.get_value("Town Event Outbox", eid, "status"), "Pending")
		errors = _system_errors("ITEM-ACT-U")
		self.assertEqual([e["data"]["code"] for e in errors], ["ON_BEHALF_OF_REJECTED"])
		self.assertEqual(errors[0]["causation_id"], eid)

	def test_actor_user_without_header_has_no_error(self):
		with _request_headers({}):
			_emit_item("ITEM-ACT-N")
		self.assertEqual(_system_errors("ITEM-ACT-N"), [])

	def _agent(self, enabled: int = 1) -> str:
		agent_user = _ensure_user("town.agent.bot@example.com")
		frappe.get_doc(
			{
				"doctype": "Town Agent Identity",
				"agent_key": "agent.test_bot",
				"user": agent_user,
				"enabled": enabled,
			}
		).insert(ignore_permissions=True)
		return agent_user

	def test_actor_agent_with_on_behalf_of(self):
		agent_user = self._agent()
		_ensure_user("test@example.com")
		with _request_headers({HEADER: "test@example.com"}), self.set_user(agent_user):
			eid = _emit_item("ITEM-ACT-A")
		ev = json.loads(frappe.db.get_value("Town Event Outbox", eid, "payload"))
		self.assertEqual(
			ev["actor"], {"kind": "agent", "id": "agent.test_bot", "on_behalf_of": "user.test@example.com"}
		)
		self.assertEqual(_system_errors("ITEM-ACT-A"), [])

	def test_actor_agent_without_header(self):
		agent_user = self._agent()
		with self.set_user(agent_user):
			self.assertEqual(resolve_actor(), {"kind": "agent", "id": "agent.test_bot", "on_behalf_of": None})

	def test_actor_agent_with_unknown_on_behalf_of(self):
		agent_user = self._agent()
		with _request_headers({HEADER: "nobody@x"}), self.set_user(agent_user):
			eid = _emit_item("ITEM-ACT-X")
		ev = json.loads(frappe.db.get_value("Town Event Outbox", eid, "payload"))
		self.assertEqual(ev["actor"], {"kind": "agent", "id": "agent.test_bot", "on_behalf_of": None})
		self.assertEqual(frappe.db.get_value("Town Event Outbox", eid, "status"), "Pending")
		self.assertEqual([e["data"]["code"] for e in _system_errors("ITEM-ACT-X")], ["ON_BEHALF_OF_INVALID"])

	def test_disabled_agent_is_a_plain_user(self):
		agent_user = self._agent(enabled=0)
		with _request_headers({HEADER: "Administrator"}), self.set_user(agent_user):
			eid = _emit_item("ITEM-ACT-D")
		ev = json.loads(frappe.db.get_value("Town Event Outbox", eid, "payload"))
		self.assertEqual(ev["actor"], {"kind": "user", "id": f"user.{agent_user}", "on_behalf_of": None})
		self.assertEqual([e["data"]["code"] for e in _system_errors("ITEM-ACT-D")], ["ON_BEHALF_OF_REJECTED"])

	def test_actor_scheduler(self):
		frappe.flags.town_system_actor = True
		with _request_headers({HEADER: "Administrator"}):
			eid = _emit_item("ITEM-ACT-S")
		ev = json.loads(frappe.db.get_value("Town Event Outbox", eid, "payload"))
		self.assertEqual(ev["actor"], {"kind": "system", "id": "system.scheduler", "on_behalf_of": None})
		self.assertEqual(_system_errors("ITEM-ACT-S"), [])

	def test_resolve_actor_without_request(self):
		self.assertIsNone(getattr(frappe.local, "request", None))
		self.assertEqual(resolve_actor(), {"kind": "user", "id": "user.Administrator", "on_behalf_of": None})

	def test_explicit_actor_skips_header(self):
		actor = {"kind": "system", "id": "system.town", "on_behalf_of": None}
		with _request_headers({HEADER: "Administrator"}):
			eid = _emit_item("ITEM-ACT-E", actor=actor)
		self.assertEqual(json.loads(frappe.db.get_value("Town Event Outbox", eid, "payload"))["actor"], actor)
		self.assertEqual(_system_errors("ITEM-ACT-E"), [])

	# --- contract (spec §9.2) -------------------------------------------------------------------

	def test_contract_fixtures(self):
		fixtures = Path(SCHEMA_DIR) / "fixtures"
		valid = sorted((fixtures / "valid").glob("*.json"))
		invalid = sorted((fixtures / "invalid").glob("*.json"))
		self.assertGreater(len(valid), 0)
		self.assertGreater(len(invalid), 0)
		for path in valid:
			with self.subTest(fixture=f"valid/{path.name}"):
				validate_event(json.loads(path.read_text(encoding="utf-8")))
		for path in invalid:
			with self.subTest(fixture=f"invalid/{path.name}"):
				with self.assertRaises(EventSchemaError) as ctx:
					validate_event(json.loads(path.read_text(encoding="utf-8")))
				self.assertTrue(ctx.exception.message)


class TestSeqConcurrency(IntegrationTestCase):
	"""Two real DB connections with open transactions, as two concurrent document submits.

	Kept out of `TestEventCore`: that class shares one never-committed transaction across its tests, and
	InnoDB keeps the locks taken after a savepoint when rolling back to it, so leftovers from earlier tests
	would block here. This class starts from a fresh transaction (setUpClass commits), like a request.
	The tests run single-threaded, so any lock wait cannot resolve by itself and surfaces as a 2 s
	lock-wait timeout instead of a hang.
	"""

	@contextmanager
	def _secondary(self):
		# Frappe's secondary_connection() captures the connection to restore only after its first
		# frappe.connect(), which would leave frappe.db on the secondary connection afterwards.
		primary = frappe.local.db
		try:
			with self.secondary_connection():
				frappe.db.sql("set session innodb_lock_wait_timeout = 2")
				yield
		finally:
			frappe.local.db = primary

	def setUp(self):
		previous = frappe.db.sql("select @@session.innodb_lock_wait_timeout")[0][0]
		frappe.db.sql("set session innodb_lock_wait_timeout = 2")
		self.addCleanup(frappe.db.sql, f"set session innodb_lock_wait_timeout = {int(previous)}")

	def test_first_events_for_different_new_subjects_do_not_block(self):
		# Monotonic document names land in the same trailing index gap; the former SELECT ... FOR UPDATE
		# + INSERT allocation deadlocked here at REPEATABLE-READ (gap lock vs insert intention).
		self.assertEqual(next_seq("Sales Order", "SO-CONC-0002"), 1)  # primary, left uncommitted
		with self._secondary():
			self.assertEqual(next_seq("Sales Order", "SO-CONC-0003"), 1)
		self.assertEqual(next_seq("Sales Order", "SO-CONC-0002"), 2)
		self.assertEqual(next_seq("Sales Order", "SO-CONC-0004"), 1)
		with self._secondary():
			self.assertEqual(next_seq("Sales Order", "SO-CONC-0003"), 2)
			self.assertEqual(next_seq("Sales Order", "SO-CONC-0005"), 1)
			# the same subject still serializes: it waits on the primary's uncommitted row
			with self.assertRaises(frappe.QueryTimeoutError):
				next_seq("Sales Order", "SO-CONC-0002")
		self.assertEqual(next_seq("Sales Order", "SO-CONC-0002"), 3)
