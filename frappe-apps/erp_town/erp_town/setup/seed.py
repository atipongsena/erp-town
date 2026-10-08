# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Dev-site seed (spec §7): `pnpm erp:seed` → `bench --site town.localhost execute erp_town.setup.seed.run`.

Idempotent: every record is looked up before it is created, so a second run creates nothing and returns the
same counts. `run()` never commits on its own (`bench execute` commits at the end), except that the two Sales
Order custom fields are added first: adding a column is DDL, which MariaDB commits implicitly.

Creates a Thai demo company with users, agents and the credit-limit approval workflow; the agent API
key/secret pairs are generated only when absent and printed once (store them in the git-ignored
`infra/docker/.env` under the printed names; `.env.example` lists the names).
"""

import os
from dataclasses import dataclass

import frappe
from frappe.utils import getdate, nowdate

from erp_town.selling.credit_approval import (
	WORKFLOW_NAME,
	ensure_approval_fields,
	ensure_approval_workflow,
	ensure_credit_controller,
)
from erp_town.setup.masters import ensure_system_settings, mark_setup_complete
from erp_town.thai.setup import HEAD_OFFICE_BRANCH, company_account, create_thai_company

COMPANY = "บริษัท ตัวอย่างเทรดดิ้ง จำกัด"
ABBR = "ACME-TH"
CURRENCY = "THB"
COUNTRY = "Thailand"
LANGUAGE = "th"
TIME_ZONE = "Asia/Bangkok"
WAREHOUSE_NAME = "คลังหลัก"
UOM = "ชิ้น"
ITEM_GROUP = "สินค้าสำนักงาน"
CUSTOMER_GROUP = "ลูกค้านิติบุคคล"
TERRITORY = "ประเทศไทย"
PRICE_LIST = "ขายมาตรฐาน"
SALES_TAX_TEMPLATE = "ภาษีขาย 7%"
OPENING_ACCOUNT = "บัญชีพักเปิดยอด"  # Temporary account of the Thai chart
DEFAULT_PASSWORD = "town-dev-123"  # dev only; override with env SEED_USER_PASSWORD
CREDIT_LIMIT = 5000
EMAIL_DOMAIN = "acme-th.test"


# --- Thai tax IDs --------------------------------------------------------------------------------------


def make_tax_id(seq: int, prefix: str = "010556") -> str:
	"""Deterministic fake juristic-person tax ID that passes the mod-11 checksum.

	`prefix` + 6-digit `seq` gives the first 12 digits (`0-10-5-56`: juristic person, Bangkok, limited
	company, registered BE 2556 — the shape of a real ID, not a real one); the 13th is the check digit
	`(11 - Σ d[i]·(13−i) mod 11) mod 10` (the rule `erp_town.thai.taxid.is_valid_tax_id` checks).
	"""
	body = f"{prefix}{seq:06d}"
	if len(body) != 12 or not body.isdigit():
		raise ValueError(f"tax ID body must be 12 digits, got {body!r}")
	total = sum(int(d) * (13 - i) for i, d in enumerate(body))
	return f"{body}{(11 - total % 11) % 10}"


COMPANY_TAX_ID = make_tax_id(1, prefix="010555")


# --- master data ---------------------------------------------------------------------------------------

# code, name, selling rate, valuation rate, opening qty, reorder level, reorder qty
ITEMS = (
	("ITEM-001", "กระดาษถ่ายเอกสาร A4 80 แกรม", 135, 98, 35, 30, 100),
	("ITEM-002", "ปากกาลูกลื่น สีน้ำเงิน", 12, 7, 400, 100, 300),
	("ITEM-003", "แฟ้มสันกว้าง 3 นิ้ว", 85, 55, 120, 30, 60),
	("ITEM-004", "หมึกพิมพ์เลเซอร์ สีดำ", 1890, 1350, 32, 30, 20),
	("ITEM-005", "เครื่องคิดเลข 12 หลัก", 450, 290, 60, 15, 30),
	("ITEM-006", "ลวดเย็บกระดาษ เบอร์ 10", 15, 8, 500, 100, 300),
	("ITEM-007", "เครื่องเย็บกระดาษ", 220, 140, 80, 20, 40),
	("ITEM-008", "กรรไกรสแตนเลส 8 นิ้ว", 95, 55, 90, 20, 40),
	("ITEM-009", "เทปใส 1 นิ้ว", 25, 14, 300, 60, 150),
	("ITEM-010", "ซองจดหมายสีน้ำตาล A4", 6, 3, 1000, 200, 500),
	("ITEM-011", "สมุดบันทึก ปกแข็ง", 65, 38, 150, 40, 80),
	("ITEM-012", "ปากกาเน้นข้อความ", 30, 17, 250, 50, 150),
	("ITEM-013", "แฟลชไดรฟ์ 64GB", 290, 210, 34, 30, 30),
	("ITEM-014", "เมาส์ไร้สาย", 390, 260, 70, 15, 30),
	("ITEM-015", "แป้นพิมพ์ภาษาไทย", 590, 400, 50, 10, 20),
	("ITEM-016", "ถ่านอัลคาไลน์ AA (แพ็ก 4)", 120, 80, 200, 40, 100),
	("ITEM-017", "กระดาษโน้ตกาว", 45, 25, 220, 50, 120),
	("ITEM-018", "ตู้เก็บเอกสาร 4 ลิ้นชัก", 4500, 3200, 12, 3, 5),
	("ITEM-019", "เก้าอี้สำนักงาน", 2900, 2000, 25, 5, 10),
	("ITEM-020", "ไวท์บอร์ด 90x120 ซม.", 1650, 1100, 18, 4, 8),
)
ITEM_CODES = [row[0] for row in ITEMS]

# code, name, city, postal code
CUSTOMERS = (
	("CUST-001", "บริษัท สยามวัสดุก่อสร้าง จำกัด", "กรุงเทพมหานคร", "10400"),
	("CUST-002", "บริษัท เจริญค้าส่ง จำกัด", "นนทบุรี", "11000"),
	("CUST-003", "ห้างหุ้นส่วนจำกัด รุ่งเรืองพาณิชย์", "ปทุมธานี", "12000"),
	("CUST-004", "บริษัท ไทยออฟฟิศซัพพลาย จำกัด", "กรุงเทพมหานคร", "10110"),
	("CUST-005", "บริษัท นครชัยเทรดดิ้ง จำกัด", "นครราชสีมา", "30000"),
	("CUST-006", "บริษัท ล้านนาเครื่องเขียน จำกัด", "เชียงใหม่", "50000"),
	("CUST-007", "บริษัท อีสานรุ่งโรจน์ จำกัด", "ขอนแก่น", "40000"),
	("CUST-008", "บริษัท ทะเลใต้ซัพพลาย จำกัด", "สงขลา", "90110"),
	("CUST-009", "ห้างหุ้นส่วนจำกัด สมใจการค้า", "ชลบุรี", "20000"),
	("CUST-010", "ห้างหุ้นส่วนจำกัด มั่นคงพาณิชย์", "ระยอง", "21000"),
)
CUSTOMER_CODES = [row[0] for row in CUSTOMERS]
LIMITED_CUSTOMERS = ("CUST-009", "CUST-010")  # credit_limit 5,000 → orders over it need approval

COMPANY_ADDRESS = {
	"address_line1": "99/9 ถนนสุขุมวิท",
	"address_line2": "แขวงคลองเตย เขตคลองเตย",
	"city": "กรุงเทพมหานคร",
	"pincode": "10110",
	"phone": "02-000-0000",
}

# Thai postal layout: house no./road, sub-district + district, province + postcode (country omitted)
ADDRESS_TEMPLATE = """{{ address_line1 }}<br>
{% if address_line2 %}{{ address_line2 }}<br>{% endif -%}
{{ city }}{% if state and state != city %} {{ state }}{% endif %}{% if pincode %} {{ pincode }}{% endif %}<br>
{% if phone %}โทร. {{ phone }}<br>{% endif -%}
{% if fax %}แฟกซ์ {{ fax }}<br>{% endif -%}
{% if email_id %}อีเมล {{ email_id }}<br>{% endif -%}
"""

# email, first name, roles: the job's role first; ERPNext's Sales Manager is an add-on to Sales User (on its
# own it cannot even read an Item), so the approver holds both
STAFF = (
	(f"sales@{EMAIL_DOMAIN}", "ฝ่ายขาย", ("Sales User",)),
	(f"stock@{EMAIL_DOMAIN}", "ฝ่ายคลังสินค้า", ("Stock User",)),
	(f"accounts@{EMAIL_DOMAIN}", "ฝ่ายบัญชี", ("Accounts User",)),
	(f"approver@{EMAIL_DOMAIN}", "ผู้อนุมัติ", ("Sales Manager", "Sales User")),
)
SALES_USER, STOCK_USER, ACCOUNTS_USER, APPROVER = (row[0] for row in STAFF)


@dataclass(frozen=True)
class Agent:
	key: str
	user: str
	first_name: str
	role: str  # the narrowest standard role that covers the agent's job
	env_prefix: str  # printed as <env_prefix>_API_KEY / <env_prefix>_API_SECRET


AGENTS = (
	Agent(
		"agent.sales_assistant",
		f"agent.sales_assistant@{EMAIL_DOMAIN}",
		"ผู้ช่วยฝ่ายขาย (agent)",
		"Sales User",
		"TOWN_AGENT_SALES_ASSISTANT",
	),
	Agent(
		"agent.ar_collector",
		f"agent.ar_collector@{EMAIL_DOMAIN}",
		"ผู้ติดตามหนี้ (agent)",
		"Accounts User",
		"TOWN_AGENT_AR_COLLECTOR",
	),
)
SEEDED_USERS = [row[0] for row in STAFF] + [a.user for a in AGENTS]
# read-only Custom DocPerms the delivery clerk needs: ERPNext fills the customer's address and the item's
# income account / cost center on every Delivery Note, which Stock User alone may not read
EXTRA_READ_PERMISSIONS = (("Address", "Stock User"), ("Account", "Stock User"), ("Cost Center", "Stock User"))

MODES_OF_PAYMENT = (("เงินสด", "Cash", "เงินสด"), ("โอนเงิน", "Bank", "เงินฝากธนาคาร"))  # name, type, account
STOCK_ENTRY_TYPES = ("Material Receipt", "Material Issue", "Material Transfer")


# --- entry point ---------------------------------------------------------------------------------------


def run(force: bool = False) -> dict:
	"""Seed the dev site; returns the number of seeded records of each kind (equal on every run).

	Demo data, demo users and a known default password: refused unless the site is a development or test
	site (`developer_mode` or `allow_tests` in its config) or `force=True` (e.g. a rehearsal pilot site,
	infra/docker/pilot/RESTORE.md). A real pilot site uses `erp_town.setup.pilot.bootstrap` instead.
	"""
	if not (force or frappe.conf.get("developer_mode") or frappe.conf.get("allow_tests")):
		frappe.throw(
			f"erp_town.setup.seed.run refused on site {frappe.local.site}: demo data only on a developer_mode "
			"or allow_tests site; pass force=True for a rehearsal (real pilot: erp_town.setup.pilot.bootstrap)"
		)
	ensure_approval_fields()  # DDL first: commits nothing but itself
	_ensure_system_settings()
	create_thai_company(COMPANY, ABBR, COMPANY_TAX_ID)  # chart, VAT templates, fiscal years around today
	_ensure_global_defaults()
	_ensure_masters()
	wh = _ensure_warehouse()
	_ensure_selling_and_stock_settings(wh)
	_ensure_items(wh)
	_ensure_opening_stock(wh)
	_ensure_address_template()
	_ensure_company_address()
	_ensure_customers()
	_ensure_modes_of_payment()
	_ensure_default_sales_taxes()
	_ensure_users()
	credentials = _ensure_agents()
	ensure_credit_controller()
	ensure_approval_workflow()
	mark_setup_complete()
	_print_credentials(credentials)
	return counts()


def warehouse() -> str:
	"""The `คลังหลัก` warehouse of the seeded company (record name)."""
	return frappe.db.get_value("Warehouse", {"warehouse_name": WAREHOUSE_NAME, "company": COMPANY})


def counts() -> dict:
	"""Seeded records currently present; any duplicate would show up as a higher count."""
	item_filter = {"parent": ["in", ITEM_CODES], "parenttype": "Item"}
	return {
		"Company": frappe.db.count("Company", {"name": COMPANY}),
		"Warehouse": frappe.db.count("Warehouse", {"warehouse_name": WAREHOUSE_NAME, "company": COMPANY}),
		"Item": frappe.db.count("Item", {"name": ["in", ITEM_CODES]}),
		"Item Price": frappe.db.count(
			"Item Price", {"price_list": PRICE_LIST, "item_code": ["in", ITEM_CODES]}
		),
		"Item Reorder": frappe.db.count("Item Reorder", item_filter),
		"Stock Reconciliation": frappe.db.count(
			"Stock Reconciliation", {"company": COMPANY, "purpose": "Opening Stock", "docstatus": 1}
		),
		"Customer": frappe.db.count("Customer", {"name": ["in", CUSTOMER_CODES]}),
		"Address": frappe.db.count(
			"Dynamic Link",
			{"parenttype": "Address", "link_name": ["in", [*CUSTOMER_CODES, COMPANY]]},
		),
		"User": frappe.db.count("User", {"name": ["in", SEEDED_USERS]}),
		"Town Agent Identity": frappe.db.count(
			"Town Agent Identity", {"agent_key": ["in", [a.key for a in AGENTS]]}
		),
		"Mode of Payment": frappe.db.count(
			"Mode of Payment", {"name": ["in", [m[0] for m in MODES_OF_PAYMENT]]}
		),
		"Workflow": frappe.db.count("Workflow", {"name": WORKFLOW_NAME}),
	}


# --- site ----------------------------------------------------------------------------------------------


def _ensure_system_settings() -> None:
	ensure_system_settings(language=LANGUAGE, time_zone=TIME_ZONE, country=COUNTRY, currency=CURRENCY)


def _ensure_global_defaults() -> None:
	wanted = {"default_company": COMPANY, "default_currency": CURRENCY, "country": COUNTRY}
	defaults = frappe.get_single("Global Defaults")
	if all(defaults.get(k) == v for k, v in wanted.items()):
		return
	defaults.update(wanted)
	defaults.save(ignore_permissions=True)


def _set_single_if_empty(doctype: str, values: dict) -> None:
	for field, value in values.items():
		if not frappe.db.get_single_value(doctype, field):
			frappe.db.set_single_value(doctype, field, value)


# --- masters -------------------------------------------------------------------------------------------


def _ensure_masters() -> None:
	for doctype, name, name_field, parent_field, root in (
		("Item Group", ITEM_GROUP, "item_group_name", "parent_item_group", "All Item Groups"),
		(
			"Customer Group",
			CUSTOMER_GROUP,
			"customer_group_name",
			"parent_customer_group",
			"All Customer Groups",
		),
		("Territory", TERRITORY, "territory_name", "parent_territory", "All Territories"),
	):
		if not frappe.db.exists(doctype, name):
			frappe.get_doc({"doctype": doctype, name_field: name, parent_field: root, "is_group": 0}).insert(
				ignore_permissions=True
			)
	if not frappe.db.exists("UOM", UOM):
		frappe.get_doc({"doctype": "UOM", "uom_name": UOM, "must_be_whole_number": 1}).insert(
			ignore_permissions=True
		)
	if not frappe.db.exists("Price List", PRICE_LIST):
		frappe.get_doc(
			{
				"doctype": "Price List",
				"price_list_name": PRICE_LIST,
				"currency": CURRENCY,
				"selling": 1,
				"enabled": 1,
			}
		).insert(ignore_permissions=True)
	for name in STOCK_ENTRY_TYPES:
		if not frappe.db.exists("Stock Entry Type", name):
			frappe.get_doc({"doctype": "Stock Entry Type", "name": name, "purpose": name}).insert(
				ignore_permissions=True
			)


def _ensure_warehouse() -> str:
	if name := warehouse():
		return name
	parent = frappe.db.get_value(
		"Warehouse", {"company": COMPANY, "is_group": 1, "parent_warehouse": ["is", "not set"]}
	)
	return (
		frappe.get_doc(
			{
				"doctype": "Warehouse",
				"warehouse_name": WAREHOUSE_NAME,
				"company": COMPANY,
				"parent_warehouse": parent,
			}
		)
		.insert(ignore_permissions=True)
		.name
	)


def _ensure_selling_and_stock_settings(wh: str) -> None:
	_set_single_if_empty(
		"Selling Settings",
		{"selling_price_list": PRICE_LIST, "customer_group": CUSTOMER_GROUP, "territory": TERRITORY},
	)
	_set_single_if_empty("Stock Settings", {"default_warehouse": wh, "stock_uom": UOM})


def _ensure_items(wh: str) -> None:
	for code, name, rate, cost, _opening, reorder_level, reorder_qty in ITEMS:
		if not frappe.db.exists("Item", code):
			frappe.get_doc(
				{
					"doctype": "Item",
					"item_code": code,
					"item_name": name,
					"item_group": ITEM_GROUP,
					"stock_uom": UOM,
					"is_stock_item": 1,
					"include_item_in_manufacturing": 0,
					"valuation_rate": cost,
					"item_defaults": [
						{"company": COMPANY, "default_warehouse": wh, "default_price_list": PRICE_LIST}
					],
					"reorder_levels": [
						{
							"warehouse": wh,
							"warehouse_reorder_level": reorder_level,
							"warehouse_reorder_qty": reorder_qty,
							"material_request_type": "Purchase",
						}
					],
				}
			).insert(ignore_permissions=True)
		if not frappe.db.exists("Item Price", {"item_code": code, "price_list": PRICE_LIST}):
			frappe.get_doc(
				{
					"doctype": "Item Price",
					"item_code": code,
					"price_list": PRICE_LIST,
					"price_list_rate": rate,
				}
			).insert(ignore_permissions=True)


def opening_stock_date() -> str:
	"""1 January of last year: before anything the scenario simulates."""
	return f"{getdate(nowdate()).year - 1}-01-01"


def _ensure_opening_stock(wh: str) -> None:
	"""One Opening Stock reconciliation for the items that have never moved in `คลังหลัก`."""
	moved = set(
		frappe.get_all(
			"Stock Ledger Entry",
			filters={"warehouse": wh, "item_code": ["in", ITEM_CODES], "is_cancelled": 0},
			pluck="item_code",
			distinct=True,
		)
	)
	rows = [
		{"item_code": code, "warehouse": wh, "qty": opening, "valuation_rate": cost}
		for code, _name, _rate, cost, opening, _level, _qty in ITEMS
		if code not in moved
	]
	if not rows:
		return
	sr = frappe.get_doc(
		{
			"doctype": "Stock Reconciliation",
			"company": COMPANY,
			"purpose": "Opening Stock",
			"set_posting_time": 1,
			"posting_date": opening_stock_date(),
			"posting_time": "00:00:00",
			"expense_account": company_account(COMPANY, OPENING_ACCOUNT),
			"cost_center": frappe.get_cached_value("Company", COMPANY, "cost_center"),
			"items": rows,
		}
	).insert(ignore_permissions=True)
	sr.submit()


def _ensure_address_template() -> None:
	"""Before any Address: ERPNext renders every saved address with its country's template."""
	if not frappe.db.exists("Address Template", COUNTRY):
		frappe.get_doc(
			{
				"doctype": "Address Template",
				"country": COUNTRY,
				"is_default": 0 if frappe.db.exists("Address Template", {"is_default": 1}) else 1,
				"template": ADDRESS_TEMPLATE,
			}
		).insert(ignore_permissions=True)


