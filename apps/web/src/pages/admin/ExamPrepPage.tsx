import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { useAction } from '../../app/feedback'
import { ClassReadiness } from '../../features/prep/ClassReadiness'
import { usePrepBank } from '../../features/prep/QuestionBank'
import { allPapers, usePaperName } from '../../features/prep/readiness'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useClasses, useRefresh } from '../../lib/queries'
import { classLabel, ReadinessGroup, SchoolReadinessData } from '../../lib/types'

const SCHOOL_PATH = '/prep/readiness'

export default function ExamPrepPage() {
  const { t, label } = useI18n()
  const paperName = usePaperName()
  const refresh = useRefresh()
  const { busy, run, onSubmit } = useAction()
  const classes = useClasses().data ?? []
  const bank = usePrepBank()
  const school = useQuery({ queryKey: [SCHOOL_PATH], queryFn: () => api<SchoolReadinessData>(SCHOOL_PATH) }).data
  const [classID, setClassID] = useState('')
  const [createdPaper, setCreatedPaper] = useState<{ count: number; examOnly: number } | null>(null)

  useEffect(() => { setClassID((current) => (classes.some((c) => c.id === current) ? current : classes[0]?.id ?? '')) }, [classes])

  const setClassStream = (stream: string) => run(async () => {
    await api(`/prep/classes/${classID}/stream`, { method: 'PUT', body: JSON.stringify({ stream: stream || null }) })
    await refresh(SCHOOL_PATH, `/prep/classes/${classID}/readiness`)
  }, t('prep.streamSaved'))

  const createPaper = onSubmit(async (f) => {
    const result = await api<{ question_count: number; exam_only_count: number }>('/prep/mock-papers', { method: 'POST', body: JSON.stringify({ paper: f.get('paper'), title: f.get('title'), question_count: Number(f.get('question_count')), duration_minutes: Number(f.get('duration_minutes')) }) })
    setCreatedPaper({ count: result.question_count, examOnly: result.exam_only_count })
    await refresh('/assessments')
  })

  const groupRow = (group: ReadinessGroup, name: string) => <tr key={group.key || 'none'}>
    <td><strong>{name}</strong></td>
    <td>{group.students}</td>
    <td>{group.estimated}</td>
    <td>{group.average_predicted === null ? '—' : Math.round(group.average_predicted)}</td>
    <td>{group.above_line}</td>
    <td>{group.near_line}</td>
    <td>{group.at_risk}</td>
  </tr>
  const groupHead = (first: string) => <thead><tr><th>{first}</th><th>{t('readiness.col.students')}</th><th>{t('readiness.col.estimated')}</th><th>{t('readiness.col.average')}</th><th>{t('readiness.status.above_line')}</th><th>{t('readiness.status.near_line')}</th><th>{t('readiness.status.at_risk')}</th></tr></thead>

  return <div className="operations-page">
    <section className="panel">
      <div className="panel-head"><div><p className="eyebrow">{t('readiness.eyebrow')}</p><h2>{t('readiness.schoolTitle')}</h2><p>{t('readiness.schoolHelp', { line: school?.pass_line ?? 300 })}</p></div></div>
      {school && <>
        <div className="table-wrap"><table>{groupHead(t('readiness.col.stream'))}<tbody>{school.streams.map((group) => groupRow(group, group.key ? label('stream', group.key) : t('stream.none')))}</tbody></table></div>
        <div className="table-wrap readiness-classes"><table>{groupHead(t('common.class'))}<tbody>
          {school.classes.map((group) => groupRow(group, `${group.grade_level} · ${group.name}`))}
          {!school.classes.length && <tr><td colSpan={7} className="empty">{t('readiness.noClasses')}</td></tr>}
        </tbody></table></div>
      </>}
      <p className="form-hint">{t('readiness.disclaimerStaff')}</p>
    </section>

    {classes.length > 0 && <section className="panel">
      <div className="panel-head">
        <div><h2>{t('prep.classSetup')}</h2><p>{t('prep.classSetupHelp')}</p></div>
      </div>
      <div className="bank-filters">
        <label className="student-picker">{t('common.class')}<select value={classID} onChange={(e) => setClassID(e.target.value)}>
          {classes.map((c) => <option key={c.id} value={c.id}>{classLabel(c)}</option>)}
        </select></label>
        <label className="student-picker">{t('prep.setClassStream')}<select value="" disabled={busy || !classID} onChange={(e) => { if (e.target.value) void setClassStream(e.target.value === 'clear' ? '' : e.target.value) }}>
          <option value="">{t('prep.chooseStream')}</option>
          <option value="natural">{t('stream.natural')}</option>
          <option value="social">{t('stream.social')}</option>
          <option value="clear">{t('stream.none')}</option>
        </select></label>
      </div>
    </section>}
    {classID && <ClassReadiness key={classID} classID={classID} />}

    <section className="panel">
      <div className="panel-head"><div><h2>{t('prep.mockPaperTitle')}</h2><p>{t('prep.mockPaperHelp')}</p></div></div>
      <form className="stack-form" onSubmit={createPaper}>
        <div className="history-fields">
          <label>{t('bank.paper')}<select name="paper" required defaultValue="">
            <option value="" disabled>{t('bank.choosePaper')}</option>
            {allPapers.map((paper) => {
              const row = bank.find((item) => item.paper === paper)
              return <option key={paper} value={paper}>{paperName(paper)} ({(row?.practice ?? 0) + (row?.exam_only ?? 0)})</option>
            })}
          </select></label>
          <label>{t('assessments.titleField')}<input name="title" required maxLength={200} placeholder={t('prep.mockPaperHint')} /></label>
          <label>{t('prep.questionCount')}<input name="question_count" type="number" min="5" max="100" defaultValue="50" required /></label>
          <label>{t('assessments.duration')}<input name="duration_minutes" type="number" min="5" max="240" defaultValue="120" required /></label>
        </div>
        <button className="primary compact" disabled={busy}>{t('prep.mockPaperCreate')} →</button>
      </form>
      {createdPaper && <p className="invite-success" role="status">
        {t('prep.mockPaperCreated', { n: createdPaper.count, examOnly: createdPaper.examOnly })} <Link to="/admin/assessments">{t('prep.openAssessments')}</Link>
      </p>}
    </section>
  </div>
}
