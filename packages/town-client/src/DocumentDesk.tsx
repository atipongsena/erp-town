import { useEffect, useRef, useState, useSyncExternalStore } from 'react'
import type { Language } from './copy.ts'
import { documentCopy, documentNames } from './document-copy.ts'
import { type DocumentKind, type DocumentSession, documentTypes } from './documents.ts'
import { classicPath, type Department } from './world-view.ts'

function today() {
  const date = new Date()
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 10)
}
export function DocumentDesk({
  session,
  language,
  department,
  onClose,
}: {
  session: DocumentSession
  language: Language
  department: Department
  onClose?: () => void
}) {
  const t = documentCopy[language]
  const names = documentNames[language]
  const view = useSyncExternalStore(session.subscribe, session.view)
  const [kind, setKind] = useState<DocumentKind>(
    department === 'warehouse'
      ? 'delivery-note'
      : department === 'bank'
        ? 'sales-invoice'
        : 'sales-order',
  )
  const [name, setName] = useState('')
  const [newOrder, setNewOrder] = useState(false)
  const [lineIds, setLineIds] = useState([0])
  const [nextLine, setNextLine] = useState(1)
  const heading = useRef<HTMLHeadingElement>(null)
  const openName = useRef<HTMLInputElement>(null)
  const doc = view.document
  useEffect(() => {
    if (doc && !view.busy) {
      heading.current?.focus({ preventScroll: true })
      heading.current?.scrollIntoView({ block: 'nearest' })
    }
  }, [doc, view.busy])
  const locked = view.busy || view.uncertain
  const path = view.ref && classicPath(documentTypes[view.ref.kind], view.ref.name)
  const listPath = `/desk/${view.ref?.kind ?? kind}`
  return (
    <section className="document-desk" aria-label={t.title} onInput={() => session.edit(kind)}>
      <div className="document-heading">
        <h3>{t.title}</h3>
        {department === 'shop' && (
          <button
            type="button"
            disabled={locked}
            onClick={() => {
              session.close()
              session.edit('sales-order')
              setNewOrder(true)
            }}
          >
            {t.newOrder}
          </button>
        )}
      </div>
      <form
        className="document-open"
        onSubmit={(event) => {
          event.preventDefault()
          setNewOrder(false)
          void session.open({ kind, name: name.trim() })
        }}
      >
        <label>
          {t.kind}
          <select
            value={kind}
            disabled={view.busy}
            onChange={(e) => setKind(e.target.value as DocumentKind)}
          >
            {(Object.keys(names) as DocumentKind[]).map((key) => (
              <option key={key} value={key}>
                {names[key]}
              </option>
            ))}
          </select>
        </label>
        <label>
          {t.name}
          <input
            ref={openName}
            required
            value={name}
            autoComplete="off"
            spellCheck={false}
            disabled={view.busy}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <button type="submit" disabled={view.busy}>
          {t.open}
        </button>
      </form>
      {view.busy && <p aria-live="polite">{t.busy}</p>}
      {view.error && (
        <p className="document-notice" role="alert">
          {t.errors[view.error]}
        </p>
      )}
      {view.uncertain && (
        <div className="document-notice" role="alert">
          <p>{t.uncertain}</p>
          <a href={path || listPath} target="_blank" rel="noreferrer">
            {t.classic} ↗
          </a>
          <button
            type="button"
            disabled={view.busy}
            onClick={() => {
              session.acknowledge()
              session.close()
              setNewOrder(false)
              if (onClose) onClose()
              else openName.current?.focus({ preventScroll: true })
            }}
          >
            {t.acknowledge}
          </button>
        </div>
      )}
      {newOrder && !view.ref && (
        <form
          className="document-form"
          aria-label={t.newOrder}
          onSubmit={(event) => {
            event.preventDefault()
            const values = new FormData(event.currentTarget)
            void session.create('sales-order', {
              customer: String(values.get('customer')).trim(),
              transaction_date: values.get('transaction_date'),
              delivery_date: values.get('delivery_date'),
              lines: lineIds.map((id) => {
                const rate = String(values.get(`rate-${id}`)).trim()
                return {
                  item_code: String(values.get(`item-${id}`)).trim(),
                  qty: values.get(`qty-${id}`),
                  ...(rate ? { rate } : {}),
                }
              }),
            })
          }}
        >
          <p className="document-hint">{t.codeHint}</p>
          <label>
            {t.customer}
            <input name="customer" required autoComplete="off" disabled={locked} />
          </label>
          <div className="document-dates">
            <label>
              {t.transactionDate}
              <input
                name="transaction_date"
                type="date"
                defaultValue={today()}
                required
                disabled={locked}
              />
            </label>
            <label>
              {t.deliveryDate}
              <input
                name="delivery_date"
                type="date"
                defaultValue={today()}
                required
                disabled={locked}
              />
            </label>
          </div>
          {lineIds.map((id, index) => (
            <fieldset className="document-line" key={id}>
              <legend>
                {t.item} {index + 1}
              </legend>
              <label>
                {t.item}
                <input name={`item-${id}`} required autoComplete="off" disabled={locked} />
              </label>
              <div className="document-dates">
                <label>
                  {t.qty}
                  <input
                    name={`qty-${id}`}
                    required
                    defaultValue="1"
                    inputMode="decimal"
                    pattern="[0-9]+(\\.[0-9]+)?"
                    disabled={locked}
                  />
                </label>
                <label>
                  {t.rate}
                  <input
                    name={`rate-${id}`}
                    inputMode="decimal"
                    pattern="[0-9]+(\\.[0-9]+)?"
                    placeholder={t.autoRate}
                    disabled={locked}
                  />
                </label>
              </div>
              {lineIds.length > 1 && (
                <button
                  type="button"
                  disabled={locked}
                  onClick={() => setLineIds(lineIds.filter((line) => line !== id))}
                >
                  {t.removeLine}
                </button>
              )}
            </fieldset>
          ))}
          <div className="document-actions">
            <button
              type="button"
              disabled={locked}
              onClick={() => {
                setLineIds([...lineIds, nextLine])
                setNextLine(nextLine + 1)
              }}
            >
              {t.addLine}
            </button>
            <button type="submit" className="document-primary" disabled={locked}>
              {t.create}
            </button>
          </div>
        </form>
      )}
      {view.ref && (
        <section className="document-detail" aria-label={t.review}>
          <div className="document-heading">
            <div>
              <small className="document-caption">{names[view.ref.kind]}</small>
              <h4 ref={heading} tabIndex={-1}>
                {view.ref.name}
              </h4>
            </div>
            <button
              type="button"
              disabled={view.busy}
              onClick={() => {
                session.close()
                setNewOrder(false)
                if (onClose) onClose()
                else openName.current?.focus({ preventScroll: true })
              }}
              aria-label={t.close}
            >
              ×
            </button>
          </div>
          <div className="document-actions">
            <button type="button" disabled={view.busy} onClick={() => void session.refresh()}>
              {t.refresh}
            </button>
            {path && (
              <a href={path} target="_blank" rel="noreferrer">
                {t.classic} ↗
              </a>
            )}
          </div>
          {doc && (
            <>
              <p className={`document-state ${doc.status}`}>{t.status[doc.status]}</p>
              <dl className="document-meta">
                <div>
                  <dt>{t.customer}</dt>
                  <dd>{doc.customer ?? doc.party}</dd>
                </div>
                {(['transaction_date', 'delivery_date', 'posting_date', 'due_date'] as const).map(
                  (key) =>
                    doc[key] && (
                      <div key={key}>
                        <dt>
                          {
                            {
                              transaction_date: t.transactionDate,
                              delivery_date: t.deliveryDate,
                              posting_date: t.postingDate,
                              due_date: language === 'th' ? 'กำหนดชำระ' : 'Due date',
                            }[key]
                          }
                        </dt>
                        <dd>{doc[key]}</dd>
                      </div>
                    ),
                )}
                {doc.mode_of_payment && (
                  <div>
                    <dt>{t.paymentMode}</dt>
                    <dd>{doc.mode_of_payment}</dd>
                  </div>
                )}
                {doc.workflow_state && (
                  <div>
                    <dt>{t.workflow}</dt>
                    <dd>{doc.workflow_state}</dd>
                  </div>
                )}
              </dl>
              {doc.lines && (
                <div className="document-table-scroll">
                  <table>
                    <caption>{names[doc.kind]}</caption>
                    <thead>
                      <tr>
                        <th>{t.item}</th>
                        <th>{t.qty}</th>
                        <th>{t.rate}</th>
                        <th>{t.grand}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {doc.lines.map((line) => (
                        <tr key={line.id}>
                          <td>{line.item_code}</td>
                          <td>{line.qty}</td>
                          <td>{line.rate}</td>
                          <td>{line.amount}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
              {doc.kind !== 'delivery-note' && (
                <div className="document-amounts">
                  <h5>{t.amounts}</h5>
                  <small className="document-currency">{t.currency}</small>
                  <dl>
                    {(
                      [
                        'net_total',
                        'tax_total',
                        'grand_total',
                        'rounded_total',
                        'outstanding',
                        'paid',
                      ] as const
                    ).map(
                      (key) =>
                        doc[key] !== undefined && (
                          <div key={key}>
                            <dt>
                              {
                                {
                                  net_total: t.net,
                                  tax_total: t.tax,
                                  grand_total: t.grand,
                                  rounded_total: t.rounded,
                                  outstanding: t.outstanding,
                                  paid: t.paid,
                                }[key]
                              }
                            </dt>
                            <dd data-amount={key}>{doc[key]}</dd>
                          </div>
                        ),
                    )}
                  </dl>
                </div>
              )}
              {(doc.against_sales_orders ?? doc.against_invoices)?.length ? (
                <p>
                  {t.related}: {(doc.against_sales_orders ?? doc.against_invoices)?.join(', ')}
                </p>
              ) : null}
              {view.approval && (
                <p className="document-notice" aria-live="polite">
                  {t.approval}
                </p>
              )}
              {doc.status === 'Draft' && (
                <div className="document-confirm">
                  <p>{t.draftHint}</p>
                  <button
                    type="button"
                    className="document-primary"
                    disabled={locked || view.approval}
                    onClick={() => void session.submit()}
                  >
                    {t.submit}
                  </button>
                </div>
              )}
              {doc.status === 'Submitted' &&
                doc.kind !== 'payment-entry' &&
                !(
                  doc.kind === 'sales-invoice' &&
                  (doc.is_return || /^0(\.0+)?$/.test(doc.outstanding ?? ''))
                ) && (
                  <form
                    className="document-form"
                    aria-label={
                      doc.kind === 'sales-order'
                        ? t.nextDelivery
                        : doc.kind === 'delivery-note'
                          ? t.nextInvoice
                          : t.nextPayment
                    }
                    onSubmit={(event) => {
                      event.preventDefault()
                      const values = new FormData(event.currentTarget)
                      const posting = String(values.get('posting_date')).trim()
                      const date = posting ? { posting_date: posting } : {}
                      if (doc.kind === 'sales-order')
                        void session.create('delivery-note', { sales_order: doc.name, ...date })
                      else if (doc.kind === 'delivery-note')
                        void session.create('sales-invoice', { delivery_note: doc.name, ...date })
                      else
                        void session.create('payment-entry', {
                          sales_invoice: doc.name,
                          mode_of_payment: String(values.get('mode')).trim(),
                          ...date,
                        })
                    }}
                  >
                    <label>
                      {t.postingDate}
                      <input type="date" name="posting_date" disabled={locked} />
                    </label>
                    <small className="document-date-hint">{t.dateHint}</small>
                    {doc.kind === 'sales-invoice' && (
                      <>
                        <label>
                          {t.paymentMode}
                          <input required name="mode" autoComplete="off" disabled={locked} />
                        </label>
                        <p>{t.paymentHint}</p>
                      </>
                    )}
                    <button type="submit" className="document-primary" disabled={locked}>
                      {doc.kind === 'sales-order'
                        ? t.nextDelivery
                        : doc.kind === 'delivery-note'
                          ? t.nextInvoice
                          : t.nextPayment}
                    </button>
                  </form>
                )}
            </>
          )}
        </section>
      )}
    </section>
  )
}
