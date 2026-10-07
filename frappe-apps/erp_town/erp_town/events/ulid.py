# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

from ulid import ULID


def new_ulid() -> str:
	"""A new 26-character Crockford base32 ULID (event id / idempotency key)."""
	return str(ULID())
