app_name = "erp_town"
app_title = "ERP Town"
app_publisher = "ERP Town"
app_description = "ERPNext v16 + HRMS as an office-simulation game: town layer and Thai localization"
app_license = "GPL-3.0-only"

required_apps = ["erpnext", "hrms"]

jinja = {"filters": ["erp_town.thai.jinja.be_date", "erp_town.thai.jinja.bahttext"]}
