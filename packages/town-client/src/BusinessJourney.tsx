import { useEffect, useMemo, useState } from 'react'
import { type BusinessJourneyView, JourneyLoader, readJourneyDocument } from './business-journey.ts'
import type { Language } from './copy.ts'
import type { DashboardView, TownSelection } from './dashboard-view.ts'
import { type DocumentRef, documentKind, documentTypes } from './documents.ts'
export function BusinessJourney({
  root,
  view,
  language,
  onSelect,
}: {
  root: DocumentRef
  view: DashboardView
  language: Language
  onSelect: (s: TownSelection) => void
}) {
  const th = language === 'th'
  const loader = useMemo(
    () => new JourneyLoader((ref, signal) => readJourneyDocument(ref, view.company, signal)),
    [view.company],
  )
  const [state, setState] = useState<{ scope: string; data: BusinessJourneyView }>()
  const [refresh, setRefresh] = useState(0)
  const request = JSON.stringify({
    company: view.company,
    source: view.source,
    root,
    candidates: view.facts.flatMap((f) => {
      const kind = documentKind(f.doctype)
      return f.kind === 'document' && kind ? [{ kind, name: f.name }] : []
    }),
    refresh,
  })
  useEffect(() => {
    const {
      company,
      source,
      root: requestedRoot,
      candidates,
    } = JSON.parse(request) as {
      company: string
      source: string
      root: DocumentRef
      candidates: DocumentRef[]
    }
    const scope = request
    if (source !== 'live') {
      loader.cancel()
      setState(undefined)
      return
    }
    let active = true
    setState(undefined)
    void loader.load(requestedRoot, candidates, company).then((data) => {
      if (active && data) setState({ scope, data })
    })
    return () => {
      active = false
      loader.cancel()
    }
  }, [request, loader])
  const data = state?.scope === request ? state.data : undefined
  return (
    <section className="business-journey" aria-label={th ? 'เส้นทางเอกสาร' : 'Business journey'}>
      <div className="hud-heading">
        <h2>{th ? 'เส้นทางเอกสาร' : 'Business journey'}</h2>
        {view.source === 'live' && (
          <button type="button" onClick={() => setRefresh((n) => n + 1)}>
            {th ? 'อ่านหลักฐานใหม่' : 'Refresh evidence'}
          </button>
        )}
      </div>
      <p>
        {th
          ? 'เชื่อมเฉพาะรายการที่อ้างอิงกันจริง ขอบเขตคือเอกสารที่คุณมองเห็นในเมือง ไม่ค้นหารายการที่ซ่อนอยู่'
          : 'Links use verified references among visible town records; unseen children are not discovered.'}
      </p>
      {view.source !== 'live' ? (
        <p>
          {th
            ? 'ไม่มีหลักฐานความเชื่อมโยงที่บันทึกไว้ในมุมมองย้อนหลัง'
            : 'Historical linkage evidence was not captured.'}
        </p>
      ) : !data ? (
        <p>{th ? 'กำลังอ่านเอกสารที่มีสิทธิ์' : 'Reading authorized evidence…'}</p>
      ) : (
        <>
          {(data.partial || data.limited) && (
            <p className="journey-partial">
              {th
                ? 'ข้อมูลบางส่วนยังอ่านไม่ได้หรือเกินขอบเขต'
                : 'Some evidence is unavailable or outside the bounded view'}
            </p>
          )}
          <ol className="journey-nodes">
            {data.nodes.map((node) => (
              <li key={node.id}>
                <button
                  type="button"
                  onClick={() =>
                    onSelect({
                      type: 'document',
                      doctype: documentTypes[node.document.kind],
                      name: node.document.name,
                    })
                  }
                >
                  {node.document.name} · {node.document.status}
                  {node.document.is_return
                    ? th
                      ? ' · เอกสารคืน'
                      : ' · Return'
                    : node.settled
                      ? th
                        ? ' · ยอดคงค้างเป็นศูนย์'
                        : ' · Zero outstanding'
                      : ''}
                </button>
              </li>
            ))}
          </ol>
          <ul className="journey-edges">
            {data.edges.map((edge) => (
              <li
                key={`${edge.from}:${edge.to}`}
                data-journey-edge={`${edge.from}:${edge.to}`}
                title={edge.evidence}
              >
                {edge.from.split(':').slice(1).join(':')} → {edge.to.split(':').slice(1).join(':')}
              </li>
            ))}
          </ul>
          {!data.nodes.length && (
            <p>{th ? 'ไม่มีรายละเอียดที่มีสิทธิ์อ่าน' : 'Authorized details unavailable'}</p>
          )}
        </>
      )}
    </section>
  )
}
