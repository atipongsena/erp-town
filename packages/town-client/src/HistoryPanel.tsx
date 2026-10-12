import { useEffect, useRef, useState } from 'react'
import { type Language, statusLabel } from './copy.ts'
import { type HistoryView, readHistory } from './history.ts'
import type { DisplayStatus } from './world-view.ts'
export function HistoryPanel({
  company,
  live,
  language,
}: {
  company: string
  live: boolean
  language: Language
}) {
  const [view, setView] = useState<HistoryView>()
  const [error, setError] = useState(false)
  const [busy, setBusy] = useState(false)
  const abort = useRef<AbortController>(undefined)
  useEffect(() => {
    if (!company || !live) abort.current?.abort()
    setView(undefined)
    setError(false)
    setBusy(false)
    return () => abort.current?.abort()
  }, [company, live])
  if (!company || !live) return null
  const th = language === 'th'
  return (
    <section className="history-panel">
      <button
        type="button"
        disabled={busy}
        onClick={() => {
          abort.current?.abort()
          const current = new AbortController()
          abort.current = current
          setBusy(true)
          setError(false)
          void readHistory(window.fetch.bind(window), company, current.signal)
            .then((result) => {
              if (!current.signal.aborted) setView(result)
            })
            .catch(() => {
              if (!current.signal.aborted) {
                setView(undefined)
                setError(true)
              }
            })
            .finally(() => {
              if (!current.signal.aborted) setBusy(false)
            })
        }}
      >
        {th ? 'ดูเหตุการณ์ย้อนหลัง 15 นาที' : 'Events from the past 15 minutes'}
      </button>
      {error && <p role="alert">{th ? 'ดูประวัติไม่ได้ในขณะนี้' : 'History is unavailable right now.'}</p>}
      {view && (
        <>
          <p>
            {th
              ? 'เฉพาะเหตุการณ์ที่ยังเก็บไว้และคุณมีสิทธิ์ดู ไม่ใช่ยอดคงเหลือย้อนหลัง'
              : 'Retained events you can currently view. This does not reconstruct earlier balances.'}
          </p>
          {view.truncated && (
            <p>
              {th ? 'แสดงเพียงบางส่วนของช่วงที่เลือก' : 'Only part of the requested window is available.'}
            </p>
          )}
          <ol>
            {view.transitions.map((row) => (
              <li key={`${row.time}-${row.doctype}-${row.name}-${row.kind}-${row.status}`}>
                <time>
                  {new Date(row.time).toLocaleTimeString(th ? 'th-TH' : 'en-GB', {
                    hour: '2-digit',
                    minute: '2-digit',
                  })}
                </time>{' '}
                {row.name} ·{' '}
                {row.status === 'updated'
                  ? th
                    ? 'อัปเดตแล้ว'
                    : 'Updated'
                  : row.status === 'decided'
                    ? th
                      ? 'พิจารณาแล้ว'
                      : 'Reviewed'
                    : statusLabel(language, row.status as DisplayStatus)}
              </li>
            ))}
          </ol>
          {!view.transitions.length && (
            <p>{th ? 'ไม่มีเหตุการณ์ที่คุณดูได้ในช่วงนี้' : 'No visible events in this window.'}</p>
          )}
        </>
      )}
    </section>
  )
}
