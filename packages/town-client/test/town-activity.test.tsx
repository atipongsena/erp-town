import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { TownActivityPanel, type TownActivityPanelProps } from '../src/TownActivityPanel.tsx'
import type { DisplayFact } from '../src/world-view.ts'

const delivery: DisplayFact = {
  id: 'delivery-document',
  doctype: 'Delivery Note',
  name: 'DN-VISIBLE',
  kind: 'document',
  scope: '',
  department: 'warehouse',
  status: 'billing',
}
function render(overrides: Partial<TownActivityPanelProps> = {}) {
  return renderToStaticMarkup(
    <TownActivityPanel
      language="en"
      facts={[delivery]}
      live
      replaying={false}
      selectedTruck
      onSelectTruck={() => {}}
      onCloseTruck={() => {}}
      onOpenFact={() => {}}
      {...overrides}
    />,
  )
}

describe('town activity visibility boundaries', () => {
  it('never retains ERP record names, statuses or counts after disconnect', () => {
    const html = render({ live: false })
    expect(html).not.toContain('DN-VISIBLE')
    expect(html).not.toContain('Department work ·')
    expect(html).toContain('Connect to ERP')
    expect(html).toContain('Local test ready')
  })

  it('shows the authorized document status without inventing truck telemetry', () => {
    const html = render()
    expect(html).toContain('DN-VISIBLE')
    expect(html).toContain('Awaiting billing')
    expect(html).toContain('not GPS tracking or proof of arrival')
    expect(html).toContain('Inspect delivery note DN-VISIBLE')
    expect(html).not.toContain('En route')
    expect(html).not.toContain('Delivered')
  })

  it('does not infer a delivery document from an unrelated approval or stock signal', () => {
    const html = render({ facts: [{ ...delivery, kind: 'approval', name: 'PRIVATE-APPROVAL' }] })
    expect(html).not.toContain('PRIVATE-APPROVAL')
    expect(html).toContain('No delivery notes are visible')
  })

  it('shows historical statuses but omits the document action during replay', () => {
    const html = render({ replaying: true })
    expect(html).toContain('DN-VISIBLE')
    expect(html).toContain('Return to live before opening documents')
    expect(html).not.toContain('Inspect delivery note DN-VISIBLE')
  })

  it('keeps truck details out of the collapsed panel', () => {
    const html = render({ selectedTruck: false })
    expect(html).toContain('aria-expanded="false"')
    expect(html).not.toContain('DN-VISIBLE')
    expect(html).toContain('No active people reported')
    expect(html).not.toContain('1 person active')
  })

  it('distinguishes actual AI run status from the local test helper', () => {
    const html = render({
      facts: [
        {
          ...delivery,
          doctype: 'Town Agent Run',
          kind: 'agent_run',
          department: 'it',
          name: 'AI-AUTHORIZED',
          status: 'ai-needs_human',
        },
      ],
    })
    expect(html).toContain('AI-AUTHORIZED')
    expect(html).toContain('ERP run status available')
    expect(html).not.toContain('Local test ready')
    expect(render({ live: false })).not.toContain('AI-AUTHORIZED')
  })

  it('uses Thai copy and escapes document names supplied by ERP', () => {
    const html = render({
      language: 'th',
      facts: [{ ...delivery, name: '<script>danger()</script>' }],
    })
    expect(html).toContain('ดูสถานะรถบรรทุก')
    expect(html).toContain('ไม่ใช่ข้อมูล GPS')
    expect(html).not.toContain('<script>')
    expect(html).toContain('&lt;script&gt;')
  })
})
