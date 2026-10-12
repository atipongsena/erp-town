import { lazy, Suspense } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App.tsx'
import './styles.css'

const DemoApp = lazy(() => import('./DemoApp.tsx').then((m) => ({ default: m.DemoApp })))

const root = document.getElementById('root')
if (!root) throw new Error('Town root is missing')
// Mount one host only: demo playback must never create a live ERP session.
createRoot(root).render(
  import.meta.env.VITE_TOWN_STATIC_DEMO === 'true' ||
    new URLSearchParams(window.location.search).get('mode') === 'demo' ? (
    <Suspense fallback={null}>
      <DemoApp />
    </Suspense>
  ) : (
    <App />
  ),
)