def _has_address(link_doctype: str, link_name: str) -> bool:
	return bool(
		frappe.db.exists(
			"Dynamic Link", {"parenttype": "Address", "link_doctype": link_doctype, "link_name": link_name}
		)
	)


def _insert_address(title: str, link_doctype: str, link_name: str, values: dict, **flags) -> None:
	frappe.get_doc(
		{
			"doctype": "Address",
			"address_title": title,
			"address_type": "Billing",
			"country": COUNTRY,
			"th_branch_code": HEAD_OFFICE_BRANCH,
			"links": [{"link_doctype": link_doctype, "link_name": link_name}],
			**values,
			**flags,
		}
	).insert(ignore_permissions=True)


def _ensure_company_address() -> None:
	if not _has_address("Company", COMPANY):
		_insert_address(COMPANY, "Company", COMPANY, COMPANY_ADDRESS, is_your_company_address=1)


def _ensure_customers() -> None:
	for seq, (code, name, city, pincode) in enumerate(CUSTOMERS, start=1):
		if not frappe.db.exists("Customer", code):
			frappe.get_doc(
				{
					"doctype": "Customer",
					"customer_name": name,
					"customer_type": "Company",
					"customer_group": CUSTOMER_GROUP,
					"territory": TERRITORY,
					"tax_id": make_tax_id(seq),
					"th_branch_code": HEAD_OFFICE_BRANCH,
					"default_price_list": PRICE_LIST,
					"credit_limits": (
						[{"company": COMPANY, "credit_limit": CREDIT_LIMIT}]
						if code in LIMITED_CUSTOMERS
						else []
					),
				}
			).insert(ignore_permissions=True, set_name=code)
		if not _has_address("Customer", code):
			_insert_address(
				name,
				"Customer",
				code,
				{"address_line1": f"{seq * 11}/{seq} ถนนพหลโยธิน", "city": city, "pincode": pincode},
				is_primary_address=1,
			)


