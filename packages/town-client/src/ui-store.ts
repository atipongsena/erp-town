import { create } from 'zustand'
import type { Language } from './copy.ts'
import type { TownSelection } from './dashboard-view.ts'
import type { SceneTime } from './scene-environment.ts'
import type { Department } from './world-view.ts'

const initialLanguage =
  new URLSearchParams(window.location.search).get('lang') === 'en' ? 'en' : 'th'
export const useUi = create<{
  language: Language
  selected: Department | undefined
  selection: TownSelection | undefined
  following: boolean
  selectObject: (selection: TownSelection | undefined, department?: Department) => void
  follow: (following: boolean) => void
  economy: boolean
  sceneTime: SceneTime
  setLanguage: (language: Language) => void
  select: (department: Department | undefined) => void
  setEconomy: (economy: boolean) => void
  setSceneTime: (sceneTime: SceneTime) => void
}>((set) => ({
  language: initialLanguage,
  selected: undefined,
  selection: undefined,
  following: false,
  selectObject: (selection, selected) => set({ selection, selected, following: false }),
  follow: (following) => set({ following }),
  economy: false,
  sceneTime: 'day',
  setLanguage: (language) => {
    const url = new URL(window.location.href)
    url.searchParams.set('lang', language)
    history.replaceState(null, '', url)
    set({ language })
  },
  select: (selected) =>
    set({
      selected,
      selection: selected ? { type: 'department', department: selected } : undefined,
      following: false,
    }),
  setEconomy: (economy) => set({ economy }),
  setSceneTime: (sceneTime) => set({ sceneTime }),
}))
