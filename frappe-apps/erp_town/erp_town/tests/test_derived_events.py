"""Derived town events (spec §3.3, §4.2, §4.3): workflow approval, stock below reorder, invoice overdue."""

import json
from unittest.mock import patch

import frappe
from frappe.model.workflow import apply_workflow
from frappe.tests import IntegrationTestCase, UnitTestCase
from frappe.utils import add_days, get_datetime, getdate, nowdate

from erp_town.erp_town.doctype.town_settings.town_settings import get_town_settings
from erp_town.events import jobs, stock, workflow
from erp_town.events.schema import SCHEMA_DIR, validate_event
from erp_town.tests.factories import (
	TEST_ABBR,
	TEST_WAREHOUSE,
	ensure_test_company,
	make_customer,
	make_dn_from_so,
	make_item,
	make_pe_for_si,
	make_si,
	make_si_from_dn,
	make_so,
)

WORKFLOW = "ERP Town Test SO Approval"
WORKFLOW_STATE_FIELD = "workflow_state"
CUSTOM_FIELD = f"Sales Order-{WORKFLOW_STATE_FIELD}"
DRAFT, PENDING, APPROVED, REJECTED = "Draft", "รออนุมัติ", "อนุมัติแล้ว", "ปฏิเสธ"
SECOND_LEVEL = "รออนุมัติขั้นสอง"  # an approval state only while a test adds it to approval_states


def _events_for(doctype: str, name: str) -> list[dict]:
	rows = frappe.get_all(
		"Town Event Outbox",
		filters={"subject_doctype": doctype, "subject_name": name},
		fields=["payload"],
		order_by="seq asc",
	)
	return [json.loads(r.payload) for r in rows]


def _at_hour(hour: int):
	"""Naive system-timezone wall clock, as `frappe.utils.now_datetime()` returns it."""
	return get_datetime(f"2026-10-08 {hour:02d}:00:00")


def _of_type(events: list[dict], event_type: str) -> list[dict]:
	return [e for e in events if e["type"] == event_type]


class _OutboxTestCase(IntegrationTestCase):
	"""Every test works in its own transaction on a clean outbox; documents are rolled back."""

	@classmethod
	def setUpClass(cls):
		super().setUpClass()
		ensure_test_company()
		frappe.db.commit()  # master data only

	def setUp(self):
		frappe.db.delete("Town Event Outbox")
		frappe.db.delete("Town Subject Seq")
		self.enqueue = patch("frappe.enqueue").start()
		self.addCleanup(patch.stopall)
		self.customer = make_customer()

	def tearDown(self):
		frappe.db.rollback()
		frappe.local.request_cache.clear()
		if hasattr(frappe.local, "future_sle"):
			frappe.local.future_sle.clear()


class _WorkflowTestCase(_OutboxTestCase):
	"""A Sales Order workflow exists only while the class runs (created and removed with commits)."""

	@classmethod
	def setUpClass(cls):
		super().setUpClass()
		cls.created_custom_field = not frappe.get_meta("Sales Order").get_field(WORKFLOW_STATE_FIELD)
		cls.other_active = frappe.get_all(
			"Workflow", filters={"document_type": "Sales Order", "is_active": 1}, pluck="name"
		)
		try:
			_create_test_workflow()
			frappe.db.commit()
		except Exception:
			_remove_test_workflow(cls.created_custom_field, cls.other_active)
			raise

	@classmethod
	def tearDownClass(cls):
		_remove_test_workflow(cls.created_custom_field, cls.other_active)
		super().tearDownClass()


