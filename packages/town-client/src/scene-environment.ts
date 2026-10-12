import tokens from './tokens.json'

export type SceneTime = 'day' | 'sunset' | 'night'

// Visual presets only: changing light never advances ERP work or replay time.
export const sceneEnvironments: Record<
  SceneTime,
  {
    background: string
    sky: string
    ground: string
    ambient: number
    light: string
    intensity: number
    position: [number, number, number]
  }
> = {
  day: {
    background: tokens.world.sea,
    sky: '#fff8ec',
    ground: '#7894ba',
    ambient: 2.3,
    light: '#ffffff',
    intensity: 3.2,
    position: [-45, 100, 55],
  },
  sunset: {
    background: '#c68c87',
    sky: '#ffd5a0',
    ground: '#7e779f',
    ambient: 1.7,
    light: '#ffc982',
    intensity: 2.4,
    position: [-85, 35, 55],
  },
  night: {
    background: '#172b49',
    sky: '#92b8ec',
    ground: '#394a70',
    ambient: 1.2,
    light: '#bacfff',
    intensity: 1.3,
    position: [-45, 100, 55],
  },
}
