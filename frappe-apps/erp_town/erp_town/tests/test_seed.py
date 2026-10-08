"""Seed + scenario (spec §7): idempotent dev-site seed, credit-limit approval workflow, scenario generator.

Runs on `test.localhost` only. `seed.run()` / `scenario.run()` never commit (the dev commands commit through
`bench execute`), so every test rolls its documents back. The only things that outlive a test are the two
Sales Order custom fields the seed adds first (DDL commits implicitly in MariaDB); the class removes them
again, puts back the other workflows and caches, and checks that nothing else was left behind.
"""

import io
from contextlib import redirect_stdout
from types import SimpleNamespace
from unittest.mock import MagicMock, patch

import frappe
from frappe.model.workflow import apply_workflow, get_transitions
from frappe.tests import IntegrationTestCase, UnitTestCase
from frappe.utils import add_days, get_datetime, getdate, now_datetime, nowdate

from erp_town.selling import credit_approval
from erp_town.setup import scenario, seed
from erp_town.thai.taxid import is_valid_tax_id

SO_FIELDS = (credit_approval.APPROVAL_FIELD, credit_approval.WORKFLOW_STATE_FIELD)
SETTINGS_SNAPSHOT = (
	("System Settings", ("language", "time_zone", "country", "currency", "setup_complete")),
	("Accounts Settings", ("credit_controller",)),
	("Global Defaults", ("default_company", "default_currency", "country")),
	("Selling Settings", ("selling_price_list", "customer_group", "territory")),
	("Stock Settings", ("default_warehouse", "stock_uom")),
)


def _settings_snapshot() -> dict:
	return {
		(doctype, field): frappe.db.get_single_value(doctype, field)
		for doctype, fields in SETTINGS_SNAPSHOT
		for field in fields
	}


def _custom_field_name(fieldname: str) -> str:
	return f"Sales Order-{fieldname}"


def _seed_quietly() -> tuple[dict, str]:
	out = io.StringIO()
	with redirect_stdout(out):
		counts = seed.run()
	return counts, out.getvalue()


def _clear_caches() -> None:
	from frappe.core.doctype.system_settings.system_settings import clear_system_settings_cache

	frappe.cache.hdel("workflow", "Sales Order")
	clear_system_settings_cache()
	frappe.clear_cache()
	frappe.cache.delete_value("fiscal_years")
	frappe.local.request_cache.clear()
	frappe.local.new_doc_templates.clear()
	if hasattr(frappe.local, "future_sle"):
		frappe.local.future_sle.clear()


class TestTaxIdGenerator(UnitTestCase):
	def test_generator_is_deterministic_and_checksum_valid(self):
		ids = [seed.make_tax_id(n) for n in range(1, 11)]
		self.assertEqual(ids, [seed.make_tax_id(n) for n in range(1, 11)])
		self.assertEqual(len(set(ids)), 10)
		for tax_id in ids:
			self.assertTrue(is_valid_tax_id(tax_id), tax_id)
		self.assertTrue(is_valid_tax_id(seed.COMPANY_TAX_ID))
		self.assertNotIn(seed.COMPANY_TAX_ID, ids)


class TestScenarioPlan(UnitTestCase):
	def test_plan_depends_only_on_the_seed(self):
		import random

		def plan(rng_seed):
			rng = random.Random(rng_seed)
			return [scenario.plan_day(rng) for _ in range(3)]

		self.assertEqual(plan(7), plan(7))
		for day in plan(7):
			self.assertTrue(scenario.MIN_ORDERS_PER_DAY <= len(day) <= scenario.MAX_ORDERS_PER_DAY)
			for order in day:
				self.assertIn(order.customer, seed.CUSTOMER_CODES)
				self.assertTrue(order.lines)

	def test_first_order_of_a_day_is_an_unpaid_cash_sale(self):
		import random

		rng = random.Random(11)
		for _ in range(10):
			first = scenario.plan_day(rng)[0]
			self.assertNotIn(first.customer, seed.LIMITED_CUSTOMERS)
			self.assertEqual(
				(first.deliver, first.invoice, first.pay, first.due_days), (True, True, False, 0)
			)

	def test_limited_customers_get_orders_now_and_then(self):
		import random

		rng = random.Random(3)
		customers = [o.customer for _ in range(20) for o in scenario.plan_day(rng)]
		limited = [c for c in customers if c in seed.LIMITED_CUSTOMERS]
		self.assertTrue(0 < len(limited) < len(customers) / 2)


