# erp_town

Frappe app for ERP Town: the town layer (event capture, outbox, Redis Streams publisher) and Thai
localization on top of ERPNext v16 + HRMS.

- Requires: Frappe / ERPNext / HRMS `version-16`, Python 3.14.
- Modules: `ERP Town`, `Thai Localization`.
- `erp_town/events/schema/` is generated from `packages/event-schema` (`pnpm schema:gen`); do not edit.

In development the app is bind-mounted into the dev bench (`pnpm erp:up`, see the repo `AGENTS.md`);
tests run on `test.localhost` with `pnpm erp:test`.

## License

GNU General Public License v3.0 only (`GPL-3.0-only`), see [LICENSE](LICENSE).
