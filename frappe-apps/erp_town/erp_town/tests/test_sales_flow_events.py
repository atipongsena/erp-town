import json
from unittest.mock import patch

import frappe
from frappe.tests import IntegrationTestCase

from erp_town.events.schema import validate_event
from erp_town.events.system import emit_system_error
from erp_town.tests.factories import (
	TEST_ABBR,
	TEST_COMPANY,
	TEST_WAREHOUSE,
	ensure_test_company,
	make_customer,
	make_dn_from_so,
	make_item,
	make_pe_for_si,
	make_si,
	make_si_from_dn,
	make_so,
	outbox_events_in_creation_order,
)

ERP_TYPES = (
	"erp.sales_order.submitted",
	"erp.delivery_note.submitted",
	"erp.sales_invoice.submitted",
	"erp.payment_entry.submitted",
)


def _events_for(doc) -> list[dict]:
	rows = frappe.get_all(
		"Town Event Outbox",
		filters={"subject_doctype": doc.doctype, "subject_name": doc.name},
		fields=["payload"],
		order_by="seq asc",
	)
	return [json.loads(r.payload) for r in rows]


def _erp_events_for(doc) -> list[dict]:
	return [e for e in _events_for(doc) if e["type"].startswith("erp.")]


def _outbox_rows(doc) -> list:
	return frappe.get_all(
		"Town Event Outbox",
		filters={"subject_doctype": doc.doctype, "subject_name": doc.name},
		fields=["event_type", "status", "seq", "last_error", "payload"],
		order_by="seq asc",
	)


def _amend(doc):
	amended = frappe.copy_doc(doc)
	amended.amended_from = doc.name
	amended.docstatus = 0
	amended.insert(ignore_permissions=True)
	amended.submit()
	return amended


