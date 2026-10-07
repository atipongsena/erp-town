"""Thai 13-digit tax ID (เลขประจำตัวผู้เสียภาษี / national ID) checksum."""

_ASCII_DIGITS = frozenset("0123456789")


def is_valid_tax_id(value: str) -> bool:
	"""True when ``value`` is exactly 13 ASCII digits with a valid check digit.

	s = Σ_{i=0..11} d[i]·(13−i); check digit = (11 − s mod 11) mod 10 must equal d[12].
	Nothing is stripped; non-str input is invalid.
	"""
	if not isinstance(value, str) or len(value) != 13 or not set(value) <= _ASCII_DIGITS:
		return False
	d = [int(c) for c in value]
	s = sum(d[i] * (13 - i) for i in range(12))
	return (11 - s % 11) % 10 == d[12]
