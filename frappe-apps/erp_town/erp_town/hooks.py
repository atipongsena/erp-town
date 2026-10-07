app_name = "erp_town"
app_title = "ERP Town"
app_publisher = "ERP Town"
app_description = "ERPNext v16 + HRMS as an office-simulation game: town layer and Thai localization"
app_license = "GPL-3.0-only"

required_apps = ["erpnext", "hrms"]

jinja = {"filters": ["erp_town.thai.jinja.be_date", "erp_town.thai.jinja.bahttext"]}

after_install = "erp_town.setup.install.after_install"
after_migrate = ["erp_town.thai.setup.ensure_custom_fields", "erp_town.thai.setup.ensure_naming"]

# Buddhist-Era document numbers `<prefix><BE yy><mm>-<####>` (spec §5.2), e.g. series `IV.BEYY.BEMM.-.####`
naming_series_variables = {"BEYY": "erp_town.thai.naming.be_yy", "BEMM": "erp_town.thai.naming.be_mm"}

# town events for the sales slice (spec §4.2); on_update_after_submit has no event type in slice 1
doc_events = {
	# Thai 13-digit tax ID check (spec §5.1); blocks the save, unlike the town layer below
	"Company": {"validate": "erp_town.thai.setup.validate_tax_id"},
	"Customer": {"validate": "erp_town.thai.setup.validate_tax_id"},
	"Sales Order": {
		"on_submit": "erp_town.events.doc_hooks.on_doc_event",
		"on_cancel": "erp_town.events.doc_hooks.on_doc_event",
		"on_change": "erp_town.events.workflow.on_change",  # workflow.approval.*
	},
	"Delivery Note": {
		# order matters: erp.stock.below_reorder is caused by the DN's submitted event
		"on_submit": [
			"erp_town.events.doc_hooks.on_doc_event",
			"erp_town.events.stock.on_dn_submit",
		],
		"on_cancel": "erp_town.events.doc_hooks.on_doc_event",
	},
	"Sales Invoice": {
		"on_submit": "erp_town.events.doc_hooks.on_doc_event",
		"on_cancel": "erp_town.events.doc_hooks.on_doc_event",
	},
	"Payment Entry": {
		"on_submit": "erp_town.events.doc_hooks.on_doc_event",
		"on_cancel": "erp_town.events.doc_hooks.on_doc_event",
	},
}

# outbox -> redis-town stream (spec §4.3): the sweeper retries what the after-commit publish job missed;
# the overdue job runs hourly but only acts in Town Settings `overdue_job_hour`
scheduler_events = {
	"cron": {
		"* * * * *": ["erp_town.events.publisher.sweep"],
		"0 * * * *": ["erp_town.events.jobs.emit_overdue_hourly"],
	},
	"daily": ["erp_town.events.publisher.purge"],
}
