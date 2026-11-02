import { useEffect, useState } from 'react'
import { Gradebook } from '../../features/exams/Gradebook'
import { GradingQueue } from '../../features/exams/GradingQueue'
import { useI18n } from '../../i18n'
import { useClasses } from '../../lib/queries'
import { classLabel } from '../../lib/types'

// The grading queue is here too, so results are never stuck when a class has no teacher.
export default function ResultsPage() {
  const { t } = useI18n()
  const classes = useClasses().data ?? []
  const [classID, setClassID] = useState('')
  const [version, setVersion] = useState(0)
  useEffect(() => { setClassID((current) => (classes.some((c) => c.id === current) ? current : classes[0]?.id ?? '')) }, [classes])
  if (!classes.length) return <section className="panel"><p className="empty">{t('results.noClasses')}</p></section>
  return <div className="operations-page">
    <section className="panel"><label className="student-picker">{t('common.class')}<select value={classID} onChange={(e) => setClassID(e.target.value)}>
      {classes.map((c) => <option key={c.id} value={c.id}>{classLabel(c)}</option>)}
    </select></label></section>
    {classID && <Gradebook key={`${classID}:${version}`} classID={classID} />}
    {classID && <GradingQueue key={classID} classID={classID} listPath="/reviews" help={t('grading.adminHelp')}
      gradePath={(review) => `/attempts/${review.attempt_id}/answers/${review.question_id}/grade`} onGraded={() => setVersion((v) => v + 1)} />}
  </div>
}
