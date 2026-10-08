# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Scenario generator (spec §7): `pnpm erp:scenario -- --days N [--bulk N]`.

Simulates `days` days of the sales flow on the seeded company, ending today (day i = today − (days−1−i)),
with backdated postings (`set_posting_time`). Each day 3–8 Sales Orders (random customers and items) are
entered by `sales@`; ~80% are delivered by `stock@`, ~70% of deliveries invoiced and ~60% of invoices paid
by `accounts@`. Orders for the credit-limited customers (CUST-009/010) sometimes cross the limit and wait
in `รออนุมัติ` for `approver@`, who approves, rejects or leaves them pending. Invoices fall due 0–30 days
after posting and are paid up to a few days later; each day's first order is a late payer on cash terms,
so every simulated day before today leaves an overdue invoice. `bulk > 0` additionally posts `bulk`
standalone invoices today, committing every `BULK_COMMIT_EVERY`.

Every random choice comes from `random.Random(rng_seed)` and is drawn up front for each day (`plan_day`),
independent of what the database holds, so the same seed always plans the same orders. Never commits on
its own except in bulk mode; `bench execute` commits at the end.
"""

import random
from collections import Counter
from contextlib import contextmanager
from dataclasses import dataclass, replace

import frappe
from frappe import _
from frappe.model.workflow import apply_workflow
from frappe.utils import add_days, cint, flt, get_datetime, getdate, now_datetime, nowdate

from erp_town.selling import credit_approval as ca
from erp_town.setup import seed
from erp_town.setup.masters import ensure_fiscal_years

MIN_ORDERS_PER_DAY, MAX_ORDERS_PER_DAY = 3, 8
DELIVER_RATE, INVOICE_RATE, PAY_RATE = 0.8, 0.7, 0.6
LIMITED_CUSTOMER_RATE = 0.15  # share of orders that go to a credit-limited customer
APPROVE_RATE, REJECT_RATE = 0.7, 0.15  # the rest of the approval requests stay pending
DUE_DAYS = (0, 1, 3, 7, 14, 30)
PAY_DELAY_DAYS = (0, 0, 1, 2, 5)
MAX_DAYS = 366
BULK_COMMIT_EVERY = 50
DAY_START = "09:00:00"
COUNT_KEYS = (
	"Sales Order",
	"Delivery Note",
	"Sales Invoice",
	"Payment Entry",
	"Stock Entry",
	"approvals_requested",
	"approved",
	"rejected",
	"pending_approval",
)
REGULAR_CUSTOMERS = tuple(c for c in seed.CUSTOMER_CODES if c not in seed.LIMITED_CUSTOMERS)
ITEM_INFO = {
	code: {"rate": rate, "cost": cost, "reorder_qty": rq} for code, _n, rate, cost, _o, _l, rq in seed.ITEMS
}


@dataclass(frozen=True)
class OrderPlan:
	customer: str
	lines: tuple[tuple[str, int], ...]  # (item_code, qty)
	delivery_days: int
	approval: str  # "approve" | "reject" | "pending" (used only when the order needs approval)
	deliver: bool
	invoice: bool
	pay: bool
	due_days: int
	pay_delay_days: int
	mode_of_payment: str


def plan_day(rng: random.Random) -> list[OrderPlan]:
	"""One simulated day of orders; consumes the same number of draws whatever the outcome.

	The day's first order is a late payer: a regular customer on cash terms (due the day it is invoiced)
	who does not pay, so every simulated day before today leaves at least one overdue invoice.
	"""
	orders = []
	for n in range(rng.randint(MIN_ORDERS_PER_DAY, MAX_ORDERS_PER_DAY)):
		limited = rng.random() < LIMITED_CUSTOMER_RATE
		customer = rng.choice(seed.LIMITED_CUSTOMERS if limited else REGULAR_CUSTOMERS)
		items = rng.sample(seed.ITEM_CODES, rng.randint(1, 3))
		lines = tuple((code, rng.randint(1, 10)) for code in items)
		roll = rng.random()
		approval = (
			"approve" if roll < APPROVE_RATE else "reject" if roll < APPROVE_RATE + REJECT_RATE else "pending"
		)
		order = OrderPlan(
			customer=customer,
			lines=lines,
			delivery_days=rng.randint(0, 3),
			approval=approval,
			deliver=rng.random() < DELIVER_RATE,
			invoice=rng.random() < INVOICE_RATE,
			pay=rng.random() < PAY_RATE,
			due_days=rng.choice(DUE_DAYS),
			pay_delay_days=rng.choice(PAY_DELAY_DAYS),
			mode_of_payment=rng.choice(seed.MODES_OF_PAYMENT)[0],
		)
		if n == 0:
			late_payer = REGULAR_CUSTOMERS[seed.CUSTOMER_CODES.index(customer) % len(REGULAR_CUSTOMERS)]
			order = replace(order, customer=late_payer, deliver=True, invoice=True, pay=False, due_days=0)
		orders.append(order)
	return orders


def run(days: int = 7, bulk: int = 0, rng_seed: int = 42) -> dict:
	"""Simulate `days` days ending today (+ `bulk` invoices); returns counts per doctype / approval outcome."""
	days, bulk, rng_seed = cint(days), cint(bulk), cint(rng_seed)
	if not 0 <= days <= MAX_DAYS:
		frappe.throw(_("days must be between 0 and {0}").format(MAX_DAYS))
	if bulk < 0:
		frappe.throw(_("bulk must not be negative"))
	if not frappe.db.exists("Company", seed.COMPANY) or not seed.warehouse():
		frappe.throw(_("Company {0} not found: run `pnpm erp:seed` first").format(seed.COMPANY))

	today = getdate(nowdate())
	first_day = getdate(add_days(today, -(days - 1))) if days else today
	ensure_fiscal_years(years=range(first_day.year, today.year + 1))
	sim = _Simulation(today)
	rng = random.Random(rng_seed)
	for i in range(days):
		day = getdate(add_days(today, -(days - 1 - i)))
		start = _day_start(day, today)
		for n, order in enumerate(plan_day(rng)):
			sim.order(order, day, start + _minutes(n * 10))
	if bulk:
		sim.bulk_invoices(rng, bulk)
	return {key: sim.counts[key] for key in COUNT_KEYS}


@contextmanager
def _as(user: str):
	previous = frappe.session.user
	frappe.set_user(user)
	try:
		yield
	finally:
		frappe.set_user(previous)


class _Simulation:
	def __init__(self, today):
		self.today = today
		self.counts = Counter()
		self.warehouse = seed.warehouse()
		self.taxes = _template_taxes(seed.sales_tax_template())

	# --- one order through the flow ----------------------------------------------------------------

	def order(self, plan: OrderPlan, day, at) -> None:
		"""`at`: when the order is entered; delivery and invoice follow minutes later."""
		so = self._sales_order(plan, day)
		if not self._confirm(so, plan):
			return
		if not plan.deliver:
			return
		dn = self._delivery_note(so, at + _minutes(2))
		if not plan.invoice:
			return
		si = self._sales_invoice(dn, day, at + _minutes(4), plan.due_days)
		if plan.pay:
			pay_day = min(getdate(add_days(day, plan.pay_delay_days)), self.today)
			self._payment(si, pay_day, plan.mode_of_payment)

	def _sales_order(self, plan: OrderPlan, day):
		with _as(seed.SALES_USER):
			so = frappe.get_doc(
				{
					"doctype": "Sales Order",
					"company": seed.COMPANY,
					"customer": plan.customer,
					"order_type": "Sales",
					"transaction_date": day,
					"delivery_date": add_days(day, plan.delivery_days),
					"currency": seed.CURRENCY,
					"selling_price_list": seed.PRICE_LIST,
					"taxes_and_charges": seed.sales_tax_template(),
					"taxes": [dict(row) for row in self.taxes],
					"items": [
						{
							"item_code": code,
							"qty": qty,
							"rate": ITEM_INFO[code]["rate"],
							"warehouse": self.warehouse,
							"delivery_date": add_days(day, plan.delivery_days),
						}
						for code, qty in plan.lines
					],
				}
			).insert()
		self.counts["Sales Order"] += 1
		return so

	def _confirm(self, so, plan: OrderPlan) -> bool:
		"""Run the order through the approval workflow; True when it ends up submitted."""
		with _as(seed.SALES_USER):
			needs_approval = bool(so.get(ca.APPROVAL_FIELD))
			apply_workflow(so.as_dict(), ca.ACTION_REQUEST if needs_approval else ca.ACTION_CONFIRM)
		if not needs_approval:
			return True
		self.counts["approvals_requested"] += 1
		if plan.approval == "pending":
			self.counts["pending_approval"] += 1
			return False
		with _as(seed.APPROVER):
			action = ca.ACTION_APPROVE if plan.approval == "approve" else ca.ACTION_REJECT
			apply_workflow(frappe.get_doc("Sales Order", so.name).as_dict(), action)
		self.counts["approved" if plan.approval == "approve" else "rejected"] += 1
		return plan.approval == "approve"

	def _delivery_note(self, so, at):
		from erpnext.selling.doctype.sales_order.sales_order import make_delivery_note

		with _as(seed.STOCK_USER):
			for row in so.items:
				self._restock_if_short(row.item_code, flt(row.qty), at - _minutes(1))
			dn = make_delivery_note(so.name)
			_set_posting(dn, at)
			dn.insert()
			dn.submit()
		self.counts["Delivery Note"] += 1
		return dn

	def _sales_invoice(self, dn, day, at, due_days: int):
		from erpnext.stock.doctype.delivery_note.delivery_note import make_sales_invoice

		with _as(seed.ACCOUNTS_USER):
			si = make_sales_invoice(dn.name)
			_set_posting(si, at)
			si.due_date = add_days(day, due_days)
			si.payment_schedule = []
			si.insert()
			si.submit()
		self.counts["Sales Invoice"] += 1
		return si

	def _payment(self, si, pay_day, mode_of_payment: str) -> None:
		from erpnext.accounts.doctype.payment_entry.payment_entry import get_payment_entry

		with _as(seed.ACCOUNTS_USER):
			pe = get_payment_entry("Sales Invoice", si.name)
			account = frappe.db.get_value(
				"Mode of Payment Account",
				{"parent": mode_of_payment, "company": seed.COMPANY},
				"default_account",
			)
			pe.mode_of_payment = mode_of_payment
			if account:
				pe.paid_to = account
			pe.posting_date = pay_day
			pe.reference_no = si.name
			pe.reference_date = pay_day
			pe.insert()
			pe.submit()
		self.counts["Payment Entry"] += 1

	# --- stock -------------------------------------------------------------------------------------

	def _restock_if_short(self, item_code: str, qty: float, at) -> None:
		"""Material Receipt just before a delivery the stock (now or later on) could not cover."""
		available = _available_from(item_code, self.warehouse, at)
		if available >= qty:
			return
		receipt = qty - available + ITEM_INFO[item_code]["reorder_qty"]
		se = frappe.get_doc(
			{
				"doctype": "Stock Entry",
				"stock_entry_type": "Material Receipt",
				"purpose": "Material Receipt",
				"company": seed.COMPANY,
				"items": [
					{
						"item_code": item_code,
						"t_warehouse": self.warehouse,
						"qty": receipt,
						"basic_rate": ITEM_INFO[item_code]["cost"],
					}
				],
			}
		)
		_set_posting(se, at)
		se.insert()
		se.submit()
		self.counts["Stock Entry"] += 1

	# --- bulk --------------------------------------------------------------------------------------

	def bulk_invoices(self, rng: random.Random, bulk: int) -> None:
		"""`bulk` standalone invoices (no order/delivery, no stock update) dated today."""
		with _as(seed.ACCOUNTS_USER):
			for i in range(bulk):
				lines = [
					(code, rng.randint(1, 10)) for code in rng.sample(seed.ITEM_CODES, rng.randint(1, 3))
				]
				si = frappe.get_doc(
					{
						"doctype": "Sales Invoice",
						"company": seed.COMPANY,
						"customer": rng.choice(REGULAR_CUSTOMERS),
						"set_posting_time": 1,
						"posting_date": self.today,
						"due_date": add_days(self.today, rng.choice(DUE_DAYS)),
						"currency": seed.CURRENCY,
						"selling_price_list": seed.PRICE_LIST,
						"taxes_and_charges": seed.sales_tax_template(),
						"taxes": [dict(row) for row in self.taxes],
						"items": [
							{"item_code": code, "qty": qty, "rate": ITEM_INFO[code]["rate"]}
							for code, qty in lines
						],
					}
				).insert()
				si.submit()
				self.counts["Sales Invoice"] += 1
				if (i + 1) % BULK_COMMIT_EVERY == 0:
					frappe.db.commit()  # bulk mode only: keep the transaction (and the outbox batch) bounded


def _day_start(day, today):
	"""09:00 on past days; today, early enough that the day's last posting is not in the future."""
	start = get_datetime(f"{day} {DAY_START}")
	if day == today:
		latest = now_datetime() - _minutes(MAX_ORDERS_PER_DAY * 10 + 10)
		start = max(min(start, latest), get_datetime(f"{day} 00:00:00"))
	return start


def _minutes(n: int):
	from datetime import timedelta

	return timedelta(minutes=n)


def _set_posting(doc, at) -> None:
	doc.set_posting_time = 1
	doc.posting_date = str(at.date())
	doc.posting_time = at.strftime("%H:%M:%S")


def _template_taxes(template: str | None) -> list[dict]:
	if not template:
		return []
	from erpnext.controllers.accounts_controller import get_taxes_and_charges

	return get_taxes_and_charges("Sales Taxes and Charges Template", template) or []


def _available_from(item_code: str, warehouse: str, at) -> float:
	"""Stock at `at`, lowered by any later entry whose balance is smaller (a backdated delivery reduces
	every later balance, which must not go negative)."""
	from erpnext.stock.utils import get_stock_balance

	stamp = get_datetime(at)
	qty = flt(get_stock_balance(item_code, warehouse, stamp.date(), stamp.time()))
	later_min = frappe.db.sql(
		"""select min(qty_after_transaction) from `tabStock Ledger Entry`
		where item_code = %s and warehouse = %s and is_cancelled = 0
		and timestamp(posting_date, posting_time) > %s""",
		(item_code, warehouse, stamp),
	)[0][0]
	return min(qty, flt(later_min)) if later_min is not None else qty
