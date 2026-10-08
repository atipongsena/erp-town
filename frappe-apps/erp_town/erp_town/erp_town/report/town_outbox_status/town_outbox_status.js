// Copyright (c) 2026, ERP Town
// License: GNU General Public License v3. See LICENSE

// Town Outbox Status (spec §4.4, §8): outbox rows by status; "ส่งใหม่" requeues the Failed rows shown.
// No default date window: every Failed row counted by the Town Failed card must be listed for resending.

const REQUEUE_LABEL = "ส่งใหม่";

function failed_event_ids(report) {
	return (report.data || []).filter((row) => row.status === "Failed").map((row) => row.event_id);
}

function requeue_failed(report) {
	const event_ids = failed_event_ids(report);
	if (!event_ids.length) {
		frappe.msgprint(__("There are no failed events in this list"));
		return;
	}
	frappe.confirm(__("Resend {0} failed events?", [event_ids.length]), () => {
		frappe
			.call({
				method: "erp_town.events.api.requeue_failed",
				args: { event_ids },
				freeze: true,
			})
			.then((r) => {
				frappe.show_alert({
					message: __("{0} events queued for resending", [r.message || 0]),
					indicator: "green",
				});
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
		},
		{
			fieldname: "to_date",
			label: __("To Date"),
			fieldtype: "Date",
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
