"""Buddhist Era (พ.ศ.) date formatting."""

from datetime import date, datetime
from typing import Literal

from frappe.utils import getdate

BE_OFFSET = 543
THAI_MONTHS = (
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
)


def be_date(value: date | datetime | str, fmt: Literal["long", "short"] = "long") -> str:
	"""Format ``value`` in the Buddhist Era.

	``long`` -> ``7 ตุลาคม 2569`` (no leading zero on the day); ``short`` -> ``07/10/2569``.
	Accepts date, datetime or an ISO date/datetime string.
	"""
	if fmt not in ("long", "short"):
		raise ValueError(f"be_date: fmt must be 'long' or 'short', got {fmt!r}")
	if value is None or value == "":
		raise ValueError("be_date: value is required")
	d = getdate(value)
	if d is None:
		raise ValueError(f"be_date: invalid date {value!r}")
	year = d.year + BE_OFFSET
	if fmt == "short":
		return f"{d.day:02d}/{d.month:02d}/{year}"
	return f"{d.day} {THAI_MONTHS[d.month - 1]} {year}"
