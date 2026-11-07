import { Link } from 'react-router'
import { useAction } from '../../app/feedback'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useTeacherLearning, useTeacherRoster } from '../../lib/queries'
import { useTeacherContext } from './TeacherLayout'

export default function ClassPage() {
  const { t, label } = useI18n()
  const { classID, classroom } = useTeacherContext()
  const rosterQuery = useTeacherRoster(classID)
  const learningQuery = useTeacherLearning(classID)
  const students = rosterQuery.data ?? []
  const rows = learningQuery.data ?? []
  if (!classID) return null
  return <>
    {classroom?.homeroom && <HomeroomMessage classID={classID} />}
    <section className="panel teacher-roster">
      <div className="panel-head">
        <div><h2>{t('admin.title.students')}</h2><p>{rosterQuery.isPending ? t('common.loading') : t('teacher.activeStudents', { n: students.length })}</p></div>
        <span className="count-pill">{t('teacher.learners', { n: students.length })}</span>
      </div>
      <div className="table-wrap"><table>
        <thead><tr><th>{t('common.col.student')}</th><th>{t('students.col.id')}</th><th>{t('teacher.col.language')}</th><th>{t('common.col.status')}</th></tr></thead>
        <tbody>
          {students.map((s) => <tr key={s.id}><td><Link to={`/teach/students/${s.id}`}><strong>{s.legal_name}</strong></Link></td><td>{s.student_number}</td><td>{s.preferred_language || '—'}</td><td><span className="status-pill">{label('status', s.status)}</span></td></tr>)}
          {!rosterQuery.isPending && !students.length && <tr><td colSpan={4} className="empty">{t('common.noActiveStudents')}</td></tr>}
        </tbody>
      </table></div>
    </section>
    <section className="panel teacher-review-panel">
      <div className="panel-head"><div><p className="eyebrow">{t('teacher.learningEyebrow')}</p><h2>{t('teacher.skillEvidence')}</h2><p>{t('teacher.skillHelp')}</p></div></div>
      <div className="table-wrap"><table>
        <thead><tr><th>{t('common.col.student')}</th><th>{t('teacher.col.skill')}</th><th>{t('teacher.col.mastery')}</th><th>{t('teacher.col.confidence')}</th><th>{t('teacher.col.plans')}</th></tr></thead>
        <tbody>
          {rows.map((row, index) => <tr key={`${row.student_id}:${row.skill_id || index}`}>
            <td><strong>{row.student_name}</strong><small>{row.student_number}</small></td>
            <td>{row.skill_title || t('teacher.noSkillEvidence')}</td>
            <td>{row.skill_id ? `${Math.round(row.estimated_mastery)}%` : '—'}</td>
            <td>{row.skill_id ? t('teacher.confidenceLine', { band: label('confidence', row.confidence_band), n: row.evidence_count }) : '—'}</td>
            <td>{row.active_recommendations}</td>
          </tr>)}
          {!learningQuery.isPending && !rows.length && <tr><td colSpan={5} className="empty">{t('common.noActiveStudents')}</td></tr>}
        </tbody>
      </table></div>
    </section>
  </>
}

// HomeroomMessage lets the homeroom teacher message the class's students and
// families in the app, and by email or Telegram. SMS stays with the office.
function HomeroomMessage({ classID }: { classID: string }) {
  const { t, label } = useI18n()
  const { busy, onSubmit } = useAction()
  const send = onSubmit(async (f) => {
    await api(`/teacher/classes/${classID}/announcements`, { method: 'POST', body: JSON.stringify({ title: f.get('title'), body: f.get('body'), channels: f.getAll('channels') }) })
  }, t('homeroom.sent'))
  return <section className="panel homeroom-message">
    <div className="panel-head"><div><p className="eyebrow">{t('homeroom.eyebrow')}</p><h2>{t('homeroom.title')}</h2><p>{t('homeroom.help')}</p></div></div>
    <form className="stack-form announce-form" onSubmit={send}>
      <label>{t('messages.title')}<input name="title" maxLength={120} required /></label>
      <label>{t('messages.body')}<textarea name="body" maxLength={2000} rows={3} required /></label>
      <fieldset className="notice-topics">
        <legend>{t('messages.alsoSendBy')}</legend>
        {['email', 'telegram'].map((channel) => <label key={channel} className="bank-check"><input type="checkbox" name="channels" value={channel} /> {label('notifyChannel', channel)}</label>)}
      </fieldset>
      <button className="primary" disabled={busy}>{t('homeroom.send')}<span>→</span></button>
    </form>
  </section>
}
