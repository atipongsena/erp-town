import { describe, expect, it } from 'vitest'
import { DeliverySimulation } from '../src/delivery-simulation.ts'
import { crossingZones, truckTripLength, truckTripPose } from '../src/work-navigation.ts'
import { WorkSimulation } from '../src/work-simulation.ts'

describe('finite recorded dispatch presentation', () => {
  it('circulates an explicitly marked demo truck continuously without completing another dispatch', () => {
    const sim = new DeliverySimulation()
    sim.observe([{ id: 'demo-traffic', name: 'DEMO', loop: true }], 'A', 0)
    for (let t = 100; t <= 240000; t += 100) sim.step(t)
    expect(sim.trip('demo-traffic')?.phase).toBe('travelling')
    expect(sim.trip('demo-traffic')?.laps).toBeGreaterThan(2)
    expect(sim.trip('demo-traffic')?.pose.arrived).toBe(false)
    expect(sim.view().trips).toHaveLength(1)
    const position = sim.trip('demo-traffic')?.pose
    sim.setPaused(true, 240000)
    sim.step(300000)
    expect(sim.trip('demo-traffic')?.pose).toEqual(position)
    sim.setPaused(false, 300000)
    sim.step(300100)
    expect(sim.trip('demo-traffic')?.pose).not.toEqual(position)
    sim.reset()
    expect(sim.view().trips).toEqual([])
  })
  it('waits for a pedestrian crossing, resumes after release and releases its own reservations', () => {
    const resources = new WorkSimulation()
    for (const zone of crossingZones) resources.reserveCrossing(zone.id, 'pedestrian')
    const sim = new DeliverySimulation(resources)
    sim.observe([{ id: 'a', name: 'DN-a' }], 'A', 0)
    let time = 0
    for (; time < 90000 && !sim.trip('a')?.waiting; time += 100) sim.step(time)
    expect(sim.trip('a')?.waiting).toBe('crossing')
    const distance = sim.trip('a')?.distance
    sim.step(time + 100)
    expect(sim.trip('a')?.distance).toBe(distance)
    resources.releaseCrossings('pedestrian')
    sim.step(time + 200)
    expect(sim.trip('a')?.waiting).toBeUndefined()
    for (time += 300; time < 150000; time += 100) sim.step(time)
    expect(sim.trip('a')?.phase).toBe('finished')
    expect(resources.view().reservations).toEqual([])
  })
  it('preserves FIFO active work under overflow and does not redispatch duplicates', () => {
    const sim = new DeliverySimulation()
    const trips = Array.from({ length: 10 }, (_, i) => ({ id: String(i), name: `DN-${i}` }))
    sim.observe(trips, 'A', 0)
    expect(sim.view().trips).toHaveLength(8)
    expect(sim.view().overflow).toBe(2)
    expect(sim.view().trips[0]?.phase).toBe('departing')
    for (let t = 0; t < 120000; t += 100) sim.step(t)
    expect(sim.view().trips[0]?.phase).toBe('finished')
    sim.observe(trips, 'A', 120000)
    expect(sim.view().trips[0]?.phase).toBe('finished')
  })
  it('cancels missing work, unblocks the next trip and clears scoped history', () => {
    const sim = new DeliverySimulation()
    sim.observe(
      [
        { id: 'a', name: 'DN-A' },
        { id: 'b', name: 'DN-B' },
      ],
      'A',
      0,
    )
    sim.observe([{ id: 'b', name: 'DN-B' }], 'A', 10)
    expect(sim.view().trips[0]?.id).toBe('b')
    expect(sim.view().trips[0]?.phase).toBe('departing')
    sim.reset()
    expect(sim.view().trips).toEqual([])
  })
  it('clamps the endpoint and never calls a business command', () => {
    const sim = new DeliverySimulation()
    sim.observe([{ id: 'a', name: 'DN-A' }], 'A', 0)
    for (let t = 0; t < 120000; t += 100) sim.step(t)
    expect(sim.view().trips[0]).toMatchObject({
      phase: 'finished',
      distance: truckTripLength,
      pose: truckTripPose(truckTripLength),
    })
  })
})
