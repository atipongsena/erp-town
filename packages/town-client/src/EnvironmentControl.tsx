import type { Language } from './copy.ts'
import { useUi } from './ui-store.ts'

export function EnvironmentControl({ language }: { language: Language }) {
  const { sceneTime, setSceneTime } = useUi()
  const th = language === 'th'
  return (
    <label
      className="town-lighting"
      title={th ? 'เปลี่ยนแสงของฉากเท่านั้น' : 'Changes scene lighting only'}
    >
      {th ? 'แสงในเมือง' : 'Town lighting'}
      <select
        value={sceneTime}
        onChange={(event) => {
          const value = event.target.value
          if (value === 'day' || value === 'sunset' || value === 'night') setSceneTime(value)
        }}
      >
        <option value="day">{th ? 'กลางวัน' : 'Day'}</option>
        <option value="sunset">{th ? 'ยามเย็น' : 'Sunset'}</option>
        <option value="night">{th ? 'กลางคืน' : 'Night'}</option>
      </select>
    </label>
  )
}
