import { useQuery } from '@tanstack/react-query'
import { useAction } from '../../app/feedback'
import { useSession } from '../../app/session'
import { AttendanceSummary } from '../../features/attendance/AttendanceSummary'
import { StudentResources } from '../../features/operations/Resources'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useRefresh } from '../../lib/queries'
import type { AttendanceSummaryData, StudentLearningProfile } from '../../lib/types'

const PROFILE_PATH = '/student/learning-profile'

export default function LearnerHome() {
  const { t, label } = useI18n()
  const { identity } = useSession()
  const { busy, run } = useAction()
  const refresh = useRefresh()
  const profile = useQuery({ queryKey: [PROFILE_PATH], queryFn: () => api<StudentLearningProfile>(PROFILE_PATH) }).data
  const attendance = useQuery({ queryKey: ['/student/attendance'], queryFn: () => api<AttendanceSummaryData>('/student/attendance') }).data
  const change = (id: string, action: 'accept' | 'complete') => run(async () => {
    await api(`/student/recommendations/${id}/${action}`, { method: 'POST', body: '{}' })
    await refresh(PROFILE_PATH)
  })
  return <>
    <div className="learner-welcome">
      <p className="eyebrow">{t('learner.eyebrow')}</p>
      <h1>{t('learner.welcome', { name: identity?.display_name.split(' ')[0] ?? '' })}</h1>
      <p className="muted">{t('learner.welcomeHelp')}</p>
    </div>
    {profile && <>
      <section className="panel mastery-panel">
        <div className="panel-head">
          <div><p className="eyebrow">{t('learner.profileEyebrow')}</p><h2>{t('learner.mastery')}</h2><p>{t('learner.masteryHelp')}</p></div>
          <span className="count-pill">{profile.algorithm_version}</span>
        </div>
        <div className="mastery-grid">
          {profile.skills.map((skill) => <article className="mastery-card" key={skill.skill_id}>
            <strong>{skill.title}</strong>
            <span>{t('learner.masteryLine', { n: Math.round(skill.estimated_mastery) })}</span>
            <div className="mastery-bar"><i style={{ width: `${skill.estimated_mastery}%` }} /></div>
            <small>{t('learner.evidenceLine', { n: skill.evidence_count, band: label('confidence', skill.confidence_band) })}</small>
          </article>)}
          {!profile.skills.length && <p className="empty">{t('learner.masteryEmpty')}</p>}
        </div>
      </section>
      <section className="panel recommendation-panel">
        <div className="panel-head"><div><p className="eyebrow">{t('learner.nextEyebrow')}</p><h2>{t('learner.recommendations')}</h2><p>{t('learner.recommendationsHelp')}</p></div></div>
        <div className="recommendation-list">
          {profile.recommendations.map((rec) => <article className="recommendation-card" key={rec.id}>
            <div className="recommendation-head"><strong>{rec.recommended_action.title || rec.skill_title}</strong><span className={`status-pill ${rec.priority}`}>{label('priority', rec.priority)}</span></div>
            <p>{rec.recommended_action.description}</p>
            <small>{rec.skill_title} · {t('learner.recommendationLine', { n: rec.evidence_summary.evidence_count ?? 0, mastery: Math.round(rec.evidence_summary.estimated_mastery ?? 0) })}</small>
            {rec.status === 'pending' && <button className="secondary compact" disabled={busy} onClick={() => void change(rec.id, 'accept')}>{t('learner.accept')}</button>}
            {rec.status === 'accepted' && <button className="secondary compact" disabled={busy} onClick={() => void change(rec.id, 'complete')}>{t('learner.complete')}</button>}
            {rec.change_points !== null && <small>{t('learner.changeLine', { n: `${rec.change_points > 0 ? '+' : ''}${rec.change_points}` })}</small>}
          </article>)}
          {!profile.recommendations.length && <p className="empty">{t('learner.recommendationsEmpty')}</p>}
        </div>
      </section>
    </>}
    {attendance && <AttendanceSummary summary={attendance} />}
    <StudentResources />
  </>
}