class TestApprovalEvents(_WorkflowTestCase):
	def setUp(self):
		super().setUp()
		self.item = make_item()

	def test_requested_then_approved(self):
		so = make_so(self.customer, [self.item], submit=False)
		self.assertEqual(so.get(WORKFLOW_STATE_FIELD), DRAFT)

		apply_workflow(so.as_dict(), "Review")
		events = _events_for("Sales Order", so.name)
		self.assertEqual([e["type"] for e in events], ["workflow.approval.requested"])
		requested = events[0]
		self.assertEqual(
			requested["data"], {"from_state": DRAFT, "to_state": PENDING, "approver_role": "Sales Manager"}
		)
		self.assertEqual(requested["source"], "erp/workflow")
		self.assertEqual(requested["company"], TEST_ABBR)
		self.assertEqual(requested["correlation_id"], so.name)
		self.assertIsNone(requested["causation_id"])
		self.assertEqual(
			requested["actor"], {"kind": "user", "id": "user.Administrator", "on_behalf_of": None}
		)

		approved = apply_workflow(frappe.get_doc("Sales Order", so.name).as_dict(), "Approve")
		self.assertEqual(approved.docstatus, 1)
		events = _events_for("Sales Order", so.name)
		self.assertEqual(
			[e["type"] for e in events],
			["workflow.approval.requested", "erp.sales_order.submitted", "workflow.approval.decided"],
		)
		submitted, decided = events[1], events[2]
		self.assertEqual(
			decided["data"], {"from_state": PENDING, "to_state": APPROVED, "decision": "approved"}
		)
		self.assertEqual(decided["causation_id"], submitted["id"])
		self.assertEqual(decided["correlation_id"], so.name)
		self.assertEqual(decided["company"], TEST_ABBR)
		self.assertEqual([e["seq"] for e in events], [1, 2, 3])
		for e in events:
			validate_event(e)

	def test_requested_then_rejected(self):
		so = make_so(self.customer, [self.item], submit=False)
		apply_workflow(so.as_dict(), "Review")
		rejected = apply_workflow(frappe.get_doc("Sales Order", so.name).as_dict(), "Reject")
		self.assertEqual(rejected.docstatus, 0)

		events = _events_for("Sales Order", so.name)
		self.assertEqual(
			[e["type"] for e in events], ["workflow.approval.requested", "workflow.approval.decided"]
		)
		requested, decided = events
		self.assertEqual(
			decided["data"], {"from_state": PENDING, "to_state": REJECTED, "decision": "rejected"}
		)
		self.assertEqual(decided["causation_id"], requested["id"])
		self.assertEqual(decided["correlation_id"], so.name)
		validate_event(decided)

	def test_multi_level_approval_decides_then_requests(self):
		real_settings = workflow.get_town_settings

		def two_levels():
			settings = real_settings()
			settings.approval_states = [*settings.approval_states, SECOND_LEVEL]
			return settings

		so = make_so(self.customer, [self.item], submit=False)
		with patch("erp_town.events.workflow.get_town_settings", side_effect=two_levels):
			apply_workflow(so.as_dict(), "Review")
			apply_workflow(frappe.get_doc("Sales Order", so.name).as_dict(), "Escalate")
			events = _events_for("Sales Order", so.name)
			self.assertEqual(
				[e["type"] for e in events],
				["workflow.approval.requested", "workflow.approval.decided", "workflow.approval.requested"],
			)
			first, decided, second = events
			self.assertEqual(first["data"]["approver_role"], "Sales Manager")
			self.assertEqual(
				decided["data"], {"from_state": PENDING, "to_state": SECOND_LEVEL, "decision": "approved"}
			)
			self.assertEqual(
				second["data"],
				{"from_state": PENDING, "to_state": SECOND_LEVEL, "approver_role": "Accounts Manager"},
			)
			self.assertEqual(decided["causation_id"], first["id"])
			self.assertEqual(second["causation_id"], decided["id"])

			apply_workflow(frappe.get_doc("Sales Order", so.name).as_dict(), "Approve")
		events = _events_for("Sales Order", so.name)
		self.assertEqual(
			[e["type"] for e in events[3:]], ["erp.sales_order.submitted", "workflow.approval.decided"]
		)
		self.assertEqual(
			events[4]["data"], {"from_state": SECOND_LEVEL, "to_state": APPROVED, "decision": "approved"}
		)
		for e in events:
			validate_event(e)

	def test_repeated_on_change_for_one_save_emits_once(self):
		# db_set() during post-save methods re-runs on_change with the same doc_before_save
		so = make_so(self.customer, [self.item], submit=False)
		so.set(WORKFLOW_STATE_FIELD, PENDING)
		so.save()
		so.db_set("po_no", "touched")
		self.assertEqual(len(_of_type(_events_for("Sales Order", so.name), "workflow.approval.requested")), 1)

	def test_transitions_outside_approval_states_emit_nothing(self):
		so = make_so(self.customer, [self.item])  # direct submit: Draft -> อนุมัติแล้ว
		self.assertEqual(so.get(WORKFLOW_STATE_FIELD), APPROVED)
		types = [e["type"] for e in _events_for("Sales Order", so.name)]
		self.assertEqual(types, ["erp.sales_order.submitted"])

	def test_failure_does_not_block_the_transition(self):
		so = make_so(self.customer, [self.item], submit=False)
		with patch("erp_town.events.workflow.get_town_settings", side_effect=RuntimeError("settings bug")):
			apply_workflow(so.as_dict(), "Review")
		self.assertEqual(frappe.db.get_value("Sales Order", so.name, WORKFLOW_STATE_FIELD), PENDING)
		events = _events_for("Sales Order", so.name)
		self.assertEqual([e["type"] for e in events], ["system.error"])
		self.assertEqual(events[0]["data"]["code"], "EVENT_INVALID")
		self.assertIn("settings bug", events[0]["data"]["message"])


