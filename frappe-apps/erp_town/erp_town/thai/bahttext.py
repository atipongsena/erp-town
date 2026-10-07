"""Thai amount in words, following Excel BAHTTEXT conventions."""

from decimal import ROUND_HALF_UP, Decimal

_DIGITS = ("ศูนย์", "หนึ่ง", "สอง", "สาม", "สี่", "ห้า", "หก", "เจ็ด", "แปด", "เก้า")
_POSITIONS = ("", "สิบ", "ร้อย", "พัน", "หมื่น", "แสน")
_MILLION = "ล้าน"
_GROUP = 1_000_000


def _group_words(n: int) -> str:
	"""Words for 0 < n < 1,000,000 (one 'ล้าน' group); returns '' for 0."""
	words = []
	digits = str(n)[::-1]
	for pos in range(len(digits) - 1, -1, -1):
		d = int(digits[pos])
		if d == 0:
			continue
		if pos == 0 and d == 1 and n > 1:
			words.append("เอ็ด")
		elif pos == 1 and d == 1:
			words.append("สิบ")
		elif pos == 1 and d == 2:
			words.append("ยี่สิบ")
		else:
			words.append(_DIGITS[d] + _POSITIONS[pos])
	return "".join(words)


def _integer_words(n: int) -> str:
	"""Words for n > 0; groups of millions repeat 'ล้าน' (10^12 -> หนึ่งล้านล้าน)."""
	groups = []
	while n:
		n, rem = divmod(n, _GROUP)
		groups.append(rem)
	return _MILLION.join(_group_words(g) for g in reversed(groups))


def bahttext(amount: Decimal | float | int | str) -> str:  # noqa: PYI041 -- signature per plan interface
	"""Return the Thai words for ``amount`` baht, rounded to satang with ROUND_HALF_UP."""
	value = Decimal(str(amount))
	if not value.is_finite():
		raise ValueError(f"bahttext: amount must be finite, got {amount!r}")
	value = value.quantize(Decimal("0.01"), rounding=ROUND_HALF_UP)
	prefix = "ลบ" if value < 0 else ""
	baht, satang = divmod(int(abs(value) * 100), 100)

	if baht == 0 and satang == 0:
		return "ศูนย์บาทถ้วน"
	if baht == 0:
		return prefix + _group_words(satang) + "สตางค์"
	tail = _group_words(satang) + "สตางค์" if satang else "ถ้วน"
	return prefix + _integer_words(baht) + "บาท" + tail
