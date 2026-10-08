# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Sales Order credit-limit approval (spec §7): workflow `อนุมัติ SO เกินวงเงิน`.

- `th_requires_approval` (Check on Sales Order) is computed by the `validate` doc_event
  `set_requires_approval`: `grand_total + customer outstanding > credit_limit` when the customer has a
  credit limit for the company (ERPNext's own limit and outstanding: GL balance + open orders + unbilled
  delivery notes; the order being saved is not counted yet). No-op when the field does not exist.
- the workflow routes `Draft` by that flag: `ขออนุมัติ` → `รออนุมัติ` (needs a Sales Manager:
  `อนุมัติ` → `อนุมัติแล้ว` submits, `ปฏิเสธ` → `ปฏิเสธ` stays draft), or `ยืนยัน` → `อนุมัติแล้ว` directly.
- ERPNext blocks submitting an order over the limit unless the user holds Accounts Settings
  `credit_controller`; `ensure_credit_controller` makes that Sales Manager, so the approver's transition submits.

`ensure_approval_fields` must run before any other write of a seed: adding a column is DDL, which commits
the open transaction in MariaDB.
"""

import frappe
from frappe.custom.doctype.custom_field.custom_field import create_custom_fields
from frappe.utils import flt

APPROVAL_FIELD = "th_requires_approval"
WORKFLOW_STATE_FIELD = "workflow_state"
WORKFLOW_NAME = "อนุมัติ SO เกินวงเงิน"
CREDIT_CONTROLLER_ROLE = "Sales Manager"

DRAFT, PENDING, APPROVED, REJECTED = "Draft", "รออนุมัติ", "อนุมัติแล้ว", "ปฏิเสธ"
ACTION_REQUEST, ACTION_CONFIRM, ACTION_APPROVE, ACTION_REJECT = "ขออนุมัติ", "ยืนยัน", "อนุมัติ", "ปฏิเสธ"
STATE_STYLES = {DRAFT: "", PENDING: "Warning", APPROVED: "Success", REJECTED: "Danger"}

_FIELDS = {
	APPROVAL_FIELD: {
		"fieldname": APPROVAL_FIELD,
		"fieldtype": "Check",
		"label": "ต้องอนุมัติ (เกินวงเงิน)",
		"insert_after": "customer",
		"read_only": 1,
		"no_copy": 1,
		"print_hide": 1,
		"description": "ยอดสั่งซื้อรวมยอดค้างชำระเกินวงเงินเครดิตของลูกค้า",
	},
	# what Frappe's Workflow would add on save (as DDL inside the seed's transaction); added up front instead
	WORKFLOW_STATE_FIELD: {
		"fieldname": WORKFLOW_STATE_FIELD,
		"fieldtype": "Link",
		"label": "Workflow State",
		"options": "Workflow State",
		"hidden": 1,
		"allow_on_submit": 1,
		"no_copy": 1,
	},
}


def set_requires_approval(doc, method: str | None = None) -> None:
	"""Sales Order `validate`: set `th_requires_approval` from the customer's credit limit."""
	if not doc.meta.has_field(APPROVAL_FIELD):
		return
	from erpnext.selling.doctype.customer.customer import get_credit_limit, get_customer_outstanding

	limit = get_credit_limit(doc.customer, doc.company) if doc.customer and doc.company else 0
	if not limit:
		doc.set(APPROVAL_FIELD, 0)
		return
	outstanding = get_customer_outstanding(doc.customer, doc.company)
	total = flt(doc.get("base_grand_total") or doc.get("grand_total"))
	doc.set(APPROVAL_FIELD, int(total + flt(outstanding) > flt(limit)))


def ensure_approval_fields() -> None:
	"""Add the missing Sales Order fields (`th_requires_approval`, `workflow_state`). DDL: commits."""
	meta = frappe.get_meta("Sales Order")
	missing = [dict(field) for name, field in _FIELDS.items() if not meta.get_field(name)]
	if missing:
		create_custom_fields({"Sales Order": missing}, update=True)
		frappe.clear_cache(doctype="Sales Order")


def ensure_credit_controller() -> None:
	"""Let Sales Managers submit orders over the credit limit (ERPNext's own check)."""
	if frappe.db.get_single_value("Accounts Settings", "credit_controller") != CREDIT_CONTROLLER_ROLE:
		frappe.db.set_single_value("Accounts Settings", "credit_controller", CREDIT_CONTROLLER_ROLE)


def ensure_approval_workflow() -> str:
	"""Create (once) the workflow with its states and actions; returns its name."""
	for state, style in STATE_STYLES.items():
		if not frappe.db.exists("Workflow State", state):
			frappe.get_doc(
				{"doctype": "Workflow State", "workflow_state_name": state, "style": style}
			).insert(ignore_permissions=True)
	for action in (ACTION_REQUEST, ACTION_CONFIRM, ACTION_APPROVE, ACTION_REJECT):
		if not frappe.db.exists("Workflow Action Master", action):
			frappe.get_doc({"doctype": "Workflow Action Master", "workflow_action_name": action}).insert(
				ignore_permissions=True
			)
	if frappe.db.exists("Workflow", WORKFLOW_NAME):
		return WORKFLOW_NAME
	frappe.get_doc(
		{
			"doctype": "Workflow",
			"workflow_name": WORKFLOW_NAME,
			"document_type": "Sales Order",
			"workflow_state_field": WORKFLOW_STATE_FIELD,
			"is_active": 1,
			"send_email_alert": 0,
			"states": [
				{"state": DRAFT, "doc_status": "0", "allow_edit": "Sales User"},
				{"state": PENDING, "doc_status": "0", "allow_edit": "Sales Manager"},
				{"state": APPROVED, "doc_status": "1", "allow_edit": "Sales Manager"},
				{"state": REJECTED, "doc_status": "0", "allow_edit": "Sales Manager"},
			],
			"transitions": [
				{
					"state": DRAFT,
					"action": ACTION_REQUEST,
					"next_state": PENDING,
					"allowed": "Sales User",
					"condition": f"doc.{APPROVAL_FIELD}",
				},
				{
					"state": DRAFT,
					"action": ACTION_CONFIRM,
					"next_state": APPROVED,
					"allowed": "Sales User",
					"condition": f"not doc.{APPROVAL_FIELD}",
				},
				{
					"state": PENDING,
					"action": ACTION_APPROVE,
					"next_state": APPROVED,
					"allowed": "Sales Manager",
				},
				{
					"state": PENDING,
					"action": ACTION_REJECT,
					"next_state": REJECTED,
					"allowed": "Sales Manager",
				},
			],
		}
	).insert(ignore_permissions=True)
	frappe.cache.hdel("workflow", "Sales Order")
	return WORKFLOW_NAME
