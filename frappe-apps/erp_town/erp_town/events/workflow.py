# Copyright (c) 2026, ERP Town
# License: GNU General Public License v3. See LICENSE

"""Workflow approval events (spec §3.3, §4.2): `on_change` hook comparing the workflow state field.

- entering a state listed in Town Settings `approval_states` → `workflow.approval.requested`
  (`approver_role` = allowed role of the first transition, in table order, out of that state);
- leaving such a state → `workflow.approval.decided` (`approved` when the new state's `doc_status` is 1,
  else `rejected`).

Correlation / causation come from the document's own latest event at emit time (an approval that submits
the document runs `on_change` after `on_submit`, so `decided` is caused by the `submitted` event).
Frappe re-runs `on_change` from `db_set()` with the same `doc_before_save`; one save emits at most once.
Town-layer failures never block the transition.
"""

from frappe.model.workflow import get_workflow, get_workflow_name
from frappe.utils import cint

from erp_town.erp_town.doctype.town_settings.town_settings import get_town_settings
from erp_town.events.emit import emit_raw
from erp_town.events.lineage import causation_of, latest_event
from erp_town.events.system import report_build_failure

SOURCE = "erp/workflow"
REQUESTED = "workflow.approval.requested"
DECIDED = "workflow.approval.decided"
APPROVED = "approved"
REJECTED = "rejected"
_HANDLED_FLAG = "town_workflow_before"


def on_change(doc, method: str | None = None) -> None:
	try:
		events = _events_for(doc)
	except Exception as e:  # noqa: BLE001 -- town-layer failures must never block the document
		_report(doc, "workflow.approval", e)
		return

	for event_type, data in events:
		try:
			own = latest_event(doc.doctype, doc.name)
		except Exception as e:  # noqa: BLE001
			_report(doc, event_type, e)
			return
		emit_raw(
			event_type,
			source=SOURCE,
			subject_doctype=doc.doctype,
			subject_name=doc.name,
			company=doc.get("company"),
			data=data,
			correlation_id=own["correlation_id"] if own else doc.name,
			causation_id=causation_of(own),
		)


def _events_for(doc) -> list[tuple[str, dict]]:
	"""(event_type, data) for this save's workflow transition, `decided` before `requested`."""
	before = doc.get_doc_before_save() if hasattr(doc, "get_doc_before_save") else None
	if before is None or doc.flags.get(_HANDLED_FLAG) is before:
		return []
	workflow_name = get_workflow_name(doc.doctype)
	if not workflow_name:
		return []
	workflow = get_workflow(doc.doctype)
	field = workflow.workflow_state_field
	old, new = before.get(field), doc.get(field)
	if not old or not new or old == new:
		return []
	doc.flags[_HANDLED_FLAG] = before

	approval_states = set(get_town_settings().approval_states)
	events = []
	if old in approval_states:
		events.append((DECIDED, {"from_state": old, "to_state": new, "decision": _decision(workflow, new)}))
	if new in approval_states:
		events.append(
			(REQUESTED, {"from_state": old, "to_state": new, "approver_role": _approver_role(workflow, new)})
		)
	return events


def _decision(workflow, state: str) -> str:
	row = next((s for s in workflow.states if s.state == state), None)
	return APPROVED if row is not None and cint(row.doc_status) == 1 else REJECTED


def _approver_role(workflow, state: str) -> str | None:
	"""None when no transition leaves the state: the event is then stored `Invalid` and reported."""
	return next((t.allowed for t in workflow.transitions if t.state == state), None)


def _report(doc, event_type: str, error: Exception) -> None:
	report_build_failure(
		event_type, error, subject_doctype=doc.doctype, subject_name=doc.name, company=doc.get("company")
	)
