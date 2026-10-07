# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Publish `Town Event Outbox` rows to the town stream on `redis-town` (spec §4.3, §8).

- `publish(event_ids)`: background job enqueued by `emit_raw` after commit (and called by `sweep`).
  Locks the listed rows that are still `Pending` (`FOR UPDATE SKIP LOCKED`, so a job and the sweeper
  never publish the same row twice), `XADD`s each payload while the rows are locked, then records the
  result and commits. Backoff (`next_attempt_at`) is not checked here -- only `sweep` honours it.
  Unknown or non-`Pending` ids are a no-op (an after-commit job can name a row that a caller's
  savepoint rollback removed). If the commit fails after `XADD` the event may be published twice;
  consumers dedupe by `id` (spec §8).
- `sweep()`: every minute; publishes `Pending` rows older than 30 s whose backoff is due.
- `purge()`: daily; deletes `Published` rows older than `outbox_retention_days`.
"""

import time
from datetime import timedelta

import frappe
import redis
from frappe.utils import add_days, now_datetime

from erp_town.erp_town.doctype.town_settings.town_settings import get_town_settings
from erp_town.events.emit import PENDING, SYSTEM_ERROR, log_failure
from erp_town.events.redis_client import get_town_redis
from erp_town.events.system import emit_system_error

OUTBOX = "Town Event Outbox"
PUBLISHED = "Published"
FAILED = "Failed"
OUTBOX_PUBLISH_FAILED = "OUTBOX_PUBLISH_FAILED"
STREAM_FIELD = "event"
MAX_BACKOFF_SECONDS = 300
SWEEP_MIN_AGE_SECONDS = 30
SWEEP_LIMIT = 500
PURGE_BATCH = 1000
_MAX_ERROR_LENGTH = 1000
# the connection is gone: stop trying the remaining rows of this batch (each would wait for a timeout)
_CONNECTION_ERRORS = (redis.exceptions.ConnectionError, redis.exceptions.TimeoutError)


def publish(event_ids: list[str]) -> int:
	"""Publish the listed rows that are still `Pending`; return how many were published. Commits."""
	ids = list(dict.fromkeys(i for i in (event_ids or []) if i))
	if not ids:
		return 0
	settings = get_town_settings()
	rows = _lock_pending(ids)
	published = 0
	broken: Exception | None = None
	client = None
	if rows:
		try:
			client = get_town_redis(settings.redis_town_url)
		except Exception as e:  # noqa: BLE001 -- e.g. no/invalid URL: every row takes the backoff path
			broken = e
	for row in rows:
		if broken is not None:
			_record_failure(row, broken, settings.max_attempts)
			continue
		try:
			stream_id = client.xadd(
				settings.stream_name,
				{STREAM_FIELD: _payload_bytes(row)},
				minid=f"{_stream_cutoff_ms(settings.stream_retention_days)}-0",
				approximate=True,
			)
		except Exception as e:  # noqa: BLE001 -- a publish failure only ever means retry with backoff
			_record_failure(row, e, settings.max_attempts)
			if isinstance(e, _CONNECTION_ERRORS):
				broken = e
			continue
		frappe.db.set_value(
			OUTBOX,
			row.name,
			{
				"status": PUBLISHED,
				"stream_id": stream_id.decode() if isinstance(stream_id, bytes) else str(stream_id),
				"published_at": now_datetime(),
				"next_attempt_at": None,
				"last_error": None,
			},
		)
		published += 1
	frappe.db.commit()
	return published


def sweep() -> int:
	"""Scheduler (every minute): publish `Pending` rows older than 30 s whose backoff is due."""
	now = now_datetime()
	ids = frappe.db.sql_list(
		f"""
		select name from `tab{OUTBOX}`
		where status = %(pending)s
			and creation < %(created_before)s
			and (next_attempt_at is null or next_attempt_at <= %(now)s)
		order by creation, name
		limit {SWEEP_LIMIT}
		""",
		{"pending": PENDING, "created_before": now - timedelta(seconds=SWEEP_MIN_AGE_SECONDS), "now": now},
	)
	return publish(ids) if ids else 0


def purge() -> int:
	"""Scheduler (daily): delete `Published` rows older than `outbox_retention_days`; return the count."""
	cutoff = add_days(now_datetime(), -get_town_settings().outbox_retention_days)
	deleted = 0
	while names := frappe.db.sql_list(
		f"""
		select name from `tab{OUTBOX}`
		where status = %(published)s and creation < %(cutoff)s
		order by creation
		limit {PURGE_BATCH}
		""",
		{"published": PUBLISHED, "cutoff": cutoff},
	):
		frappe.db.delete(OUTBOX, {"name": ("in", names)})
		frappe.db.commit()
		deleted += len(names)
	return deleted


def _lock_pending(ids: list[str]) -> list[frappe._dict]:
	return frappe.db.sql(
		f"""
		select name, event_type, payload, attempts from `tab{OUTBOX}`
		where name in %(ids)s and status = %(pending)s
		order by creation, name
		for update skip locked
		""",
		{"ids": ids, "pending": PENDING},
		as_dict=True,
	)


def _payload_bytes(row) -> bytes:
	if not row.payload:
		raise ValueError(f"event {row.name} has no payload")
	return row.payload.encode("utf-8")


def _stream_cutoff_ms(retention_days: int) -> int:
	"""`MINID` for `XADD`: entries older than the stream retention are trimmed (approximately)."""
	return max(int(time.time() * 1000) - retention_days * 86_400_000, 0)


def _record_failure(row, error: Exception, max_attempts: int) -> None:
	"""Backoff bookkeeping: `attempts += 1`, retry after `min(2**attempts, 300)` s; `Failed` at the limit."""
	attempts = (row.attempts or 0) + 1
	message = (f"{type(error).__name__}: {error}".strip())[:_MAX_ERROR_LENGTH]
	failed = attempts >= max_attempts
	frappe.db.set_value(
		OUTBOX,
		row.name,
		{
			"attempts": attempts,
			"last_error": message,
			"status": FAILED if failed else PENDING,
			"next_attempt_at": None
			if failed
			else now_datetime() + timedelta(seconds=min(2**attempts, MAX_BACKOFF_SECONDS)),
		},
	)
	if failed:
		_report_failed(row, attempts, message)


def _report_failed(row, attempts: int, message: str) -> None:
	text = f"event {row.name} ({row.event_type}) not published after {attempts} attempts: {message}"
	if row.event_type == SYSTEM_ERROR:
		# reporting a failed system.error with another system.error would chain while redis-town is down
		log_failure(f"ERP Town: {text}")
		return
	emit_system_error(
		OUTBOX_PUBLISH_FAILED,
		text,
		subject_doctype=OUTBOX,
		subject_name=row.name,
		causation_id=row.name,
	)
