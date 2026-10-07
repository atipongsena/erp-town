# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

from frappe.model.document import Document


class TownNamingRule(Document):
	# begin: auto-generated types
	# This code is auto-generated. Do not modify anything in this block.

	from typing import TYPE_CHECKING

	if TYPE_CHECKING:
		from frappe.types import DF

		document_type: DF.Link
		parent: DF.Data
		parentfield: DF.Data
		parenttype: DF.Data
		prefix: DF.Data
	# end: auto-generated types
