import { useEffect, useMemo } from 'react'

/** Foreground elapsed time, independent of GPU frame rate, without background catch-up. */
export class WorkClock {
  #last: number | undefined
  reset() {
    this.#last = undefined
  }
  step(now: number, active: boolean): number {
    const last = this.#last
    this.#last = active ? now : undefined
    return active && last !== undefined ? Math.max(0, Math.min((now - last) / 1000, 0.5)) : 0
  }
}
export function useWorkClock(active: boolean): WorkClock {
  const clock = useMemo(() => new WorkClock(), [])
  useEffect(() => {
    if (!active) clock.reset()
    const reset = () => clock.reset()
    document.addEventListener('visibilitychange', reset)
    return () => document.removeEventListener('visibilitychange', reset)
  }, [clock, active])
  return clock
}
