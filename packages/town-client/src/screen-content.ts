import { departmentNames, type Language } from './copy.ts'
import type { DashboardView } from './dashboard-view.ts'
import type { Department } from './world-view.ts'
export type ScreenContent = {
  scope: string
  company: string
  source: DashboardView['source']
  title: string
  lines: string[]
  total: number | undefined
  signature: string
}
export function departmentScreenContent(
  view: DashboardView,
  department: Department,
  language: Language,
): ScreenContent {
  const th = language === 'th'
  const company = view.available ? view.company : ''
  const queues = view.queues.filter((q) => q.department === department)
  const lines = queues.length
    ? queues.map((q) => `${q.doctype}: ${q.total ?? '—'}`)
    : [
        `${th ? 'สัญญาณงาน' : 'Work signals'}: ${view.available ? view.facts.filter((f) => f.department === department).length : '—'}`,
      ]
  const title = departmentNames[language][department]
  const scope = `${company}:${view.source}:${language}`
  const total = view.available
    ? view.facts.filter((f) => f.department === department).length
    : undefined
  return {
    scope,
    company,
    source: view.source,
    title,
    lines,
    total,
    signature: JSON.stringify([scope, title, lines]),
  }
}
