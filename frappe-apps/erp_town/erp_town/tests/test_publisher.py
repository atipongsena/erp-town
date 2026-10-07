"""Publisher, sweeper and purge (spec §4.3, §8) against the real `redis-town`.

`publish()`, `sweep()` and `purge()` commit (they run as background jobs), so these tests commit too:
every row they create is deleted again in `tearDown`, Town Settings are restored, and each test writes to
its own stream `town:test:<hash>` which is deleted afterwards. `frappe.enqueue` is patched so no real
publish job races the test.
"""

import json
import time
from datetime import timedelta
from unittest.mock import patch

import frappe
from frappe.tests import IntegrationTestCase
from frappe.utils import add_days, get_datetime, now_datetime

from erp_town.erp_town.doctype.town_settings.town_settings import clear_town_settings_cache
from erp_town.events.emit import emit_raw
from erp_town.events.publisher import publish, purge, sweep
from erp_town.events.redis_client import get_town_redis
from erp_town.events.system import emit_system_error

OUTBOX = "Town Event Outbox"
REDIS_DOWN_URL = "redis://127.0.0.1:1/0"
SETTINGS_KEYS = (
	"stream_name",
	"redis_town_url",
	"max_attempts",
	"outbox_retention_days",
	"stream_retention_days",
)