FIXTURES_DIR = SCHEMA_DIR / "fixtures" / "valid"


def _fixture(event_type: str) -> dict:
	return json.loads((FIXTURES_DIR / f"{event_type}.json").read_text(encoding="utf-8"))


class TestProducersMatchFixtures(_WorkflowTestCase):
	"""Every event type built by its real producer has the shape of its golden fixture (Ruling AD)."""

	def test_real_producer_events_match_valid_fixtures(self):
		item = make_item(reorder_level=3, opening_qty=4)
		so = make_so(self.customer, [{"item_code": item, "qty": 2}], submit=False)
		apply_workflow(so.as_dict(), "Review")  # workflow.approval.requested
		so = apply_workflow(frappe.get_doc("Sales Order", so.name).as_dict(), "Approve")  # submitted, decided
		so = frappe.get_doc("Sales Order", so.name)
		dn = make_dn_from_so(so)  # erp.delivery_note.submitted + erp.stock.below_reorder (4 -> 2 < 3)
		si = make_si_from_dn(dn)
		jobs.emit_overdue(today=add_days(getdate(si.due_date), 3))  # erp.sales_invoice.overdue
		pe = make_pe_for_si(si)
		for doc in (pe, si, dn, so):  # cancellations, downstream first
			frappe.get_doc(doc.doctype, doc.name).cancel()
		with patch("erp_town.events.doc_hooks.find_lineage", side_effect=RuntimeError("lineage bug")):
			broken = make_so(self.customer, [make_item()])  # system.error EVENT_INVALID

		subjects = [("Item", item)] + [(d.doctype, d.name) for d in (so, dn, si, pe, broken)]
		produced = {}
		for doctype, name in subjects:
			for event in _events_for(doctype, name):
				produced.setdefault(event["type"], event)

		fixture_types = sorted(p.stem for p in FIXTURES_DIR.glob("*.json"))
		self.assertEqual(sorted(produced), fixture_types)
		company_names = set(frappe.get_all("Company", pluck="name"))
		for event_type, event in produced.items():
			with self.subTest(event_type):
				fixture = _fixture(event_type)
				validate_event(event)
				self.assertEqual(set(event), set(fixture))
				self.assertEqual(set(event["data"]), set(fixture["data"]))
				self.assertEqual(event["visibility"], fixture["visibility"])
				self.assertEqual(event["source"], fixture["source"])
				self.assertEqual(
					event["subject"]["doctype"] == "Item", fixture["subject"]["doctype"] == "Item"
				)
				if fixture["actor"]["kind"] == "system":
					self.assertEqual(event["actor"], fixture["actor"])
				else:
					self.assertNotEqual(event["actor"]["kind"], "system")
				if fixture["company"] is None:
					self.assertIsNone(event["company"])
				else:
					self.assertEqual(event["company"], TEST_ABBR)  # the code, like "ACME-TH"
					self.assertNotIn(event["company"], company_names)


