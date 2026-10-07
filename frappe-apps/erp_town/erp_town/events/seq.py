# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Per-subject event sequence (spec §3.1 `seq`, §4.2 step 4)."""

import frappe
from frappe.utils import now


def next_seq(subject_doctype: str, subject_name: str) -> int:
	"""Allocate the next seq (1, 2, 3, ...) for a subject inside the caller's transaction.

	Insert-first upsert on the unique (subject_doctype, subject_name) pair: a new subject is inserted with
	`last_seq = 1`, an existing one is incremented. Either way our write X-locks the row until the
	transaction ends, so emitters for the same subject serialize, and a rollback (of the transaction or of
	the caller's savepoint) restores `last_seq`.

	Not `SELECT ... FOR UPDATE` then `INSERT`: at REPEATABLE-READ a locking read that misses takes a gap
	lock, and two transactions creating different new subjects (monotonic document names share the
	trailing gap) then deadlock on each other's gap lock. Plain inserts of different keys do not conflict.
	"""
	timestamp = now()
	user = frappe.session.user
	frappe.db.sql(
		"""insert into `tabTown Subject Seq`
			(name, subject_doctype, subject_name, last_seq, creation, modified, owner, modified_by, docstatus, idx)
		values (%s, %s, %s, 1, %s, %s, %s, %s, 0, 0)
		on duplicate key update last_seq = last_seq + 1, modified = values(modified)""",
		(frappe.generate_hash(), subject_doctype, subject_name, timestamp, timestamp, user, user),
	)
	# The row is X-locked by the write above and our own change is its newest version, so this read
	# returns the value we just wrote (no LAST_INSERT_ID(): the table has no auto-increment column).
	return frappe.db.sql(
		"""select last_seq from `tabTown Subject Seq`
		where subject_doctype = %s and subject_name = %s""",
		(subject_doctype, subject_name),
	)[0][0]
