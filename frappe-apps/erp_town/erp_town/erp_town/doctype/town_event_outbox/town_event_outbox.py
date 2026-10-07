# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

import frappe
from frappe.model.document import Document


class TownEventOutbox(Document):
	# begin: auto-generated types
	# This code is auto-generated. Do not modify anything in this block.

	from typing import TYPE_CHECKING

	if TYPE_CHECKING:
		from typing import Literal

		from frappe.types import DF

		attempts: DF.Int
		correlation_id: DF.Data | None
		event_id: DF.Data
		event_type: DF.Data | None
		last_error: DF.LongText | None
		next_attempt_at: DF.Datetime | None
		payload: DF.JSON | None
		published_at: DF.Datetime | None
		seq: DF.Int
		status: Literal["Pending", "Published", "Failed", "Invalid"]
		stream_id: DF.Data | None
		subject_doctype: DF.Link | None
		subject_name: DF.Data | None
	# end: auto-generated types


def on_doctype_update():
	frappe.db.add_unique("Town Event Outbox", ["subject_doctype", "subject_name", "seq"])
	frappe.db.add_index("Town Event Outbox", ["status", "next_attempt_at"])