class TestBelowReorderEvents(_OutboxTestCase):
	def _deliver(self, item: str, *qtys: float):
		"""One Delivery Note against one Sales Order per qty (rows aggregate per item/warehouse)."""
		sos = [make_so(self.customer, [{"item_code": item, "qty": qty}]) for qty in qtys]
		return make_dn_from_so(*sos)

	def test_crossing_emits_one_event_caused_by_the_dn(self):
		item = make_item(reorder_level=3, opening_qty=4)
		seen_at_hook = []
		original = stock.on_dn_submit

		def spy(doc, method=None):
			seen_at_hook.append(
				frappe.db.get_value("Bin", {"item_code": item, "warehouse": TEST_WAREHOUSE}, "actual_qty")
			)
			return original(doc, method)

		with patch("erp_town.events.stock.on_dn_submit", side_effect=spy):
			dn = self._deliver(item, 1, 1)  # two rows, 4 -> 2: one crossing, not one per row

		self.assertEqual(seen_at_hook, [2])  # Bin already reflects this DN when the hook runs
		dn_event = _of_type(_events_for("Delivery Note", dn.name), "erp.delivery_note.submitted")[0]
		events = _events_for("Item", item)
		self.assertEqual([e["type"] for e in events], ["erp.stock.below_reorder"])
		event = events[0]
		self.assertEqual(event["data"], {"warehouse": TEST_WAREHOUSE, "fill_ratio": 0.67})
		self.assertEqual(event["causation_id"], dn_event["id"])
		self.assertEqual(event["correlation_id"], dn_event["correlation_id"])
		self.assertEqual(event["company"], TEST_ABBR)
		self.assertEqual(event["source"], "erp/stock")
		self.assertEqual(event["subject"], {"doctype": "Item", "name": item})
		self.assertEqual(event["actor"]["kind"], "system")
		validate_event(event)

	def test_events_are_emitted_in_item_code_order(self):
		"""One lock order for the Item seq rows, whatever the DN row order (no seq-lock deadlocks)."""
		items = sorted(make_item(reorder_level=3, opening_qty=4) for _ in range(3))
		sos = [make_so(self.customer, [{"item_code": code, "qty": 2}]) for code in reversed(items)]
		emitted = []
		real_emit = stock.emit_raw

		def spy(*args, **kwargs):
			emitted.append(kwargs["subject_name"])
			return real_emit(*args, **kwargs)

		with patch("erp_town.events.stock.emit_raw", side_effect=spy):
			make_dn_from_so(*sos)
		self.assertEqual(emitted, items)

	def test_landing_exactly_on_level_is_not_below(self):
		item = make_item(reorder_level=3, opening_qty=4)
		self._deliver(item, 1)  # 4 -> 3
		self.assertEqual(_events_for("Item", item), [])

	def test_starting_exactly_on_level_crosses(self):
		item = make_item(reorder_level=3, opening_qty=3)
		self._deliver(item, 3)  # 3 -> 0
		events = _events_for("Item", item)
		self.assertEqual(len(events), 1)
		self.assertEqual(events[0]["data"]["fill_ratio"], 0)

	def test_staying_above_emits_nothing(self):
		item = make_item(reorder_level=10, opening_qty=100)
		self._deliver(item, 5)
		self.assertEqual(_events_for("Item", item), [])

	def test_already_below_emits_nothing(self):
		item = make_item(reorder_level=10, opening_qty=5)
		self._deliver(item, 1)
		self.assertEqual(_events_for("Item", item), [])

	def test_item_without_reorder_level_emits_nothing(self):
		item = make_item(opening_qty=5)
		self._deliver(item, 4)
		self.assertEqual(_events_for("Item", item), [])

	def test_failure_does_not_block_submit(self):
		item = make_item(reorder_level=3, opening_qty=4)
		with patch("erp_town.events.stock._reorder_levels", side_effect=RuntimeError("reorder bug")):
			dn = self._deliver(item, 2)
		self.assertEqual(dn.docstatus, 1)
		self.assertEqual(_events_for("Item", item), [])
		errors = _of_type(_events_for("Delivery Note", dn.name), "system.error")
		self.assertEqual(len(errors), 1)
		self.assertEqual(errors[0]["data"]["code"], "EVENT_INVALID")
		self.assertIn("reorder bug", errors[0]["data"]["message"])