class TestPublisher(IntegrationTestCase):
	def setUp(self):
		self.enqueue = patch("frappe.enqueue").start()
		self.addCleanup(patch.stopall)
		self.saved_settings = {k: frappe.db.get_single_value("Town Settings", k) for k in SETTINGS_KEYS}
		self.stream = f"town:test:{frappe.generate_hash(length=8)}"
		self.subjects: list[str] = []
		self._set_settings(stream_name=self.stream)
		self.redis_url = frappe.conf.get("town_redis_url")

	def tearDown(self):
		frappe.db.rollback()
		names = [*self.subjects, *self._outbox_names()]
		if names:
			frappe.db.delete(OUTBOX, {"subject_name": ("in", names)})
			frappe.db.delete(OUTBOX, {"name": ("in", names)})
			frappe.db.delete("Town Subject Seq", {"subject_name": ("in", names)})
		for key, value in self.saved_settings.items():
			frappe.db.set_single_value("Town Settings", key, value)
		frappe.db.commit()
		clear_town_settings_cache()
		get_town_redis(self.redis_url).delete(self.stream)

	# --- helpers ------------------------------------------------------------------------------

	def _set_settings(self, **values):
		for key, value in values.items():
			frappe.db.set_single_value("Town Settings", key, value)
		clear_town_settings_cache()

	def _emit(self, data: dict | None = None) -> str:
		subject = f"ITEM-PUB-{frappe.generate_hash(length=8)}"
		self.subjects.append(subject)
		eid = emit_raw(
			"erp.stock.below_reorder",
			source="erp/stock",
			subject_doctype="Item",
			subject_name=subject,
			company=None,
			data=data or {"warehouse": "Stores - T", "fill_ratio": 0.5},
		)
		self.assertIsNotNone(eid)
		return eid

	def _outbox_names(self) -> list[str]:
		if not self.subjects:
			return []
		return frappe.get_all(OUTBOX, filters={"subject_name": ("in", self.subjects)}, pluck="name")

	def _row(self, eid: str):
		return frappe.get_doc(OUTBOX, eid)

	def _stream_ids(self) -> list[str]:
		return [json.loads(f[b"event"].decode())["id"] for _, f in get_town_redis().xrange(self.stream)]

	def _set_row(self, eid: str, **values):
		frappe.db.set_value(OUTBOX, eid, values, update_modified=False)

	def _publish_failed_errors(self, eid: str) -> list[dict]:
		rows = frappe.get_all(
			OUTBOX,
			filters={"event_type": "system.error", "subject_doctype": OUTBOX, "subject_name": eid},
			pluck="payload",
		)
		return [json.loads(p) for p in rows]

	# --- publish ------------------------------------------------------------------------------

	def test_publish_marks_published_and_roundtrips_thai(self):
		eid = self._emit({"warehouse": "คลังสินค้า - T", "fill_ratio": 0.5})
		self.assertEqual(publish([eid]), 1)
		row = self._row(eid)
		self.assertEqual(row.status, "Published")
		self.assertTrue(row.stream_id)
		self.assertIsNotNone(row.published_at)
		[(sid, fields)] = get_town_redis().xrange(self.stream)
		self.assertEqual(sid.decode(), row.stream_id)
		self.assertEqual(fields[b"event"], row.payload.encode("utf-8"))  # byte-exact
		event = json.loads(fields[b"event"].decode())
		self.assertEqual(event["id"], eid)
		self.assertEqual(event["data"]["warehouse"], "คลังสินค้า - T")

	def test_publish_twice_is_noop(self):
		eid = self._emit()
		self.assertEqual(publish([eid]), 1)
		self.assertEqual(publish([eid]), 0)
		self.assertEqual(get_town_redis().xlen(self.stream), 1)

	def test_publish_ignores_unknown_and_non_pending_ids(self):
		invalid = emit_raw(
			"erp.stock.below_reorder",
			source="erp/stock",
			subject_doctype="Item",
			subject_name=self._track("ITEM-PUB-INV"),
			company=None,
			data={},
			invalid_reason="mapper failed",
		)
		self.assertEqual(publish(["01NOTANEVENTID0000000000000", invalid]), 0)
		self.assertEqual(self._row(invalid).status, "Invalid")
		self.assertEqual(get_town_redis().xlen(self.stream), 0)

	def test_publish_ignores_backoff_of_listed_rows(self):
		eid = self._emit()
		self._set_row(eid, attempts=3, next_attempt_at=now_datetime() + timedelta(minutes=5))
		self.assertEqual(publish([eid]), 1)
		self.assertEqual(self._row(eid).status, "Published")

	def test_publish_trims_stream_by_retention(self):
		# `MINID ~` trims whole radix-tree nodes only, so assert the arguments rather than the stream
		self._set_settings(stream_retention_days=7)
		redis = get_town_redis()
		eid = self._emit()
		before_ms = int(time.time() * 1000)
		with patch.object(redis, "xadd", wraps=redis.xadd) as xadd:
			self.assertEqual(publish([eid]), 1)
		after_ms = int(time.time() * 1000)
		[call] = xadd.call_args_list
		self.assertEqual(call.args[0], self.stream)
		self.assertTrue(call.kwargs["approximate"])
		minid_ms, minid_seq = call.kwargs["minid"].split("-")
		self.assertEqual(minid_seq, "0")
		week_ms = 7 * 86_400_000
		self.assertTrue(before_ms - week_ms <= int(minid_ms) <= after_ms - week_ms)
		self.assertEqual(self._stream_ids(), [eid])

	def test_redis_down_sets_backoff(self):
		eid = self._emit()
		self._set_settings(redis_town_url=REDIS_DOWN_URL)
		before = now_datetime()
		self.assertEqual(publish([eid]), 0)
		after = now_datetime()
		row = self._row(eid)
		self.assertEqual(row.status, "Pending")
		self.assertEqual(row.attempts, 1)
		self.assertTrue(row.last_error)
		self.assertFalse(row.stream_id)
		next_at = get_datetime(row.next_attempt_at)
		self.assertGreaterEqual(next_at, before + timedelta(seconds=2) - timedelta(seconds=1))
		self.assertLessEqual(next_at, after + timedelta(seconds=2) + timedelta(seconds=1))
		self.assertEqual(self._publish_failed_errors(eid), [])

	def test_backoff_is_capped_at_300_seconds(self):
		eid = self._emit()
		self._set_row(eid, attempts=8)
		self._set_settings(redis_town_url=REDIS_DOWN_URL, max_attempts=20)
		before = now_datetime()
		publish([eid])
		row = self._row(eid)
		self.assertEqual(row.attempts, 9)
		self.assertGreaterEqual(get_datetime(row.next_attempt_at), before + timedelta(seconds=299))
		self.assertLessEqual(get_datetime(row.next_attempt_at), now_datetime() + timedelta(seconds=301))

	def test_max_attempts_marks_failed_and_emits_error(self):
		eid = self._emit()
		self._set_row(eid, attempts=9)
		self._set_settings(redis_town_url=REDIS_DOWN_URL)
		self.assertEqual(publish([eid]), 0)
		row = self._row(eid)
		self.assertEqual(row.status, "Failed")
		self.assertEqual(row.attempts, 10)
		self.assertTrue(row.last_error)
		[error] = self._publish_failed_errors(eid)
		self.assertEqual(error["type"], "system.error")
		self.assertEqual(error["data"]["code"], "OUTBOX_PUBLISH_FAILED")
		self.assertIn(eid, error["data"]["message"])
		self.assertEqual(error["subject"], {"doctype": OUTBOX, "name": eid})
		self.assertEqual(error["causation_id"], eid)
		self.assertEqual(error["visibility"], {"level": "roles", "roles": ["System Manager"]})

	def test_failed_system_error_is_not_reported_again(self):
		subject = self._track("ITEM-PUB-SYS")
		eid = emit_system_error("EVENT_INVALID", "boom", subject_doctype="Item", subject_name=subject)
		self._set_row(eid, attempts=9)
		self._set_settings(redis_town_url=REDIS_DOWN_URL)
		self.assertEqual(publish([eid]), 0)
		self.assertEqual(self._row(eid).status, "Failed")
		self.assertEqual(self._publish_failed_errors(eid), [])

	# --- sweep --------------------------------------------------------------------------------

	def test_sweep_picks_only_due_rows(self):
		due, backoff, fresh = self._emit(), self._emit(), self._emit()
		for eid in (due, backoff):
			frappe.db.sql(
				f"update `tab{OUTBOX}` set creation = %s where name = %s",
				(now_datetime() - timedelta(seconds=60), eid),
			)
		self._set_row(backoff, attempts=2, next_attempt_at=now_datetime() + timedelta(minutes=5))
		published = sweep()
		self.assertEqual(self._row(due).status, "Published")
		self.assertEqual(self._row(backoff).status, "Pending")
		self.assertEqual(self._row(fresh).status, "Pending")
		self.assertEqual(published, len(self._stream_ids()))
		self.assertIn(due, self._stream_ids())
		self.assertNotIn(backoff, self._stream_ids())
		self.assertNotIn(fresh, self._stream_ids())

	# --- purge --------------------------------------------------------------------------------

	def test_purge_deletes_only_old_published(self):
		self._set_settings(outbox_retention_days=400)
		old_published, new_published, old_pending = self._emit(), self._emit(), self._emit()
		self.assertEqual(publish([old_published, new_published]), 2)
		for eid in (old_published, old_pending):
			frappe.db.sql(
				f"update `tab{OUTBOX}` set creation = %s where name = %s",
				(add_days(now_datetime(), -401), eid),
			)
		self.assertGreaterEqual(purge(), 1)
		self.assertFalse(frappe.db.exists(OUTBOX, old_published))
		self.assertTrue(frappe.db.exists(OUTBOX, new_published))
		self.assertTrue(frappe.db.exists(OUTBOX, old_pending))

	def test_scheduler_runs_sweep_every_minute_and_purge_daily(self):
		hooks = frappe.get_hooks("scheduler_events", app_name="erp_town")
		self.assertIn("erp_town.events.publisher.sweep", hooks["cron"]["* * * * *"])
		self.assertIn("erp_town.events.publisher.purge", hooks["daily"])

	def _track(self, subject: str) -> str:
		subject = f"{subject}-{frappe.generate_hash(length=6)}"
		self.subjects.append(subject)
		return subject
