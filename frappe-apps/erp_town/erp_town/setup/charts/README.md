# Charts of accounts

## `th_tfrs_npaes.json` — draft — accountant review required

Starter chart for a Thai trading company laid out after TFRS for NPAEs (หมวด 1 สินทรัพย์, 2 หนี้สิน,
3 ส่วนของเจ้าของ, 4 รายได้, 5 ค่าใช้จ่าย). It holds the minimum accounts the ERP Town sales flow, stock
valuation and VAT templates need, and has **not** been reviewed by an accountant yet (spec §5.1).

Format: ERPNext's chart file format (`country_code`, `name`, `tree`). JSON has no comments, so the draft
marker lives in the top-level `_comment` key, which ERPNext's chart loaders ignore (they read only `name`,
`disabled` and `tree`). `erp_town.thai.setup.create_thai_company` passes `tree` to ERPNext's
`create_charts(company, custom_chart=tree)`; the file is not installed into ERPNext's own chart folders,
so it does not appear in the setup wizard.

Accounts are looked up by `account_name` (e.g. `ภาษีขาย`), never by the generated record name
(`2120 - ภาษีขาย - <abbr>`), so account numbers may change during review. Keep the `account_type` of every
leaf the setup relies on (see `DEFAULT_ACCOUNTS` in `erp_town/thai/setup.py` and `LEAVES` in `tests/test_thai_setup.py`).
