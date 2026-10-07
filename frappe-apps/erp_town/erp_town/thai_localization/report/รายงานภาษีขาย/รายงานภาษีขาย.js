// Copyright (c) 2026, ERP Town
// License: GNU General Public License v3. See LICENSE

// Sales VAT report filters (spec §5.4): company, tax month and Gregorian year (label shows the BE year).

const BE_OFFSET = 543;
const THAI_MONTHS = [
	"มกราคม",
	"กุมภาพันธ์",
	"มีนาคม",
	"เมษายน",
	"พฤษภาคม",
	"มิถุนายน",
	"กรกฎาคม",
	"สิงหาคม",
	"กันยายน",
	"ตุลาคม",
	"พฤศจิกายน",
	"ธันวาคม",
];
const YEAR_LABEL = "ปี (ค.ศ.)";

function year_label(year) {
	const value = Number.parseInt(year, 10);
	return value ? `${YEAR_LABEL} — พ.ศ. ${value + BE_OFFSET}` : YEAR_LABEL;
}

function show_be_year(report) {
	const filter = report.get_filter("year");
	filter.df.label = year_label(filter.get_value());
	filter.set_label?.(filter.df.label);
	filter.$wrapper?.attr("title", filter.df.label);
	filter.$input?.attr("placeholder", filter.df.label);
}

const today = new Date();

frappe.query_reports["รายงานภาษีขาย"] = {
	filters: [
		{
			fieldname: "company",
			label: __("Company"),
			fieldtype: "Link",
			options: "Company",
			reqd: 1,
			default: frappe.defaults.get_user_default("Company"),
		},
		{
			fieldname: "month",
			label: __("Month"),
			fieldtype: "Select",
			reqd: 1,
			options: THAI_MONTHS.map((label, i) => ({ value: String(i + 1), label })),
			default: String(today.getMonth() + 1),
		},
		{
			fieldname: "year",
			label: year_label(today.getFullYear()),
			fieldtype: "Int",
			reqd: 1,
			default: today.getFullYear(),
			on_change: (report) => {
				show_be_year(report);
				report.refresh();
			},
		},
	],
	onload: (report) => show_be_year(report),
	formatter: (value, row, column, data, default_formatter) => {
		if (data && data.invoice_no === "รวม") {
			if (column.fieldname === "invoice_no") {
				return `<b>${value}</b>`;
			}
			return `<b>${default_formatter(value, row, column, data)}</b>`;
		}
		return default_formatter(value, row, column, data);
	},
};