def _ensure_modes_of_payment() -> None:
	for name, mop_type, account_name in MODES_OF_PAYMENT:
		account = company_account(COMPANY, account_name)
		if not frappe.db.exists("Mode of Payment", name):
			frappe.get_doc(
				{
					"doctype": "Mode of Payment",
					"mode_of_payment": name,
					"type": mop_type,
					"enabled": 1,
					"accounts": [{"company": COMPANY, "default_account": account}],
				}
			).insert(ignore_permissions=True)


def _ensure_default_sales_taxes() -> None:
	"""VAT 7% is the company's default sales tax template (pre-filled on new orders in Desk)."""
	if frappe.db.exists("Sales Taxes and Charges Template", {"company": COMPANY, "is_default": 1}):
		return
	name = sales_tax_template()
	if name:
		frappe.db.set_value("Sales Taxes and Charges Template", name, "is_default", 1)


def sales_tax_template() -> str | None:
	return frappe.db.get_value(
		"Sales Taxes and Charges Template", {"company": COMPANY, "title": SALES_TAX_TEMPLATE}
	)


# --- users and agents ----------------------------------------------------------------------------------


def seed_password() -> str:
	return os.environ.get("SEED_USER_PASSWORD") or DEFAULT_PASSWORD


def _ensure_user(email: str, first_name: str, roles: tuple[str, ...], password: str | None) -> None:
	if not frappe.db.exists("User", email):
		user = {
			"doctype": "User",
			"email": email,
			"first_name": first_name,
			"language": LANGUAGE,
			"send_welcome_email": 0,
			"user_type": "System User",
			"roles": [{"role": role} for role in roles],
		}
		if password:
			user["new_password"] = password
		frappe.get_doc(user).insert(ignore_permissions=True)
	elif missing := [role for role in roles if role not in frappe.get_roles(email)]:
		frappe.get_doc("User", email).add_roles(*missing)


