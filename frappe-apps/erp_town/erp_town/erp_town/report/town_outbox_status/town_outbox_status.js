// Copyright (c) 2026, ERP Town
// License: GNU General Public License v3. See LICENSE

// Town Outbox Status (spec §4.4, §8): outbox rows by status; "ส่งใหม่" requeues the Failed rows shown.

const REQUEUE_LABEL = "ส่งใหม่";

function failed_event_ids(report) {
	return (report.data || []).filter((row) => row.status === "Failed").map((row) => row.event_id);
}

function requeue_failed(report) {
	const event_ids = failed_event_ids(report);
	if (!event_ids.length) {
		frappe.msgprint("ไม่มีเหตุการณ์ที่ส่งไม่สำเร็จในรายการนี้");
		return;
	}
	frappe.confirm(`ส่งเหตุการณ์ที่ส่งไม่สำเร็จ ${event_ids.length} รายการใหม่หรือไม่?`, () => {
		frappe
			.call({
				method: "erp_town.events.api.requeue_failed",
				args: { event_ids },
				freeze: true,
			})
			.then((r) => {
				frappe.show_alert({ message: `ตั้งให้ส่งใหม่แล้ว ${r.message || 0} รายการ`, indicator: "green" });
				report.refresh();
			});
	});
}

frappe.query_reports["Town Outbox Status"] = {
	filters: [
		{
			fieldname: "status",
			label: __("Status"),
			fieldtype: "Select",
			options: ["", "Failed", "Invalid", "Pending", "Published"],
		},
		{
			fieldname: "from_date",
			label: __("From Date"),
			fieldtype: "Date",
			default: frappe.datetime.add_days(frappe.datetime.get_today(), -7),
		},
		{
			fieldname: "to_date",
			label: __("To Date"),
			fieldtype: "Date",
			default: frappe.datetime.get_today(),
		},
	],
	onload: (report) => {
		report.page.add_inner_button(REQUEUE_LABEL, () => requeue_failed(report));
	},
	formatter: (value, row, column, data, default_formatter) => {
		const formatted = default_formatter(value, row, column, data);
		if (column.fieldname === "status" && data && data.status === "Failed") {
			return `<span class="text-danger">${formatted}</span>`;
		}
		return formatted;
	},
};
