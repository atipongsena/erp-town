# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

from frappe.model.document import Document


class TownAgentIdentity(Document):
	# begin: auto-generated types
	# This code is auto-generated. Do not modify anything in this block.

	from typing import TYPE_CHECKING

	if TYPE_CHECKING:
		from frappe.types import DF

		agent_key: DF.Data
		enabled: DF.Check
		user: DF.Link
	# end: auto-generated types