def _ensure_users() -> None:
	password = seed_password()
	for email, first_name, roles in STAFF:
		_ensure_user(email, first_name, roles, password)
	for doctype, role in EXTRA_READ_PERMISSIONS:
		_ensure_read_permission(doctype, role)


def _ensure_read_permission(doctype: str, role: str) -> None:
	"""A level-0 read rule for `role` (Custom DocPerm; Frappe first copies the standard rules)."""
	from frappe.permissions import add_permission

	granted = frappe.db.exists(
		"Custom DocPerm", {"parent": doctype, "role": role, "permlevel": 0, "if_owner": 0, "read": 1}
	)
	if not granted:
		add_permission(doctype, role, 0, "read")


def _ensure_agents() -> list[tuple[Agent, str, str]]:
	"""Agent users (API only, no password) + Town Agent Identity; returns newly generated key pairs."""
	from frappe.utils.password import get_decrypted_password

	generated = []
	for agent in AGENTS:
		_ensure_user(agent.user, agent.first_name, (agent.role,), None)
		if not frappe.db.exists("Town Agent Identity", agent.key):
			frappe.get_doc(
				{"doctype": "Town Agent Identity", "agent_key": agent.key, "user": agent.user, "enabled": 1}
			).insert(ignore_permissions=True)
		user = frappe.get_doc("User", agent.user)
		if user.api_key and get_decrypted_password("User", agent.user, "api_secret", raise_exception=False):
			continue
		user.api_key = user.api_key or frappe.generate_hash(length=15)
		api_secret = frappe.generate_hash(length=15)
		user.api_secret = api_secret
		user.save(ignore_permissions=True)
		generated.append((agent, user.api_key, api_secret))
	return generated


def _print_credentials(generated: list[tuple[Agent, str, str]]) -> None:
	"""Shown once: copy the lines into infra/docker/.env (git-ignored). Never commit them."""
	if not generated:
		return
	print("# ERP Town agent API credentials (shown once) - add to infra/docker/.env, never commit:")
	for agent, api_key, api_secret in generated:
		print(f"{agent.env_prefix}_API_KEY={api_key}")
		print(f"{agent.env_prefix}_API_SECRET={api_secret}")
