# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Per-subject event sequence (spec §3.1 `seq`, §4.2 step 4)."""

import frappe
from frappe.utils import cint


def next_seq(subject_doctype: str, subject_name: str) -> int:
	"""Allocate the next seq (1, 2, 3, ...) for a subject inside the caller's transaction.

	The `Town Subject Seq` row is locked with `SELECT ... FOR UPDATE` until the transaction ends, so
	concurrent emitters for the same subject serialize; a rollback restores `last_seq`. A missing row is
	inserted; if a concurrent transaction inserted it first, the unique (subject_doctype, subject_name)
	pair rejects ours and the row is re-selected.
	"""
	row = _lock_row(subject_doctype, subject_name)
	if row is None:
		savepoint = f"town_seq_{frappe.generate_hash(length=10)}"
		messages = len(getattr(frappe.local, "message_log", None) or [])
		frappe.db.savepoint(savepoint)
		try:
			frappe.get_doc(
				{
					"doctype": "Town Subject Seq",
					"subject_doctype": subject_doctype,
					"subject_name": subject_name,
					"last_seq": 1,
				}
			).insert(ignore_permissions=True)
		except frappe.UniqueValidationError, frappe.DuplicateEntryError:
			frappe.db.rollback(save_point=savepoint)
			if getattr(frappe.local, "message_log", None):  # the handled duplicate is not a user error
				del frappe.local.message_log[messages:]
			row = _lock_row(subject_doctype, subject_name)
			if row is None:
				raise
		else:
			frappe.db.release_savepoint(savepoint)
			return 1

	name, last_seq = row
	seq = cint(last_seq) + 1
	frappe.db.set_value("Town Subject Seq", name, "last_seq", seq)
	return seq


def _lock_row(subject_doctype: str, subject_name: str) -> tuple[str, int] | None:
	rows = frappe.db.sql(
		"""select name, last_seq from `tabTown Subject Seq`
		where subject_doctype = %s and subject_name = %s
		for update""",
		(subject_doctype, subject_name),
	)
	return (rows[0][0], rows[0][1]) if rows else None
