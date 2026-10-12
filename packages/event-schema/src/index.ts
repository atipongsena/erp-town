import { Ajv2020, type ErrorObject, type ValidateFunction } from 'ajv/dist/2020.js'
import addFormats from 'ajv-formats'
import envelope from '../schema/envelope.json' with { type: 'json' }
import agentProgressUpdated from '../schema/events/agent.progress.updated.json' with {
  type: 'json',
}
import agentRunCompleted from '../schema/events/agent.run.completed.json' with { type: 'json' }
import agentRunFailed from '../schema/events/agent.run.failed.json' with { type: 'json' }
import agentRunNeedsHuman from '../schema/events/agent.run.needs_human.json' with { type: 'json' }
import agentRunStarted from '../schema/events/agent.run.started.json' with { type: 'json' }
import devTaskCompleted from '../schema/events/dev.task.completed.json' with { type: 'json' }
import devTaskFailed from '../schema/events/dev.task.failed.json' with { type: 'json' }
import devTaskStarted from '../schema/events/dev.task.started.json' with { type: 'json' }
import devTaskUpdated from '../schema/events/dev.task.updated.json' with { type: 'json' }
import deliveryNoteCancelled from '../schema/events/erp.delivery_note.cancelled.json' with {
  type: 'json',
}
import deliveryNoteSubmitted from '../schema/events/erp.delivery_note.submitted.json' with {
  type: 'json',
}
import paymentEntryCancelled from '../schema/events/erp.payment_entry.cancelled.json' with {
  type: 'json',
}
import paymentEntrySubmitted from '../schema/events/erp.payment_entry.submitted.json' with {
  type: 'json',
}
import salesInvoiceCancelled from '../schema/events/erp.sales_invoice.cancelled.json' with {
  type: 'json',
}
import salesInvoiceOverdue from '../schema/events/erp.sales_invoice.overdue.json' with {
  type: 'json',
}
import salesInvoiceSubmitted from '../schema/events/erp.sales_invoice.submitted.json' with {
  type: 'json',
}
import salesOrderCancelled from '../schema/events/erp.sales_order.cancelled.json' with {
  type: 'json',
}
import salesOrderSubmitted from '../schema/events/erp.sales_order.submitted.json' with {
  type: 'json',
}
import stockBelowReorder from '../schema/events/erp.stock.below_reorder.json' with { type: 'json' }
import systemError from '../schema/events/system.error.json' with { type: 'json' }
import approvalDecided from '../schema/events/workflow.approval.decided.json' with { type: 'json' }
import approvalRequested from '../schema/events/workflow.approval.requested.json' with {
  type: 'json',
}

export * from './generated.ts'

export const ENVELOPE_SCHEMA_ID = 'https://erp-town.local/schema/envelope.json'

/** Every schema of the contract (envelope first), each identified by its `$id`. */
export const SCHEMAS: readonly object[] = [
  envelope,
  salesOrderSubmitted,
  salesOrderCancelled,
  deliveryNoteSubmitted,
  deliveryNoteCancelled,
  salesInvoiceSubmitted,
  salesInvoiceCancelled,
  salesInvoiceOverdue,
  paymentEntrySubmitted,
  paymentEntryCancelled,
  stockBelowReorder,
  approvalRequested,
  approvalDecided,
  systemError,
  agentRunStarted,
  agentRunCompleted,
  agentRunNeedsHuman,
  agentRunFailed,
  agentProgressUpdated,
  devTaskStarted,
  devTaskUpdated,
  devTaskCompleted,
  devTaskFailed,
]

export type ValidationResult = { valid: true } | { valid: false; errors: string[] }

/** A fresh Ajv (2020-12, `date`/`date-time` formats) with every contract schema registered. */
export function createAjv(): Ajv2020 {
  const ajv = new Ajv2020({ allErrors: true })
  // ajv-formats is CommonJS; `.default` is the plugin under both Node ESM interop and bundlers.
  addFormats.default(ajv, ['date', 'date-time'])
  for (const schema of SCHEMAS) ajv.addSchema(schema)
  return ajv
}

const compileEnvelope = (): ValidateFunction => {
  const validate = createAjv().getSchema(ENVELOPE_SCHEMA_ID)
  if (!validate) throw new Error(`event schema ${ENVELOPE_SCHEMA_ID} failed to load`)
  return validate
}
const validateEnvelope = compileEnvelope()

const formatError = (e: ErrorObject): string => {
  const where = e.instancePath === '' ? '(root)' : e.instancePath
  const extra =
    'additionalProperty' in e.params
      ? ` (${String(e.params.additionalProperty)})`
      : 'propertyName' in e.params
        ? ` (${String(e.params.propertyName)})`
        : ''
  return `${where} ${e.message ?? 'is invalid'}${extra}`
}

/** Validate an event against the town event contract (envelope + per-type data schema). */
export function validateEvent(e: unknown): ValidationResult {
  if (validateEnvelope(e)) return { valid: true }
  return { valid: false, errors: (validateEnvelope.errors ?? []).map(formatError) }
}
