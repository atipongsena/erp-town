# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

import frappe
from frappe.model.document import Document


class TownSubjectSeq(Document):
	# begin: auto-generated types
	# This code is auto-generated. Do not modify anything in this block.

	from typing import TYPE_CHECKING

	if TYPE_CHECKING:
		from frappe.types import DF

		last_seq: DF.Int
		subject_doctype: DF.Link
		subject_name: DF.Data
	# end: auto-generated types


def on_doctype_update():
	frappe.db.add_unique("Town Subject Seq", ["subject_doctype", "subject_name"])