class TestFillRatio(UnitTestCase):
	def test_rounded_to_two_decimals(self):
		self.assertEqual(stock.fill_ratio(2, 3), 0.67)
		self.assertEqual(stock.fill_ratio(1, 3), 0.33)
		self.assertEqual(stock.fill_ratio(7, 10), 0.7)
		self.assertEqual(stock.fill_ratio(0.004, 1), 0)

	def test_floored_at_zero(self):
		self.assertEqual(stock.fill_ratio(-4, 10), 0)
		self.assertNotEqual(str(stock.fill_ratio(-0.001, 1)), "-0.0")


class TestOverdueEvents(_OutboxTestCase):
	def setUp(self):
		super().setUp()
		self.item = make_item()
		self.today = getdate(nowdate())

	def test_unpaid_overdue_invoices_emit_once_per_day(self):
		yesterday = make_si(self.customer, [self.item], posting_date=add_days(self.today, -1))
		three_days = make_si(self.customer, [self.item], posting_date=add_days(self.today, -3))
		paid = make_si(self.customer, [self.item], posting_date=add_days(self.today, -1))
		make_pe_for_si(paid)
		due_today = make_si(self.customer, [self.item])

		count = jobs.emit_overdue()
		overdue = frappe.get_all(
			"Town Event Outbox", filters={"event_type": "erp.sales_invoice.overdue"}, pluck="subject_name"
		)
		self.assertEqual(count, len(overdue))
		self.assertEqual(overdue.count(yesterday.name), 1)
		self.assertEqual(overdue.count(three_days.name), 1)
		self.assertNotIn(paid.name, overdue)
		self.assertNotIn(due_today.name, overdue)

		self.assertEqual(jobs.emit_overdue(), 0)  # same day: deduplicated from the outbox

		submitted, event = _events_for("Sales Invoice", three_days.name)
		self.assertEqual(event["type"], "erp.sales_invoice.overdue")
		self.assertEqual(event["data"], {"due_date": str(add_days(self.today, -3)), "days_overdue": 3})
		self.assertEqual(event["actor"], {"kind": "system", "id": "system.scheduler", "on_behalf_of": None})
		self.assertEqual(event["causation_id"], submitted["id"])
		self.assertEqual(event["correlation_id"], submitted["correlation_id"])
		self.assertEqual(event["company"], TEST_ABBR)
		self.assertEqual(event["source"], "erp/accounts")
		validate_event(event)
		self.assertEqual(_events_for("Sales Invoice", yesterday.name)[-1]["data"]["days_overdue"], 1)

	def test_failing_invoice_does_not_stop_the_job(self):
		broken = make_si(self.customer, [self.item], posting_date=add_days(self.today, -2))
		fine = make_si(self.customer, [self.item], posting_date=add_days(self.today, -1))
		real_emitted_on = jobs._emitted_on

		def emitted_on(invoice, day):
			if invoice == broken.name:
				raise RuntimeError("dedupe bug")
			return real_emitted_on(invoice, day)

		with patch("erp_town.events.jobs._emitted_on", side_effect=emitted_on):
			count = jobs.emit_overdue()
		self.assertGreaterEqual(count, 1)
		self.assertEqual(
			len(_of_type(_events_for("Sales Invoice", fine.name), "erp.sales_invoice.overdue")), 1
		)
		broken_events = _events_for("Sales Invoice", broken.name)
		self.assertEqual(_of_type(broken_events, "erp.sales_invoice.overdue"), [])
		errors = _of_type(broken_events, "system.error")
		self.assertEqual(len(errors), 1)
		self.assertEqual(errors[0]["data"]["code"], "EVENT_INVALID")
		self.assertIn("dedupe bug", errors[0]["data"]["message"])

	def test_scheduled_run_commits_per_small_batch(self):
		"""Seq locks are held for one batch, not the whole run (the scheduled job passes commit_every)."""
		for days in (1, 2, 3):
			make_si(self.customer, [self.item], posting_date=add_days(self.today, -days))
		overdue_at_commit = []

		def commit():
			overdue_at_commit.append(frappe.db.count("Town Event Outbox", {"event_type": jobs.OVERDUE}))

		with patch.object(frappe.db, "commit", side_effect=commit):
			self.assertEqual(jobs.emit_overdue(commit_every=2), 3)
		self.assertEqual(overdue_at_commit, [2, 3])
		self.assertEqual(jobs.COMMIT_EVERY, 20)

	def test_direct_call_does_not_commit(self):
		make_si(self.customer, [self.item], posting_date=add_days(self.today, -1))
		with patch.object(frappe.db, "commit") as commit:
			jobs.emit_overdue()
		commit.assert_not_called()

	def test_next_day_emits_again(self):
		si = make_si(self.customer, [self.item], posting_date=add_days(self.today, -1))
		jobs.emit_overdue()
		frappe.db.sql(
			"update `tabTown Event Outbox` set creation = %s where subject_name = %s",
			(add_days(self.today, -1), si.name),
		)
		jobs.emit_overdue()
		self.assertEqual(len(_of_type(_events_for("Sales Invoice", si.name), "erp.sales_invoice.overdue")), 2)

	def test_system_actor_flag_is_reset(self):
		frappe.flags.town_system_actor = None
		jobs.emit_overdue()
		self.assertFalse(frappe.flags.town_system_actor)
		with (
			patch("erp_town.events.jobs._overdue_invoices", side_effect=RuntimeError("query bug")),
			self.assertRaises(RuntimeError),
		):
			jobs.emit_overdue()
		self.assertFalse(frappe.flags.town_system_actor)

	def test_hourly_job_runs_only_at_the_configured_hour(self):
		hour = get_town_settings().overdue_job_hour
		with (
			patch("erp_town.events.jobs.emit_overdue", return_value=0) as emit_overdue,
			patch("erp_town.events.jobs.now_datetime", return_value=_at_hour((hour + 1) % 24)),
		):
			jobs.emit_overdue_hourly()
			emit_overdue.assert_not_called()
		with (
			patch("erp_town.events.jobs.emit_overdue", return_value=0) as emit_overdue,
			patch("erp_town.events.jobs.now_datetime", return_value=_at_hour(hour)),
		):
			jobs.emit_overdue_hourly()
			emit_overdue.assert_called_once_with(commit_every=jobs.COMMIT_EVERY)

	def test_hooks_are_registered(self):
		hooks = frappe.get_hooks("scheduler_events", app_name="erp_town")
		self.assertIn("erp_town.events.jobs.emit_overdue_hourly", hooks["cron"]["0 * * * *"])
		self.assertIn("erp_town.events.publisher.sweep", hooks["cron"]["* * * * *"])
		doc_events = frappe.get_hooks("doc_events", app_name="erp_town")
		self.assertEqual(
			doc_events["Delivery Note"]["on_submit"],
			["erp_town.events.doc_hooks.on_doc_event", "erp_town.events.stock.on_dn_submit"],
		)
		self.assertIn("erp_town.events.workflow.on_change", doc_events["Sales Order"]["on_change"])


