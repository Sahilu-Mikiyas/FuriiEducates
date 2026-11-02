import { useI18n } from '../../i18n'
import { useTeacherLearning, useTeacherRoster } from '../../lib/queries'
import { useTeacherContext } from './TeacherLayout'

export default function ClassPage() {
  const { t, label } = useI18n()
  const { classID } = useTeacherContext()
  const rosterQuery = useTeacherRoster(classID)
  const learningQuery = useTeacherLearning(classID)
  const students = rosterQuery.data ?? []
  const rows = learningQuery.data ?? []
  if (!classID) return null
  return <>
    <section className="panel teacher-roster">
      <div className="panel-head">
        <div><h2>{t('admin.title.students')}</h2><p>{rosterQuery.isPending ? t('common.loading') : t('teacher.activeStudents', { n: students.length })}</p></div>
        <span className="count-pill">{t('teacher.learners', { n: students.length })}</span>
      </div>
      <div className="table-wrap"><table>
        <thead><tr><th>{t('common.col.student')}</th><th>{t('students.col.id')}</th><th>{t('teacher.col.language')}</th><th>{t('common.col.status')}</th></tr></thead>
        <tbody>
          {students.map((s) => <tr key={s.id}><td><strong>{s.legal_name}</strong></td><td>{s.student_number}</td><td>{s.preferred_language || '—'}</td><td><span className="status-pill">{label('status', s.status)}</span></td></tr>)}
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
