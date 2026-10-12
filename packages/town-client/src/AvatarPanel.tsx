import type { Appearance, AvatarSession, AvatarView } from './avatar.ts'
import type { Language } from './copy.ts'
export function AvatarPanel({
  language,
  session,
  view,
}: {
  language: Language
  session: AvatarSession
  view: AvatarView
}) {
  if (!view.available) return null
  const th = language === 'th'
  return (
    <section className="avatar-panel" aria-label={th ? 'ตัวละครของฉัน' : 'My avatar'}>
      <label className="avatar-consent">
        <input
          type="checkbox"
          checked={view.preference.consent}
          disabled={view.busy}
          onChange={(e) => {
            void session.setConsent(e.target.checked)
          }}
        />
        {th ? 'แสดงตัวละครของฉัน' : 'Show my avatar'}
      </label>
      <p>
        {th
          ? 'คนอื่นเห็นแผนกและท่าทำงานขณะใช้เอกสาร ไม่มีชื่อหรือประวัติการเดิน ปิดได้ทุกเมื่อ'
          : 'Others see your work department and pose while using documents. No names or movement history. Turn it off anytime.'}
      </p>
      <label>
        {th ? 'รูปลักษณ์' : 'Appearance'}
        <select
          value={view.preference.appearance}
          disabled={view.busy}
          onChange={(e) => {
            void session.setAppearance(e.target.value as Appearance)
          }}
        >
          <option value="human-blue">{th ? 'น้ำเงิน' : 'Blue'}</option>
          <option value="human-coral">{th ? 'ปะการัง' : 'Coral'}</option>
          <option value="human-green">{th ? 'เขียว' : 'Green'}</option>
        </select>
      </label>
      {view.error && (
        <p role="alert">{th ? 'บันทึกไม่สำเร็จ ลองใหม่อีกครั้ง' : 'Could not save. Please try again.'}</p>
      )}
    </section>
  )
}