class TestSalesFlowEvents(IntegrationTestCase):
	@classmethod
	def setUpClass(cls):
		super().setUpClass()
		ensure_test_company()
		frappe.db.commit()  # master data only; every test rolls its own documents back

	def setUp(self):
		# a clean outbox inside this test's transaction (rolled back in tearDown)
		frappe.db.delete("Town Event Outbox")
		frappe.db.delete("Town Subject Seq")
		self.enqueue = patch("frappe.enqueue").start()
		self.addCleanup(patch.stopall)
		self.customer = make_customer()
		self.item = make_item()

	def tearDown(self):
		frappe.db.rollback()
		frappe.local.request_cache.clear()
		if hasattr(frappe.local, "future_sle"):
			frappe.local.future_sle.clear()

	def test_full_flow_chain(self):
		so = make_so(self.customer, [self.item])
		dn = make_dn_from_so(so)
		si = make_si_from_dn(dn)
		pe = make_pe_for_si(si)
		evs = outbox_events_in_creation_order()
		self.assertEqual([e["type"] for e in evs][:4], list(ERP_TYPES))
		self.assertTrue(all(e["correlation_id"] == so.name for e in evs[:4]))
		self.assertIsNone(evs[0]["causation_id"])
		self.assertEqual(evs[1]["causation_id"], evs[0]["id"])
		self.assertEqual(evs[2]["causation_id"], evs[1]["id"])
		self.assertEqual(evs[3]["causation_id"], evs[2]["id"])
		self.assertEqual(evs[3]["data"]["against"], [si.name])
		for e in evs:
			self.assertNotIn("grand_total", e["data"])
			validate_event(e)

		self.assertEqual(
			[(e["subject"]["doctype"], e["subject"]["name"], e["seq"]) for e in evs[:4]],
			[("Sales Order", so.name, 1), ("Delivery Note", dn.name, 1)]
			+ [("Sales Invoice", si.name, 1), ("Payment Entry", pe.name, 1)],
		)
		self.assertEqual([e["source"] for e in evs[:4]], ["erp/selling", "erp/stock"] + ["erp/accounts"] * 2)
		self.assertTrue(all(e["company"] == TEST_ABBR for e in evs[:4]))
		self.assertTrue(all(e["visibility"] == {"level": "public", "roles": []} for e in evs[:4]))
		self.assertEqual(evs[0]["actor"], {"kind": "user", "id": "user.Administrator", "on_behalf_of": None})

		self.assertEqual(
			evs[0]["data"],
			{
				"status": so.status,
				"party": self.customer,
				"item_count": 1,
				"delivery_date": str(so.delivery_date),
			},
		)
		self.assertEqual(evs[0]["data"]["status"], "To Deliver and Bill")
		self.assertEqual(
			evs[1]["data"],
			{
				"status": dn.status,
				"party": self.customer,
				"item_count": 1,
				"warehouse": TEST_WAREHOUSE,
				"is_return": False,
				"return_against": None,
				"against": [so.name],
			},
		)
		self.assertEqual(
			evs[2]["data"],
			{
				"status": si.status,
				"party": self.customer,
				"due_date": str(si.due_date),
				"is_return": False,
				"return_against": None,
				"against": [dn.name, so.name],
			},
		)
		self.assertEqual(
			evs[3]["data"], {"party": self.customer, "mode_of_payment": None, "against": [si.name]}
		)
		statuses = frappe.get_all("Town Event Outbox", filters={"correlation_id": so.name}, pluck="status")
		self.assertEqual(set(statuses), {"Pending"})

	def test_returns_carry_is_return_and_return_against(self):
		"""Ruling AF: a return DN / credit-note SI says so, and names the original document."""
		from erpnext.accounts.doctype.sales_invoice.sales_invoice import make_sales_return as si_return
		from erpnext.stock.doctype.delivery_note.delivery_note import make_sales_return as dn_return

		so = make_so(self.customer, [self.item])
		dn = make_dn_from_so(so)
		si = make_si_from_dn(dn)

		credit_note = si_return(si.name)
		credit_note.insert(ignore_permissions=True)
		credit_note.submit()
		returned = dn_return(dn.name)
		returned.insert(ignore_permissions=True)
		returned.submit()

		si_data = _erp_events_for(credit_note)[0]["data"]
		self.assertIs(si_data["is_return"], True)
		self.assertEqual(si_data["return_against"], si.name)
		dn_data = _erp_events_for(returned)[0]["data"]
		self.assertIs(dn_data["is_return"], True)
		self.assertEqual(dn_data["return_against"], dn.name)
		for doc in (credit_note, returned):
			validate_event(_erp_events_for(doc)[0])

	def test_dn_against_two_sos_uses_first_and_lists_all(self):
		item2 = make_item()
		so1 = make_so(self.customer, [self.item])
		so2 = make_so(self.customer, [item2])
		so1_event, so2_event = _erp_events_for(so1)[0], _erp_events_for(so2)[0]
		self.assertEqual(so2_event["correlation_id"], so2.name)

		dn = make_dn_from_so(so1, so2)
		dn_event = _erp_events_for(dn)[0]
		self.assertEqual(dn_event["data"]["against"], [so1.name, so2.name])
		self.assertEqual(dn_event["data"]["item_count"], 2)
		self.assertEqual(dn_event["correlation_id"], so1.name)
		self.assertEqual(dn_event["causation_id"], so1_event["id"])

		# per item row: delivery note first, then its sales order; de-duplicated in order of appearance
		si = make_si_from_dn(dn)
		si_event = _erp_events_for(si)[0]
		self.assertEqual(si_event["data"]["against"], [dn.name, so1.name, so2.name])
		self.assertEqual(si_event["correlation_id"], so1.name)
		self.assertEqual(si_event["causation_id"], dn_event["id"])

	def test_si_without_upstream_is_its_own_root(self):
		si = make_si(self.customer, [self.item])
		events = _erp_events_for(si)
		self.assertEqual(len(events), 1)
		ev = events[0]
		self.assertEqual(ev["type"], "erp.sales_invoice.submitted")
		self.assertEqual(ev["correlation_id"], si.name)
		self.assertIsNone(ev["causation_id"])
		self.assertEqual(ev["data"]["against"], [])
		validate_event(ev)

	def test_cancel_emits_cancelled_with_next_seq(self):
		so = make_so(self.customer, [self.item])
		so.cancel()
		events = _events_for(so)
		self.assertEqual(
			[e["type"] for e in events], ["erp.sales_order.submitted", "erp.sales_order.cancelled"]
		)
		self.assertEqual([e["seq"] for e in events], [1, 2])
		self.assertEqual(events[1]["data"], {"status": "Cancelled"})
		self.assertEqual(events[1]["correlation_id"], so.name)
		self.assertEqual(events[1]["causation_id"], events[0]["id"])
		for e in events:
			validate_event(e)

	def test_cancelled_downstream_keeps_chain_correlation(self):
		so = make_so(self.customer, [self.item])
		dn = make_dn_from_so(so)
		si = make_si_from_dn(dn)
		pe = make_pe_for_si(si)
		for doc in (pe, si, dn):
			doc.reload()
			doc.cancel()
		for doc, prefix in ((dn, "erp.delivery_note"), (si, "erp.sales_invoice"), (pe, "erp.payment_entry")):
			with self.subTest(doctype=doc.doctype):
				submitted, cancelled = _events_for(doc)
				self.assertEqual(cancelled["type"], f"{prefix}.cancelled")
				self.assertEqual(cancelled["seq"], 2)
				self.assertEqual(cancelled["data"], {"status": "Cancelled"})
				self.assertEqual(cancelled["correlation_id"], so.name)
				self.assertEqual(cancelled["causation_id"], submitted["id"])
				validate_event(cancelled)

	def test_amended_doc_new_subject_same_correlation(self):
		si = make_si(self.customer, [self.item])
		si.cancel()
		cancel_event = _erp_events_for(si)[-1]
		amended = _amend(si)
		self.assertNotEqual(amended.name, si.name)
		events = _erp_events_for(amended)
		self.assertEqual(len(events), 1)
		ev = events[0]
		self.assertEqual(ev["type"], "erp.sales_invoice.submitted")
		self.assertEqual(ev["subject"], {"doctype": "Sales Invoice", "name": amended.name})
		self.assertEqual(ev["seq"], 1)
		self.assertEqual(ev["correlation_id"], si.name)  # the original chain root, not the new name
		self.assertEqual(ev["causation_id"], cancel_event["id"])
		validate_event(ev)

	def test_amended_doc_in_chain_keeps_root_and_upstream_cause(self):
		so = make_so(self.customer, [self.item])
		dn = make_dn_from_so(so)
		si = make_si_from_dn(dn)
		si.cancel()
		amended = _amend(si)
		ev = _erp_events_for(amended)[0]
		self.assertEqual(ev["seq"], 1)
		self.assertEqual(ev["correlation_id"], so.name)
		self.assertEqual(ev["causation_id"], _erp_events_for(dn)[0]["id"])
		self.assertEqual(ev["data"]["against"], [dn.name, so.name])

	def test_lineage_skips_system_errors_of_upstream(self):
		so = make_so(self.customer, [self.item])
		so_event = _erp_events_for(so)[0]
		emit_system_error(
			"ON_BEHALF_OF_REJECTED", "header ignored", subject_doctype="Sales Order", subject_name=so.name
		)
		self.assertEqual(_events_for(so)[-1]["type"], "system.error")
		dn_event = _erp_events_for(make_dn_from_so(so))[0]
		self.assertEqual(dn_event["correlation_id"], so.name)
		self.assertEqual(dn_event["causation_id"], so_event["id"])

	def test_mapper_exception_does_not_block_submit(self):
		from erp_town.events.mappers import REGISTRY

		with patch.object(REGISTRY["Sales Order"], "data", side_effect=RuntimeError("mapper bug")):
			so = make_so(self.customer, [self.item])
		self.assertEqual(frappe.db.get_value("Sales Order", so.name, "docstatus"), 1)
		rows = _outbox_rows(so)
		self.assertEqual(
			(rows[0].event_type, rows[0].status, rows[0].seq), ("erp.sales_order.submitted", "Invalid", 1)
		)
		self.assertIn("mapper bug", rows[0].last_error)
		errors = [json.loads(r.payload) for r in rows if r.event_type == "system.error"]
		self.assertEqual([e["data"]["code"] for e in errors], ["EVENT_INVALID"])
		self.assertIn("mapper bug", errors[0]["data"]["message"])
		self.assertEqual(errors[0]["causation_id"], json.loads(rows[0].payload)["id"])

	def test_invalid_upstream_keeps_root_without_causation(self):
		# spec §3.1: correlation_id is the flow root even when the root's own event is Invalid (never
		# published); causation must not point at an event consumers never receive
		from erp_town.events.mappers import REGISTRY

		with patch.object(REGISTRY["Sales Order"], "data", side_effect=RuntimeError("mapper bug")):
			so = make_so(self.customer, [self.item])
		self.assertEqual(_outbox_rows(so)[0].status, "Invalid")
		dn = make_dn_from_so(so)
		dn_event = _erp_events_for(dn)[0]
		self.assertEqual(dn_event["correlation_id"], so.name)
		self.assertIsNone(dn_event["causation_id"])
		self.assertEqual(dn_event["data"]["against"], [so.name])
		si_event = _erp_events_for(make_si_from_dn(dn))[0]
		self.assertEqual(si_event["correlation_id"], so.name)
		self.assertEqual(si_event["causation_id"], dn_event["id"])

	def test_cancel_after_invalid_submit_keeps_correlation_without_causation(self):
		from erp_town.events.mappers import REGISTRY

		si = make_si(self.customer, [self.item])
		with patch.object(REGISTRY["Sales Invoice"], "data", side_effect=RuntimeError("mapper bug")):
			si.cancel()
		amended = _amend(si)
		ev = _erp_events_for(amended)[0]
		self.assertEqual(ev["correlation_id"], si.name)
		self.assertIsNone(ev["causation_id"])

	def test_latest_event_reports_invalid_rows(self):
		from erp_town.events.lineage import latest_event
		from erp_town.events.mappers import REGISTRY

		with patch.object(REGISTRY["Sales Order"], "data", side_effect=RuntimeError("mapper bug")):
			so = make_so(self.customer, [self.item])
		invalid = latest_event("Sales Order", so.name)
		self.assertEqual(invalid["correlation_id"], so.name)
		self.assertTrue(invalid["invalid"])
		self.assertEqual(invalid["event_id"], json.loads(_outbox_rows(so)[0].payload)["id"])
		self.assertIsNone(latest_event("Sales Order", "NO-SUCH-SO"))

	def test_ensure_test_company_refuses_without_allow_tests(self):
		with patch.dict(frappe.conf, {"allow_tests": False}), self.assertRaises(RuntimeError):
			ensure_test_company()

	def test_hook_failure_does_not_block_submit(self):
		with patch("erp_town.events.doc_hooks.find_lineage", side_effect=RuntimeError("lineage bug")):
			so = make_so(self.customer, [self.item])
		self.assertEqual(frappe.db.get_value("Sales Order", so.name, "docstatus"), 1)
		events = _events_for(so)
		self.assertEqual([e["type"] for e in events], ["system.error"])
		self.assertEqual(events[0]["data"]["code"], "EVENT_INVALID")
		self.assertIn("lineage bug", events[0]["data"]["message"])
		self.assertEqual(events[0]["company"], TEST_ABBR)

	def test_envelope_company_is_the_company_code(self):
		"""Ruling AD: producers pass the Company record name; the envelope carries its abbr."""
		from erp_town.events.emit import emit_raw

		def emit(company):
			eid = emit_raw(
				"system.error",
				source="erp/town",
				subject_doctype="Town Event Outbox",
				subject_name=f"T-{frappe.generate_hash(length=6)}",
				company=company,
				data={"code": "TEST", "message": "company mapping"},
				actor={"kind": "system", "id": "system.town", "on_behalf_of": None},
				visibility={"level": "roles", "roles": ["System Manager"]},
			)
			return json.loads(frappe.db.get_value("Town Event Outbox", eid, "payload"))["company"]

		self.assertEqual(emit(TEST_COMPANY), TEST_ABBR)
		self.assertEqual(emit(TEST_ABBR), TEST_ABBR)  # already a code: kept
		self.assertIsNone(emit(None))
		with self.assertLogs(frappe.logger("erp_town"), level="WARNING"):
			self.assertEqual(emit("No Such Company Ltd"), "No Such Company Ltd")  # kept and logged

	def test_payment_entry_without_customer_party_emits_nothing(self):
		from erp_town.events.doc_hooks import on_doc_event

		doc = frappe._dict(
			doctype="Payment Entry",
			name="PE-SUPPLIER-T1",
			company=TEST_COMPANY,
			party_type="Supplier",
			party="SUP-001",
			references=[],
			docstatus=1,
			status="Submitted",
		)
		on_doc_event(doc, "on_submit")
		self.assertEqual(frappe.db.count("Town Event Outbox", {"subject_name": "PE-SUPPLIER-T1"}), 0)

	def test_unmapped_methods_and_doctypes_are_ignored(self):
		from erp_town.events.doc_hooks import on_doc_event

		so = make_so(self.customer, [self.item])
		before = frappe.db.count("Town Event Outbox")
		on_doc_event(so, "on_update_after_submit")
		on_doc_event(frappe._dict(doctype="ToDo", name="T-1", docstatus=1), "on_submit")
		self.assertEqual(frappe.db.count("Town Event Outbox"), before)