class TestDayClock(UnitTestCase):
	"""Every posting time of today is at or before `now`, and steps/orders keep their order."""

	DAY = "2026-10-08"
	STEPS = (0, scenario.RESTOCK_OFFSET, scenario.DELIVERY_OFFSET, scenario.INVOICE_OFFSET)

	def _times(self, clock):
		return [clock.at(n, offset) for n in range(scenario.MAX_ORDERS_PER_DAY) for offset in self.STEPS]

	def test_today_never_after_now_and_ordered(self):
		for now in ("00:00:00", "00:05:00", "00:59:00", "01:14:00", "01:30:00", "09:30:00", "15:00:00"):
			now_dt = get_datetime(f"{self.DAY} {now}")
			times = self._times(scenario.DayClock(self.DAY, self.DAY, now=now_dt))
			self.assertLessEqual(max(times), now_dt, now)
			self.assertGreaterEqual(min(times), get_datetime(f"{self.DAY} 00:00:00"), now)
			self.assertEqual(times, sorted(times), now)
			if now != "00:00:00":
				self.assertEqual(len(set(times)), len(times), now)  # strictly increasing

	def test_full_spacing_from_nine_when_the_day_fits(self):
		clock = scenario.DayClock(self.DAY, self.DAY, now=get_datetime(f"{self.DAY} 15:00:00"))
		self.assertEqual(clock.at(0), get_datetime(f"{self.DAY} 09:00:00"))
		self.assertEqual(clock.at(1, scenario.INVOICE_OFFSET), get_datetime(f"{self.DAY} 09:14:00"))

	def test_past_days_start_at_nine_whatever_the_time(self):
		clock = scenario.DayClock("2026-10-07", self.DAY, now=get_datetime(f"{self.DAY} 00:05:00"))
		self.assertEqual(clock.at(0), get_datetime("2026-10-07 09:00:00"))
		self.assertEqual(clock.at(2, scenario.DELIVERY_OFFSET), get_datetime("2026-10-07 09:22:00"))


class TestRequiresApprovalHook(UnitTestCase):
	def test_no_op_without_the_custom_field(self):
		doc = SimpleNamespace(meta=MagicMock(), customer="X", company="Y")
		doc.meta.has_field.return_value = False
		credit_approval.set_requires_approval(doc)
		self.assertFalse(hasattr(doc, credit_approval.APPROVAL_FIELD))

	def test_hook_is_registered(self):
		hooks = frappe.get_hooks("doc_events")["Sales Order"]["validate"]
		self.assertIn("erp_town.selling.credit_approval.set_requires_approval", hooks)


class TestSeedSiteGuard(UnitTestCase):
	"""seed.run() writes demo users with a known password: dev/test sites only unless forced."""

	def test_refuses_without_developer_mode_or_allow_tests(self):
		with (
			patch.dict(frappe.conf, {"developer_mode": 0, "allow_tests": False}),
			patch.object(seed, "ensure_approval_fields") as first_step,
			self.assertRaises(frappe.ValidationError) as ctx,
		):
			seed.run()
		first_step.assert_not_called()
		self.assertIn("pilot.bootstrap", str(ctx.exception))

	def test_runs_on_dev_test_or_forced(self):
		for conf, kwargs in (
			({"developer_mode": 1, "allow_tests": False}, {}),
			({"developer_mode": 0, "allow_tests": True}, {}),
			({"developer_mode": 0, "allow_tests": False}, {"force": True}),
		):
			with (
				patch.dict(frappe.conf, conf),
				patch.object(seed, "ensure_approval_fields", side_effect=RuntimeError("past the guard")),
				self.assertRaisesRegex(RuntimeError, "past the guard", msg=repr((conf, kwargs))),
			):
				seed.run(**kwargs)


