# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

from unittest.mock import patch
from contextlib import ExitStack, contextmanager

import frappe
from frappe.tests import IntegrationTestCase, UnitTestCase

from erp_town.setup.local_reader import provision, validate_permissions


@contextmanager
def reader_fixture_scope(company):
	"""Use the test database while satisfying the explicit local-only entry guard."""
	original = frappe.db.get_value
	def value(doctype, *args, **kwargs):
		if doctype == "Company" and args and args[0] == {"abbr": "ACME-TH"}:
			return company
		return original(doctype, *args, **kwargs)
	with ExitStack() as stack:
		stack.enter_context(patch.object(frappe.local, "site", "town.localhost"))
		stack.enter_context(patch.dict(frappe.conf, {"developer_mode": True}))
		stack.enter_context(patch("erp_town.setup.local_reader.USER", "world-reader-fixture@example.test"))
		stack.enter_context(patch("erp_town.setup.local_reader.PROJECTION_ROLE", "Town Projection Reader Fixture"))
		stack.enter_context(patch("frappe.db.get_value", side_effect=value))
		# Bench-only repair commits its transaction; keep test fixtures rollback-only.
		stack.enter_context(patch("frappe.db.commit"))
		yield


def permission_grants(rows):
	"""Compare permission semantics rather than generated child-row identities."""
	fields = ("role", "permlevel", "read", "create", "write", "delete", "submit", "cancel", "amend", "share", "export", "report", "email", "print", "if_owner", "import", "select", "mask", "impersonate")
	return sorted(tuple(row.get(field) or 0 for field in fields) for row in rows)


class TestLocalReader(UnitTestCase):
	def test_refuses_other_sites_without_touching_the_database(self):
		with patch.object(frappe.local, "site", "test.localhost"), patch.dict(frappe.conf, {"developer_mode": True}), patch("frappe.db.get_value") as read:
			with self.assertRaises(frappe.PermissionError):
				provision()
			read.assert_not_called()

	def test_refuses_non_development_sites(self):
		with patch.object(frappe.local, "site", "town.localhost"), patch.dict(frappe.conf, {"developer_mode": False}), patch("frappe.db.get_value") as read:
			with self.assertRaises(frappe.PermissionError):
				provision()
			read.assert_not_called()

	def test_refuses_a_non_administrator(self):
		with patch.object(frappe.local, "site", "town.localhost"), patch.dict(frappe.conf, {"developer_mode": True}), patch.dict(frappe.session, {"user": "sales-fixture@example.test"}), patch("frappe.db.get_value") as read:
			with self.assertRaises(frappe.PermissionError):
				provision()
			read.assert_not_called()

	def test_permission_repair_refuses_unsafe_scope_before_database_access(self):
		from erp_town.setup.local_reader import repair_existing_permissions
		for site, developer_mode, user in (("test.localhost", True, "Administrator"), ("town.localhost", False, "Administrator"), ("town.localhost", True, "sales-fixture@example.test")):
			with self.subTest(site=site, developer_mode=developer_mode, user=user), patch.object(frappe.local, "site", site), patch.dict(frappe.conf, {"developer_mode": developer_mode}), patch.dict(frappe.session, {"user": user}), patch("frappe.get_all") as read:
				with self.assertRaises(frappe.PermissionError):
					repair_existing_permissions()
				read.assert_not_called()

	def test_permission_profile_is_exact_and_read_only(self):
		from erp_town.setup.local_reader import DOCTYPES
		rows = [frappe._dict(parent=dt, permlevel=0, read=1) for dt in DOCTYPES]
		validate_permissions(rows)
		for field in ("write", "create", "delete", "submit", "cancel", "amend", "share", "export", "report", "email", "print", "if_owner"):
			with self.subTest(field=field):
				changed = [frappe._dict(row) for row in rows]
				changed[0][field] = 1
				with self.assertRaises(frappe.PermissionError):
					validate_permissions(changed)
		for changed in (rows[:-1], rows + [frappe._dict(parent="User", read=1)], rows + [rows[0]]):
			with self.assertRaises(frappe.PermissionError):
				validate_permissions(changed)


