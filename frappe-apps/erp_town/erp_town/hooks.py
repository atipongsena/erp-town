app_name = "erp_town"
app_title = "ERP Town"
app_publisher = "ERP Town"
app_description = "ERPNext v16 + HRMS as an office-simulation game: town layer and Thai localization"
app_license = "GPL-3.0-only"

required_apps = ["erpnext", "hrms"]

jinja = {"filters": ["erp_town.thai.jinja.be_date", "erp_town.thai.jinja.bahttext"]}

# town events for the sales slice (spec §4.2); on_update_after_submit has no event type in slice 1
doc_events = {
	"Sales Order": {
		"on_submit": "erp_town.events.doc_hooks.on_doc_event",
		"on_cancel": "erp_town.events.doc_hooks.on_doc_event",
	},
	"Delivery Note": {
		"on_submit": "erp_town.events.doc_hooks.on_doc_event",
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

# outbox -> redis-town stream (spec §4.3): the sweeper retries what the after-commit publish job missed
scheduler_events = {
	"cron": {"* * * * *": ["erp_town.events.publisher.sweep"]},
	"daily": ["erp_town.events.publisher.purge"],
}