class TestSeed(IntegrationTestCase):
	@classmethod
	def setUpClass(cls):
		super().setUpClass()
		meta = frappe.get_meta("Sales Order")
		cls.preexisting_fields = {
			f for f in SO_FIELDS if frappe.db.exists("Custom Field", _custom_field_name(f))
		}
		cls.had_meta_fields = {f for f in SO_FIELDS if meta.get_field(f)}
		cls.other_active = frappe.get_all(
			"Workflow", filters={"document_type": "Sales Order", "is_active": 1}, pluck="name"
		)
		cls.settings_before = _settings_snapshot()
		cls.lang_before = frappe.local.lang
		cls.users_before = set(frappe.get_all("User", pluck="name"))

	@classmethod
	def tearDownClass(cls):
		frappe.db.rollback()
		for fieldname in SO_FIELDS:
			name = _custom_field_name(fieldname)
			if fieldname not in cls.preexisting_fields and frappe.db.exists("Custom Field", name):
				frappe.delete_doc("Custom Field", name, ignore_permissions=True, force=True)
		if frappe.db.exists("Workflow", credit_approval.WORKFLOW_NAME):
			frappe.delete_doc("Workflow", credit_approval.WORKFLOW_NAME, ignore_permissions=True, force=True)
		for name in cls.other_active:
			frappe.db.set_value("Workflow", name, "is_active", 1)
		frappe.db.commit()
		frappe.local.lang = cls.lang_before
		_clear_caches()
		try:
			cls._assert_nothing_leaked()
		finally:
			super().tearDownClass()

	@classmethod
	def _assert_nothing_leaked(cls):
		meta = frappe.get_meta("Sales Order")
		leaks = []
		if frappe.db.exists("Company", seed.COMPANY):
			leaks.append("company")
		if frappe.db.exists("Workflow", credit_approval.WORKFLOW_NAME):
			leaks.append("workflow")
		if set(frappe.get_all("User", pluck="name")) != cls.users_before:
			leaks.append("users")
		if frappe.db.exists("Item", seed.ITEM_CODES[0]) or frappe.db.exists(
			"Customer", seed.CUSTOMER_CODES[0]
		):
			leaks.append("items/customers")
		if {f for f in SO_FIELDS if meta.get_field(f)} != cls.had_meta_fields:
			leaks.append("Sales Order custom fields")
		if _settings_snapshot() != cls.settings_before:
			leaks.append("settings")
		if frappe.local.lang != cls.lang_before:
			leaks.append("frappe.local.lang")
		assert not leaks, f"test_seed leaked: {', '.join(leaks)}"

	def setUp(self):
		self.enqueue = patch("frappe.enqueue").start()
		self.addCleanup(patch.stopall)

	def tearDown(self):
		frappe.db.rollback()
		frappe.local.lang = self.lang_before
		_clear_caches()

	# --- seed --------------------------------------------------------------------------------------------

	def test_run_twice_gives_identical_counts_and_no_duplicates(self):
		first, _ = _seed_quietly()
		second, _ = _seed_quietly()
		self.assertEqual(first, second)
		self.assertEqual(first["Company"], 1)
		self.assertEqual(first["Item"], 20)
		self.assertEqual(first["Item Price"], 20)
		self.assertEqual(first["Item Reorder"], 20)
		self.assertEqual(first["Stock Reconciliation"], 1)
		self.assertEqual(first["Customer"], 10)
		self.assertEqual(first["Address"], 11)  # one per customer + the company's
		self.assertEqual(first["User"], 6)
		self.assertEqual(first["Town Agent Identity"], 2)
		self.assertEqual(first["Workflow"], 1)

		self.assertEqual(seed.ITEM_CODES, [f"ITEM-{n:03d}" for n in range(1, 21)])
		self.assertEqual(seed.CUSTOMER_CODES, [f"CUST-{n:03d}" for n in range(1, 11)])
		company = frappe.get_doc("Company", seed.COMPANY)
		self.assertEqual(company.abbr, "ACME-TH")
		self.assertEqual(company.default_currency, "THB")
		warehouse = seed.warehouse()
		self.assertEqual(frappe.db.get_value("Warehouse", warehouse, "warehouse_name"), "คลังหลัก")
		today = nowdate()
		self.assertTrue(
			frappe.db.exists(
				"Fiscal Year", {"year_start_date": ["<=", today], "year_end_date": [">=", today]}
			)
		)
		for code in seed.ITEM_CODES:
			item = frappe.get_doc("Item", code)
			self.assertEqual((item.stock_uom, item.is_stock_item), ("ชิ้น", 1))
			self.assertEqual([r.warehouse for r in item.reorder_levels], [warehouse])
			self.assertGreater(
				frappe.db.get_value("Bin", {"item_code": code, "warehouse": warehouse}, "actual_qty"), 0
			)

		settings = frappe.get_single("System Settings")
		self.assertEqual(
			(settings.language, settings.time_zone, settings.country, settings.currency),
			("th", "Asia/Bangkok", "Thailand", "THB"),
		)
		self.assertEqual(
			frappe.db.get_single_value("Accounts Settings", "credit_controller"), "Sales Manager"
		)
		self.assertTrue(frappe.db.exists("Address Template", "Thailand"))

	def test_users_roles_and_agents(self):
		_seed_quietly()
		expected = {
			"sales@acme-th.test": "Sales User",
			"stock@acme-th.test": "Stock User",
			"accounts@acme-th.test": "Accounts User",
			"approver@acme-th.test": "Sales Manager",
			"agent.sales_assistant@acme-th.test": "Sales User",
			"agent.ar_collector@acme-th.test": "Accounts User",
		}
		for user, role in expected.items():
			self.assertIn(role, frappe.get_roles(user), user)
			self.assertEqual(frappe.db.get_value("User", user, "user_type"), "System User")
		self.assertEqual(
			dict(frappe.get_all("Town Agent Identity", fields=["agent_key", "user"], as_list=True)),
			{
				"agent.sales_assistant": "agent.sales_assistant@acme-th.test",
				"agent.ar_collector": "agent.ar_collector@acme-th.test",
			},
		)

	def test_seed_user_password_from_env(self):
		from frappe.utils.password import check_password

		with patch.dict("os.environ", {"SEED_USER_PASSWORD": "env-pass-456"}):
			_seed_quietly()
		self.assertEqual(
			check_password("sales@acme-th.test", "env-pass-456", delete_tracker_cache=False),
			"sales@acme-th.test",
		)

	def test_agent_api_keys_generated_once_and_printed_once(self):
		from frappe.utils.password import get_decrypted_password

		_, first_out = _seed_quietly()
		keys = {}
		for agent in seed.AGENTS:
			api_key = frappe.db.get_value("User", agent.user, "api_key")
			api_secret = get_decrypted_password("User", agent.user, "api_secret")
			self.assertTrue(api_key and api_secret)
			self.assertIn(f"{agent.env_prefix}_API_KEY={api_key}", first_out)
			self.assertIn(f"{agent.env_prefix}_API_SECRET={api_secret}", first_out)
			keys[agent.user] = (api_key, api_secret)

		counts, second_out = _seed_quietly()
		self.assertNotIn("_API_SECRET=", second_out)
		self.assertNotIn("_API_KEY=", second_out)
		for user, pair in keys.items():
			again = (
				frappe.db.get_value("User", user, "api_key"),
				get_decrypted_password("User", user, "api_secret"),
			)
			self.assertEqual(again, pair)
		self.assertNotIn(keys[seed.AGENTS[0].user][1], repr(counts))

	def test_customer_tax_ids_pass_checksum(self):
		_seed_quietly()
		tax_ids = frappe.get_all("Customer", filters={"name": ["in", seed.CUSTOMER_CODES]}, pluck="tax_id")
		self.assertEqual(len(tax_ids), 10)
		self.assertEqual(len(set(tax_ids)), 10)
		for tax_id in tax_ids:
			self.assertTrue(is_valid_tax_id(tax_id), tax_id)
		limits = {
			code: frappe.db.get_value(
				"Customer Credit Limit", {"parent": code, "company": seed.COMPANY}, "credit_limit"
			)
			for code in seed.CUSTOMER_CODES
		}
		self.assertEqual({c for c, v in limits.items() if v}, {"CUST-009", "CUST-010"})
		self.assertEqual(limits["CUST-009"], 5000)
		self.assertEqual(limits["CUST-010"], 5000)

	# --- approval workflow -------------------------------------------------------------------------------

	def _so(self, customer: str, qty: int, rate: float):
		return frappe.get_doc(
			{
				"doctype": "Sales Order",
				"company": seed.COMPANY,
				"customer": customer,
				"transaction_date": nowdate(),
				"delivery_date": add_days(nowdate(), 3),
				"selling_price_list": seed.PRICE_LIST,
				"items": [
					{"item_code": seed.ITEM_CODES[3], "qty": qty, "rate": rate, "warehouse": seed.warehouse()}
				],
			}
		).insert()

	def test_over_limit_so_waits_for_the_approver_then_submits(self):
		_seed_quietly()
		workflow = frappe.get_doc("Workflow", credit_approval.WORKFLOW_NAME)
		self.assertEqual(workflow.document_type, "Sales Order")
		self.assertTrue(workflow.is_active)
		self.assertEqual(
			[(s.state, int(s.doc_status)) for s in workflow.states],
			[("Draft", 0), ("รออนุมัติ", 0), ("อนุมัติแล้ว", 1), ("ปฏิเสธ", 0)],
		)

		with self.set_user("sales@acme-th.test"):
			so = self._so("CUST-009", qty=3, rate=2000)  # 6,000 > 5,000
			self.assertEqual(so.th_requires_approval, 1)
			self.assertEqual(so.workflow_state, "Draft")
			self.assertEqual([t.action for t in get_transitions(so)], [credit_approval.ACTION_REQUEST])
			apply_workflow(so.as_dict(), credit_approval.ACTION_REQUEST)
			so.reload()
			self.assertEqual(so.workflow_state, "รออนุมัติ")
			self.assertEqual(so.docstatus, 0)
			self.assertEqual(get_transitions(so), [])  # a sales user cannot approve

		with self.set_user("approver@acme-th.test"):
			self.assertNotIn("System Manager", frappe.get_roles())
			apply_workflow(frappe.get_doc("Sales Order", so.name).as_dict(), credit_approval.ACTION_APPROVE)
		so.reload()
		self.assertEqual((so.workflow_state, so.docstatus), ("อนุมัติแล้ว", 1))

		events = frappe.get_all(
			"Town Event Outbox",
			filters={"subject_doctype": "Sales Order", "subject_name": so.name},
			order_by="seq asc",
			pluck="event_type",
		)
		self.assertEqual(
			events,
			["workflow.approval.requested", "erp.sales_order.submitted", "workflow.approval.decided"],
		)

	def test_without_the_credit_controller_role_erpnext_blocks_the_approval(self):
		_seed_quietly()
		frappe.db.set_single_value("Accounts Settings", "credit_controller", "")
		with self.set_user("sales@acme-th.test"):
			so = self._so("CUST-009", qty=3, rate=2000)
			apply_workflow(so.as_dict(), credit_approval.ACTION_REQUEST)
		with self.set_user("approver@acme-th.test"), self.assertRaises(frappe.ValidationError):
			apply_workflow(frappe.get_doc("Sales Order", so.name).as_dict(), credit_approval.ACTION_APPROVE)

	def test_within_limit_or_unlimited_so_is_confirmed_directly(self):
		_seed_quietly()
		with self.set_user("sales@acme-th.test"):
			for customer, qty, rate in (("CUST-009", 1, 1000), ("CUST-001", 10, 2000)):
				so = self._so(customer, qty=qty, rate=rate)
				self.assertEqual(so.th_requires_approval, 0, customer)
				self.assertEqual([t.action for t in get_transitions(so)], [credit_approval.ACTION_CONFIRM])
				apply_workflow(so.as_dict(), credit_approval.ACTION_CONFIRM)
				so.reload()
				self.assertEqual((so.workflow_state, so.docstatus), ("อนุมัติแล้ว", 1), customer)

	def test_rejected_so_stays_draft(self):
		_seed_quietly()
		with self.set_user("sales@acme-th.test"):
			so = self._so("CUST-010", qty=5, rate=2000)
			apply_workflow(so.as_dict(), credit_approval.ACTION_REQUEST)
		with self.set_user("approver@acme-th.test"):
			apply_workflow(frappe.get_doc("Sales Order", so.name).as_dict(), credit_approval.ACTION_REJECT)
		so.reload()
		self.assertEqual((so.workflow_state, so.docstatus), ("ปฏิเสธ", 0))

	def test_outstanding_counts_towards_the_limit(self):
		_seed_quietly()
		with self.set_user("sales@acme-th.test"):
			first = self._so("CUST-009", qty=2, rate=2000)  # 4,000: within the limit
			apply_workflow(first.as_dict(), credit_approval.ACTION_CONFIRM)
			second = self._so("CUST-009", qty=1, rate=1500)  # 4,000 open + 1,500 > 5,000
			self.assertEqual(second.th_requires_approval, 1)

	# --- scenario ----------------------------------------------------------------------------------------

	def test_scenario_same_seed_same_number_of_orders(self):
		_seed_quietly()
		first = scenario.run(days=1, rng_seed=1)
		second = scenario.run(days=1, rng_seed=1)
		self.assertEqual(first["Sales Order"], second["Sales Order"])
		self.assertTrue(scenario.MIN_ORDERS_PER_DAY <= first["Sales Order"] <= scenario.MAX_ORDERS_PER_DAY)

	def test_scenario_days_end_today_with_overdue_invoices(self):
		_seed_quietly()
		days = 3
		counts = scenario.run(days=days, rng_seed=42)
		today = getdate(nowdate())
		first_day = add_days(today, -(days - 1))

		self.assertGreaterEqual(counts["Sales Order"], days * scenario.MIN_ORDERS_PER_DAY)
		self.assertLessEqual(counts["Delivery Note"], counts["Sales Order"])
		self.assertLessEqual(counts["Sales Invoice"], counts["Delivery Note"])
		self.assertLessEqual(counts["Payment Entry"], counts["Sales Invoice"])
		self.assertGreater(counts["Payment Entry"], 0)
		self.assertGreater(counts["approvals_requested"], 0)

		orders = frappe.get_all("Sales Order", filters={"company": seed.COMPANY}, pluck="transaction_date")
		self.assertEqual(len(orders), counts["Sales Order"])
		self.assertEqual({getdate(d) for d in orders}, {getdate(add_days(first_day, i)) for i in range(days)})
		invoices = frappe.get_all(
			"Sales Invoice",
			filters={"company": seed.COMPANY, "docstatus": 1},
			fields=["posting_date", "due_date", "outstanding_amount"],
		)
		self.assertEqual(len(invoices), counts["Sales Invoice"])
		self.assertTrue(all(getdate(i.posting_date) <= today for i in invoices))
		latest = frappe.db.sql(
			"""select max(timestamp(posting_date, posting_time)) from `tabStock Ledger Entry`
			where warehouse = %s and is_cancelled = 0""",
			seed.warehouse(),
		)[0][0]
		self.assertLessEqual(latest, now_datetime())  # nothing posted in the future
		overdue = [i for i in invoices if i.outstanding_amount > 0 and getdate(i.due_date) < today]
		self.assertTrue(overdue, "some unpaid invoices must be overdue")
		self.assertTrue(
			frappe.db.exists("Sales Order", {"company": seed.COMPANY, "workflow_state": "อนุมัติแล้ว"})
		)

	def test_scenario_just_after_midnight_posts_nothing_in_the_future(self):
		_seed_quietly()
		now = get_datetime(f"{nowdate()} 00:05:00")  # 00:05 Bangkok: the day's 09:00 slots do not fit
		with patch.object(scenario, "now_datetime", return_value=now):
			counts = scenario.run(days=1, rng_seed=1)
		self.assertGreater(counts["Sales Invoice"], 0)
		latest = frappe.db.sql(
			"""select max(timestamp(posting_date, posting_time)) from `tabStock Ledger Entry`
			where warehouse = %s and is_cancelled = 0""",
			seed.warehouse(),
		)[0][0]
		self.assertLessEqual(latest, now)
		# per document chain: order date <= delivery <= invoice <= now, payment on or after the invoice
		chains = frappe.db.sql(
			"""select so.transaction_date, timestamp(dn.posting_date, dn.posting_time) as dn_at,
				timestamp(si.posting_date, si.posting_time) as si_at,
				(select min(pe.posting_date) from `tabPayment Entry Reference` per
				 join `tabPayment Entry` pe on pe.name = per.parent and pe.docstatus = 1
				 where per.reference_name = si.name) as paid_on
			from `tabSales Invoice` si
			join `tabSales Invoice Item` sii on sii.parent = si.name and sii.idx = 1
			join `tabDelivery Note` dn on dn.name = sii.delivery_note
			join `tabDelivery Note Item` dni on dni.parent = dn.name and dni.idx = 1
			join `tabSales Order` so on so.name = dni.against_sales_order
			where si.company = %s and si.docstatus = 1""",
			seed.COMPANY,
			as_dict=True,
		)
		self.assertEqual(len(chains), counts["Sales Invoice"])
		for c in chains:
			self.assertLessEqual(get_datetime(c.transaction_date), c.dn_at)
			self.assertLess(c.dn_at, c.si_at)
			self.assertLessEqual(c.si_at, now)
			if c.paid_on:
				self.assertGreaterEqual(getdate(c.paid_on), getdate(c.si_at))

	def test_scenario_bulk_commits_every_batch(self):
		_seed_quietly()
		with patch.object(scenario, "BULK_COMMIT_EVERY", 2), patch.object(frappe.db, "commit") as commit:
			counts = scenario.run(days=0, bulk=5, rng_seed=5)
		self.assertEqual(counts["Sales Invoice"], 5)
		self.assertEqual(counts["Sales Order"], 0)
		self.assertEqual(commit.call_count, 2)  # after invoices 2 and 4; the rest commits with bench execute

	def test_scenario_restocks_before_a_delivery_stock_cannot_cover(self):
		_seed_quietly()
		sim = scenario._Simulation(getdate(nowdate()))
		code, wh = "ITEM-018", seed.warehouse()  # opening stock 12
		with scenario._as(seed.STOCK_USER):
			sim._restock_if_short(code, 5, now_datetime())
			self.assertEqual(sim.counts["Stock Entry"], 0)
			sim._restock_if_short(code, 20, now_datetime())
		self.assertEqual(sim.counts["Stock Entry"], 1)
		self.assertGreaterEqual(
			frappe.db.get_value("Bin", {"item_code": code, "warehouse": wh}, "actual_qty"), 20
		)

	def test_scenario_requires_the_seed(self):
		with self.assertRaises(frappe.ValidationError):
			scenario.run(days=1)
