import { useEffect, useLayoutEffect, useMemo, useState } from 'react'
import { DeliverySimulation, type DeliveryTrip } from './delivery-simulation.ts'
import { DemoMotion } from './demo-motion.ts'
import type { WorkActor } from './live-work.ts'
import { WorkSimulation } from './work-simulation.ts'

export function usePresentation(
  actors: WorkActor[],
  trips: DeliveryTrip[],
  scope: string,
  enabled: boolean,
  demoLoop = false,
  motionEnabled = true,
) {
  const work = useMemo(() => new WorkSimulation(), [])
  const delivery = useMemo(() => new DeliverySimulation(work), [work])
  const loop = useMemo(() => new DemoMotion(), [])
  const loopTrips = useMemo<DeliveryTrip[]>(
    () => [{ id: 'demo-traffic', name: 'DEMO', loop: true }],
    [],
  )
  const looping = enabled && demoLoop
  const [snapshot, setSnapshot] = useState(() => ({
    scope,
    work: work.view(),
    delivery: delivery.view(),
  }))
  useLayoutEffect(() => {
    if (enabled) {
      if (looping) loop.observe(actors, scope)
      else loop.reset()
      const now = Date.now()
      work.observe(looping ? loop.view() : actors, scope, now)
      delivery.observe(looping ? loopTrips : trips, scope, now)
      const paused = looping && (!motionEnabled || document.visibilityState === 'hidden')
      work.setPaused(paused, now)
      delivery.setPaused(paused, now)
    } else {
      loop.reset()
      work.reset()
      delivery.reset()
    }
    setSnapshot({ scope, work: work.view(), delivery: delivery.view() })
  }, [actors, trips, scope, enabled, work, delivery, looping, loop, loopTrips, motionEnabled])
  useLayoutEffect(() => {
    const visibility = () => {
      const paused = looping && (!motionEnabled || document.visibilityState === 'hidden')
      work.setPaused(paused, Date.now())
      delivery.setPaused(paused, Date.now())
      loop.step(Date.now(), work.view(), false)
    }
    document.addEventListener('visibilitychange', visibility)
    return () => document.removeEventListener('visibilitychange', visibility)
  }, [looping, motionEnabled, work, delivery, loop])
  const awake =
    enabled && (looping || actors.some((a) => a.activity !== 'idle') || trips.length > 0)
  useEffect(() => {
    if (!awake) return
    const timer = setInterval(() => {
      const now = Date.now()
      if (looping) {
        const active = motionEnabled && document.visibilityState !== 'hidden'
        loop.step(now, work.view(), active)
        if (!active) return
        work.observe(loop.view(), scope, now)
      } else work.step(now)
      delivery.step(now)
      setSnapshot({ scope, work: work.view(), delivery: delivery.view() })
    }, 250)
    return () => clearInterval(timer)
  }, [awake, scope, work, delivery, looping, motionEnabled, loop])
  return {
    work,
    delivery,
    actors: looping ? snapshot.work.actors.map((a) => a.intent) : actors,
    trips: looping ? loopTrips : trips,
    looping,
    snapshot:
      enabled && snapshot.scope === scope
        ? snapshot
        : {
            scope,
            work: { actors: [], overflow: 0, reservations: [] },
            delivery: { trips: [], overflow: 0 },
          },
  }
}
