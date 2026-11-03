import { Link, useNavigate } from 'react-router'
import { useAction } from '../../app/feedback'
import { useDates } from '../../calendar/useDates'
import { useI18n } from '../../i18n'
import { api, apiURL } from '../../lib/api'
import type { LearningAction, LearningPlanData, LearningSubject, LearningTopic } from '../../lib/types'
import { usePaperName } from '../prep/readiness'

function Change({ change }: { change: number | null }) {
  const { t } = useI18n()
  if (change === null || change === 0) return null
  return <small className={change > 0 ? 'insight-up' : 'insight-down'}>{change > 0 ? t('learning.up', { n: change }) : t('learning.down', { n: -change })}</small>
}

function SubjectRow({ subject }: { subject: LearningSubject }) {
  const { t } = useI18n()
  return <li>
    <span>{subject.name}</span>
    <strong>{subject.total}</strong>
    <Change change={subject.change} />
    {subject.below_pass && <small className="insight-down">{t('learning.belowPass')}</small>}
    {subject.topics.length > 0 && <small>{t('learning.especially', { topics: subject.topics.slice(0, 2).map((topic) => topic.title).join(', ') })}</small>}
  </li>
}

function TopicRow({ topic }: { topic: LearningTopic }) {
  const { t } = useI18n()
  return <li>
    <span>{topic.title}</span>
    <strong>{Math.round(topic.percent)}%</strong>
    <small>{t('learning.topicLine', { n: topic.responses })}</small>
  </li>
}

function NextStep({ action, readOnly }: { action: LearningAction; readOnly: boolean }) {
  const { t } = useI18n()
  const { date } = useDates()
  const navigate = useNavigate()
  const paperName = usePaperName()
  const { busy, run } = useAction()
  const practice = (mode: 'drill' | 'review') => run(async () => {
    const session = await api<{ id: string }>('/student/prep/sessions', { method: 'POST', body: JSON.stringify({ mode, paper: action.paper, skill_id: action.skill_id ?? null }) })
    navigate(`/learn/prep/session/${session.id}`)
  })
  const button = {
    assignment: <Link className="primary compact prep-link" to={`/learn/assessments/${action.assignment_id}`}>{t('learning.open')}</Link>,
    resume: <Link className="primary compact prep-link" to={`/learn/prep/session/${action.session_id}`}>{t('prep.resume')}</Link>,
    practice: <button className="secondary compact" disabled={busy} onClick={() => void practice('drill')}>{t('learning.start')}</button>,
    review: <button className="secondary compact" disabled={busy} onClick={() => void practice('review')}>{t('learning.start')}</button>,
    resource: <a className="secondary compact" href={apiURL(`/resources/${action.resource_id}/download`)}>{t('resources.download')}</a>,
  }[action.type]
  const text = {
    assignment: [t('learning.action.assignment', { title: action.title ?? '' }), action.due_at ? t('learning.action.due', { date: date(action.due_at) }) : t('learning.action.assignmentHelp')],
    resume: [t('learning.action.resume'), paperName(action.paper ?? '')],
    practice: [t('learning.action.practice', { title: action.title ?? '' }), t('learning.action.practiceHelp')],
    review: [t('learning.action.review', { n: action.count ?? 0 }), paperName(action.paper ?? '')],
    resource: [t('learning.action.resource', { title: action.title ?? '' }), t('learning.action.resourceHelp', { subject: action.subject ?? '' })],
  }[action.type]
  return <li className="next-step">
    <div><strong>{text[0]}</strong><small>{text[1]}</small></div>
    {!readOnly && button}
  </li>
}

// LearningPlan shows what is going well, where there is most room to improve, and
// what to do next. Everyone sees the same plan. Only the student can act on it,
// so `readOnly` leaves the next steps as a list for parents and teachers.
export function LearningPlan({ plan, readOnly = false }: { plan: LearningPlanData; readOnly?: boolean }) {
  const { t } = useI18n()
  const well = plan.going_well.length + plan.strong_topics.length > 0
  const room = plan.room_to_improve.length + plan.weak_topics.length > 0
  const attendance = plan.attendance
  return <>
    {!plan.has_evidence && <p className="empty">{t('learning.none', { n: plan.min_responses })}</p>}
    {(well || room) && <div className="insights">
      {well && <div className="insight-group best">
        <h3>{t('learning.goingWell')}</h3>
        <ul>
          {plan.going_well.map((subject) => <SubjectRow key={subject.name} subject={subject} />)}
          {plan.strong_topics.map((topic) => <TopicRow key={topic.skill_id} topic={topic} />)}
        </ul>
      </div>}
      {room && <div className="insight-group focus">
        <h3>{t('learning.room')}</h3>
        <ul>
          {plan.room_to_improve.map((subject) => <SubjectRow key={subject.name} subject={subject} />)}
          {plan.weak_topics.map((topic) => <TopicRow key={topic.skill_id} topic={topic} />)}
        </ul>
      </div>}
    </div>}
    {attendance && attendance.reason && <p className="report-incomplete learning-note">
      {attendance.reason === 'streak' ? t('learning.attendanceStreak', { n: attendance.current_streak }) : t('learning.attendanceRate', { absent: attendance.days_absent, marked: attendance.days_marked })} {t('learning.attendanceHelp')}
    </p>}
    {attendance && attendance.days_absent === 0 && <p className="learning-good">{t('learning.attendanceFull', { n: attendance.days_marked })}</p>}
    {plan.next.length > 0 && <div className="next-steps">
      <h3 className="attendance-subhead">{readOnly ? t('learning.nextReadOnly') : t('learning.next')}</h3>
      <ul>{plan.next.map((action, index) => <NextStep key={index} action={action} readOnly={readOnly} />)}</ul>
    </div>}
    {plan.report_term && <p className="form-hint learning-source">{t('learning.source', { term: plan.report_term })}</p>}
  </>
}