class TestLocalReaderDatabase(IntegrationTestCase):
	def setUp(self):
		super().setUp()
		frappe.set_user("Administrator")
		from erp_town.setup.local_reader import DOCTYPES
		# The integration runner shares a transaction; isolate each permission fixture.
		self.assertEqual(frappe.local.site, "test.localhost")
		self.savepoint = "local_reader_fixture"
		frappe.db.savepoint(self.savepoint)
		for doctype in DOCTYPES:
			frappe.db.delete("Custom DocPerm", {"parent": doctype})
			frappe.clear_cache(doctype=doctype)
		self.company = frappe.get_all("Company", pluck="name", limit=1)[0]
		self.sales_user = "sales-reader-regression@example.test"
		frappe.get_doc({"doctype": "User", "email": self.sales_user, "first_name": "Sales permission fixture", "enabled": 1, "user_type": "System User", "send_welcome_email": 0, "roles": [{"role": "Sales User"}]}).insert(ignore_permissions=True)

	def tearDown(self):
		from erp_town.setup.local_reader import DOCTYPES
		frappe.db.rollback(save_point=self.savepoint)
		for doctype in DOCTYPES:
			frappe.clear_cache(doctype=doctype)
		frappe.clear_cache(user=self.sales_user)
		frappe.clear_cache(user="world-reader-fixture@example.test")
		super().tearDown()

	def test_real_permission_defaults_and_idempotent_reader(self):
		from erp_town.setup.local_reader import DOCTYPES
		standard = {doctype: permission_grants(frappe.get_all("DocPerm", filters={"parent": doctype}, fields=["*"])) for doctype in DOCTYPES}
		with reader_fixture_scope(self.company):
			self.assertTrue(provision() == provision(), "Idempotent provisioning must reuse the same reader credential")
			validate_permissions(frappe.get_all("Custom DocPerm", filters={"role": "Town Projection Reader Fixture"}, fields=["*"]))
			for doctype in DOCTYPES:
				copied = frappe.get_all("Custom DocPerm", filters={"parent": doctype, "role": ["!=", "Town Projection Reader Fixture"]}, fields=["*"])
				self.assertEqual(permission_grants(copied), standard[doctype])
				self.assertEqual(permission_grants(frappe.get_all("DocPerm", filters={"parent": doctype}, fields=["*"])), standard[doctype])
			# The effective human profile must still permit reading and creating orders.
			from frappe.permissions import get_role_permissions
			sales = get_role_permissions(frappe.get_meta("Sales Order"), user=self.sales_user)
			self.assertTrue(sales.get("read"))
			self.assertTrue(sales.get("create"))
			projection = get_role_permissions(frappe.get_meta("Sales Order"), user="world-reader-fixture@example.test")
			self.assertTrue(projection.get("read"))
			for field in ("create", "write", "delete", "submit", "cancel", "amend", "share", "export"):
				self.assertFalse(projection.get(field), field)
			reader = frappe.get_doc("User", "world-reader-fixture@example.test")
			self.assertEqual(reader.user_type, "System User")
			self.assertFalse(reader.send_welcome_email)
			reader.append("roles", {"role": "System Manager"})
			reader.save(ignore_permissions=True)
			with self.assertRaises(frappe.PermissionError):
				provision()

	def test_repairs_projection_only_shadow_and_is_idempotent(self):
		from erp_town.setup.local_reader import DOCTYPES, FORBIDDEN, repair_existing_permissions
		with reader_fixture_scope(self.company):
			provision()
			for doctype in DOCTYPES:
				frappe.db.delete("Custom DocPerm", {"parent": doctype, "role": ["!=", "Town Projection Reader Fixture"]})
				frappe.clear_cache(doctype=doctype)
			from frappe.permissions import get_role_permissions
			self.assertFalse(get_role_permissions(frappe.get_meta("Sales Order"), user=self.sales_user).get("read"))
			before = {doctype: permission_grants(frappe.get_all("Custom DocPerm", filters={"parent": doctype}, fields=["*"])) for doctype in DOCTYPES}
			review = repair_existing_permissions()
			self.assertTrue(review["dry_run"])
			self.assertEqual(set(review["restored"]), set(DOCTYPES))
			self.assertEqual(before, {doctype: permission_grants(frappe.get_all("Custom DocPerm", filters={"parent": doctype}, fields=["*"])) for doctype in DOCTYPES})
			repair_existing_permissions(dry_run=False)
			first = {doctype: permission_grants(frappe.get_all("Custom DocPerm", filters={"parent": doctype}, fields=["*"])) for doctype in DOCTYPES}
			repair_existing_permissions(dry_run=False)
			second = {doctype: permission_grants(frappe.get_all("Custom DocPerm", filters={"parent": doctype}, fields=["*"])) for doctype in DOCTYPES}
			self.assertEqual(first, second)
			for doctype in DOCTYPES:
				copied = frappe.get_all("Custom DocPerm", filters={"parent": doctype, "role": ["!=", "Town Projection Reader Fixture"]}, fields=["*"])
				self.assertEqual(permission_grants(copied), permission_grants(frappe.get_all("DocPerm", filters={"parent": doctype}, fields=["*"])))
			validate_permissions(frappe.get_all("Custom DocPerm", filters={"role": "Town Projection Reader Fixture"}, fields=["*"]))
			sales = get_role_permissions(frappe.get_meta("Sales Order"), user=self.sales_user)
			self.assertTrue(sales.get("read"))
			self.assertTrue(sales.get("create"))
			projection = get_role_permissions(frappe.get_meta("Sales Order"), user="world-reader-fixture@example.test")
			self.assertTrue(projection.get("read"))
			self.assertFalse(any(projection.get(field) for field in FORBIDDEN))

	def test_existing_operator_custom_permissions_are_preserved(self):
		from erp_town.setup.local_reader import repair_existing_permissions
		custom = frappe.get_doc({"doctype": "Custom DocPerm", "parent": "Sales Order", "parenttype": "DocType", "parentfield": "permissions", "role": "Sales User", "permlevel": 0, "read": 1, "create": 0, "write": 0}).insert(ignore_permissions=True)
		with reader_fixture_scope(self.company):
			provision()
			before = frappe.get_all("Custom DocPerm", filters={"parent": "Sales Order"}, fields=["*"])
			review = repair_existing_permissions(dry_run=False)
			self.assertIn("Sales Order", review["preserved_custom"])
			after = frappe.get_all("Custom DocPerm", filters={"parent": "Sales Order"}, fields=["*"])
			self.assertEqual(permission_grants(before), permission_grants(after))
			self.assertEqual(len(after), 2)
			self.assertEqual(frappe.db.get_value("Custom DocPerm", custom.name, "create"), 0)
