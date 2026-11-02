import { useQuery } from '@tanstack/react-query'
import { useAction } from '../../app/feedback'
import { useSession } from '../../app/session'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useRefresh } from '../../lib/queries'
import type { ClassReadinessData } from '../../lib/types'
import { heatClass, TotalBadge, usePaperName } from './readiness'

// ClassReadiness shows one class's readiness and weakest topics. Administrators
// can also correct each student's stream here.
export function ClassReadiness({ classID }: { classID: string }) {
  const { t, label } = useI18n()
  const { identity } = useSession()
  const paperName = usePaperName()
  const refresh = useRefresh()
  const { busy, run } = useAction()
  const isAdmin = identity?.roles.includes('school_admin') ?? false
  const path = `/prep/classes/${classID}/readiness`
  const data = useQuery({ queryKey: [path], queryFn: () => api<ClassReadinessData>(path), enabled: Boolean(classID) }).data

  const setStream = (studentID: string, stream: string) => run(async () => {
    await api(`/prep/students/${studentID}/stream`, { method: 'PUT', body: JSON.stringify({ stream: stream || null }) })
    await refresh(path, '/prep/readiness')
  })

  if (!data) return <p className="empty" role="status">{t('common.loading')}</p>
  const papers = (scores: ClassReadinessData['students'][number]) => scores.papers.filter((paper) => paper.score !== null)

  return <>
    <section className="panel">
      <div className="panel-head">
        <div><p className="eyebrow">{t('readiness.eyebrow')}</p><h2>{t('readiness.classTitle')}</h2><p>{t('readiness.classHelp', { line: data.pass_line })}</p></div>
        <span className="count-pill">{t('readiness.atRiskCount', { n: data.at_risk })}</span>
      </div>
      <div className="table-wrap"><table>
        <thead><tr><th>{t('common.col.student')}</th><th>{t('readiness.col.stream')}</th><th>{t('readiness.col.estimate')}</th><th>{t('readiness.col.papers')}</th><th>{t('readiness.col.skills')}</th></tr></thead>
        <tbody>
          {data.students.map((student) => <tr key={student.student_id}>
            <td><strong>{student.student_name}</strong><small>{student.student_number}</small></td>
            <td>{isAdmin
              ? <select className="stream-select" aria-label={t('readiness.streamFor', { name: student.student_name })} value={student.stream} disabled={busy} onChange={(e) => void setStream(student.student_id, e.target.value)}>
                  <option value="">{t('stream.none')}</option>
                  <option value="natural">{t('stream.natural')}</option>
                  <option value="social">{t('stream.social')}</option>
                </select>
              : student.stream ? label('stream', student.stream) : t('stream.none')}</td>
            <td>{student.total ? <TotalBadge total={student.total} /> : <small>{label('readiness.status', student.status)}</small>}</td>
            <td><div className="paper-chips">
              {papers(student).map((paper) => <span key={paper.paper} className={`paper-chip ${paper.enough_evidence ? heatClass(paper.score ?? 0) : 'heat-none'}`} title={t('readiness.responses', { n: paper.responses })}>{paperName(paper.paper)} {paper.enough_evidence ? Math.round(paper.score ?? 0) : '—'}</span>)}
              {!papers(student).length && <small>{t('readiness.noWork')}</small>}
            </div></td>
            <td>{student.onboarded ? '✓' : '—'}</td>
          </tr>)}
          {!data.students.length && <tr><td colSpan={5} className="empty">{t('common.noActiveStudents')}</td></tr>}
        </tbody>
      </table></div>
    </section>
    <section className="panel">
      <div className="panel-head"><div><h2>{t('readiness.topicsTitle')}</h2><p>{t('readiness.topicsHelp')}</p></div></div>
      <div className="heatmap">
        {data.topics.map((topic) => <article className={`heat-cell ${heatClass(topic.percent)}`} key={topic.skill_id}>
          <strong>{topic.title}</strong>
          <small>{paperName(topic.paper)}</small>
          <b>{Math.round(topic.percent)}%</b>
          <small>{t('readiness.topicStudents', { n: topic.responses, students: topic.students })}</small>
        </article>)}
        {!data.topics.length && <p className="empty">{t('readiness.topicsEmpty')}</p>}
      </div>
    </section>
  </>
}