def _create_test_workflow() -> None:
	for state in (DRAFT, PENDING, APPROVED, REJECTED, SECOND_LEVEL):
		if not frappe.db.exists("Workflow State", state):
			frappe.get_doc({"doctype": "Workflow State", "workflow_state_name": state}).insert(
				ignore_permissions=True
			)
	for action in ("Review", "Approve", "Reject", "Escalate"):
		if not frappe.db.exists("Workflow Action Master", action):
			frappe.get_doc({"doctype": "Workflow Action Master", "workflow_action_name": action}).insert(
				ignore_permissions=True
			)
	if frappe.db.exists("Workflow", WORKFLOW):
		frappe.delete_doc("Workflow", WORKFLOW, ignore_permissions=True, force=True)
	frappe.get_doc(
		{
			"doctype": "Workflow",
			"workflow_name": WORKFLOW,
			"document_type": "Sales Order",
			"workflow_state_field": WORKFLOW_STATE_FIELD,
			"is_active": 1,
			"send_email_alert": 0,
			"states": [
				{"state": DRAFT, "doc_status": "0", "allow_edit": "Sales User"},
				{"state": PENDING, "doc_status": "0", "allow_edit": "Sales Manager"},
				{"state": APPROVED, "doc_status": "1", "allow_edit": "Sales Manager"},
				{"state": REJECTED, "doc_status": "0", "allow_edit": "Sales Manager"},
				{"state": SECOND_LEVEL, "doc_status": "0", "allow_edit": "Accounts Manager"},
			],
			"transitions": [
				{"state": DRAFT, "action": "Review", "next_state": PENDING, "allowed": "Sales User"},
				{"state": PENDING, "action": "Approve", "next_state": APPROVED, "allowed": "Sales Manager"},
				{"state": PENDING, "action": "Reject", "next_state": REJECTED, "allowed": "Sales Manager"},
				{
					"state": PENDING,
					"action": "Escalate",
					"next_state": SECOND_LEVEL,
					"allowed": "Sales Manager",
				},
				# out of the second level the first transition in table order names the approver
				{
					"state": SECOND_LEVEL,
					"action": "Approve",
					"next_state": APPROVED,
					"allowed": "Accounts Manager",
				},
				{
					"state": SECOND_LEVEL,
					"action": "Reject",
					"next_state": REJECTED,
					"allowed": "Sales Manager",
				},
			],
		}
	).insert(ignore_permissions=True)
	frappe.cache.hdel("workflow", "Sales Order")


def _remove_test_workflow(created_custom_field: bool, other_active: list[str]) -> None:
	"""Drop the test workflow (and its state field if this class created it); reactivate the others."""
	frappe.db.rollback()
	if frappe.db.exists("Workflow", WORKFLOW):
		frappe.delete_doc("Workflow", WORKFLOW, ignore_permissions=True, force=True)
	for name in other_active:
		frappe.db.set_value("Workflow", name, "is_active", 1)
	if created_custom_field and frappe.db.exists("Custom Field", CUSTOM_FIELD):
		frappe.delete_doc("Custom Field", CUSTOM_FIELD, ignore_permissions=True, force=True)
	frappe.db.commit()
	frappe.cache.hdel("workflow", "Sales Order")
	frappe.clear_cache(doctype="Sales Order")
