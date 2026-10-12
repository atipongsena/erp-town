import { useEffect, useRef, useState } from 'react'
import { type AgentRunStatus, type AgentRuns, readAgentRuns } from './agent-runs.ts'
import type { Language } from './copy.ts'

const statuses: Record<Language, Record<AgentRunStatus, string>> = {
  th: {
    queued: 'รอเริ่มงาน',
    running: 'กำลังทำงาน',
    completed: 'เสร็จแล้ว',
    needs_human: 'รอตรวจใน ERP',
    failed: 'หยุดเพราะข้อผิดพลาด',
    cancelled: 'ยกเลิกแล้ว',
  },
  en: {
    queued: 'Queued',
    running: 'In progress',
    completed: 'Completed',
    needs_human: 'Review in ERP',
    failed: 'Stopped with an error',
    cancelled: 'Cancelled',
  },
}
export function AgentRunsPanel({
  company,
  live,
  language,
}: {
  company: string
  live: boolean
  language: Language
}) {
  const [view, setView] = useState<AgentRuns>()
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const abort = useRef<AbortController>(undefined)
  // The host mounts a new panel for each live company and unmounts on replay or
  // disconnect, so neither a pending response nor old metadata crosses scopes.
  useEffect(() => () => abort.current?.abort(), [])
  if (!company || !live) return null
  const th = language === 'th'
  return (
    <section className="agent-runs-panel" aria-label={th ? 'งาน AI' : 'AI work'}>
      <button
        type="button"
        disabled={busy}
        onClick={() => {
          abort.current?.abort()
          const current = new AbortController()
          abort.current = current
          setBusy(true)
          setError(undefined)
          setView(undefined)
          void readAgentRuns(window.fetch.bind(window), company, current.signal)
            .then((result) => {
              if (!current.signal.aborted) setView(result)
            })
            .catch((err) => {
              if (!current.signal.aborted)
                setError(
                  err instanceof Error && err.message === 'forbidden' ? 'forbidden' : 'unavailable',
                )
            })
            .finally(() => {
              if (!current.signal.aborted) setBusy(false)
            })
        }}
      >
        {th ? 'ดูสถานะงาน AI' : 'View AI work'}
      </button>
      <p>{th ? 'แสดงงานของบริษัทนี้สำหรับผู้ดูแลระบบ' : 'Company work visible to system managers.'}</p>
      {error && (
        <p role="alert">
          {error === 'forbidden'
            ? th
              ? 'บัญชีนี้ไม่มีสิทธิ์ดูงาน AI'
              : 'This account cannot view AI work.'
            : th
              ? 'ดูงาน AI ไม่ได้ในขณะนี้'
              : 'AI work is unavailable right now.'}
        </p>
      )}
      {view && (
        <>
          {view.truncated && (
            <p>
              {th
                ? 'แสดงสูงสุด 25 งาน โดยงานที่ต้องตรวจใน ERP อยู่ด้านบน'
                : 'Showing up to 25 runs, with ERP reviews first.'}
            </p>
          )}
          {!view.runs.length && (
            <p>{th ? 'ยังไม่มีงาน AI ในบริษัทนี้' : 'No AI runs in this company yet.'}</p>
          )}
          <ol>
            {view.runs.map((run) => (
              <li key={run.id}>
                <strong>
                  {run.agent} · {statuses[language][run.status]}
                </strong>
                <span>
                  {run.id} ·{' '}
                  <time>{new Date(run.created_at).toLocaleString(th ? 'th-TH' : 'en-GB')}</time>
                </span>
                {(run.unresolved || run.status === 'needs_human') && (
                  <p className="agent-review-note">
                    {th
                      ? 'ตรวจรายการจริงใน ERP ก่อนทำงานซ้ำ เพราะผลการทำรายการอาจยังไม่ชัดเจน'
                      : 'Check the actual ERP records before repeating this work. The earlier outcome may still be uncertain.'}
                  </p>
                )}
              </li>
            ))}
          </ol>
        </>
      )}
    </section>
  )
}
