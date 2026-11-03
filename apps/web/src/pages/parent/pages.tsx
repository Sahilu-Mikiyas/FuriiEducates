import { useQuery } from '@tanstack/react-query'
import { Link, Navigate, useParams } from 'react-router'
import { useDates } from '../../calendar/useDates'
import { ChangePasswordPanel, PreferencesPanel } from '../../features/account/account'
import { AttendanceSummary } from '../../features/attendance/AttendanceSummary'
import { LearningPlan } from '../../features/learning/LearningPlan'
import { PaperBars, TotalBadge, usePaperName } from '../../features/prep/readiness'
import { ReportCardView, usePrint } from '../../features/reports/ReportCard'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { money } from '../../lib/money'
import { ChildData, ChildSummary, classLabel, LearningPlanData, ReportCardData } from '../../lib/types'

export function ParentAccountPage() {
  return <><PreferencesPanel /><ChangePasswordPanel /></>
}

const CHILDREN_PATH = '/parent/children'

// A parent with one child goes straight to that child.
export function ParentHome() {
  const { t } = useI18n()
  const query = useQuery({ queryKey: [CHILDREN_PATH], queryFn: () => api<ChildSummary[]>(CHILDREN_PATH) })
  const children = query.data ?? []
  if (children.length === 1) return <Navigate to={`/family/children/${children[0].id}`} replace />
  return <>
    <div className="learner-welcome"><h1>{t('parent.childrenTitle')}</h1><p className="muted">{t('parent.childrenHelp')}</p></div>
    {query.isSuccess && !children.length && <section className="panel"><p className="empty">{t('parent.noChildren')}</p></section>}
    <div className="child-list">
      {children.map((child) => <Link key={child.id} className="child-card" to={`/family/children/${child.id}`}>
        <strong>{child.legal_name}</strong>
        <small>{child.class_name ? classLabel({ grade_level: child.grade_level, name: child.class_name }) : child.student_number}</small>
        <span>{t('parent.absentLine', { n: child.attendance.absent })}</span>
        {child.latest_average !== null && <span>{t('parent.averageLine', { term: child.latest_term, n: child.latest_average })}</span>}
      </Link>)}
    </div>
  </>
}

// HowTheyAreDoing shows the latest published figures and the child's learning
// plan, which compares them only with themself.
function HowTheyAreDoing({ card, name, plan }: { card: ReportCardData; name: string; plan: LearningPlanData }) {
  const { t } = useI18n()
  return <section className="panel">
    <div className="panel-head"><div><h2>{t('parent.doingTitle', { name })}</h2><p>{t('parent.doingHelp', { term: card.term_name })}</p></div></div>
    <div className="attendance-counts child-figures">
      <div className="attendance-count"><strong>{card.average ?? '—'}</strong><span>{t('reports.average')}</span></div>
      <div className="attendance-count"><strong>{card.grade || '—'}</strong><span>{t('reports.grade')}</span></div>
      <div className="attendance-count"><strong>{card.rank ? t('reports.rankOf', { rank: card.rank, n: card.students_ranked }) : '—'}</strong><span>{t('reports.rank')}</span></div>
    </div>
    <LearningPlan plan={plan} readOnly />
  </section>
}

function Fees({ fees }: { fees: ChildData['fees'] }) {
  const { t, label } = useI18n()
  const { date } = useDates()
  return <section className="panel">
    <div className="panel-head"><div><h2>{t('parent.feesTitle')}</h2><p>{t('parent.feesHelp')}</p></div></div>
    {!fees.entries.length && <p className="empty">{t('parent.feesEmpty')}</p>}
    {fees.balances.length > 0 && <div className="attendance-counts child-figures">
      {fees.balances.map((balance) => <div key={balance.currency_code} className={`attendance-count ${balance.due_minor > 0 ? 'late' : 'present'}`}>
        <strong>{money(balance.due_minor, balance.currency_code)}</strong>
        <span>{t('parent.feesDue')}</span>
      </div>)}
    </div>}
    {fees.entries.length > 0 && <div className="table-wrap"><table>
      <thead><tr><th>{t('finance.col.date')}</th><th>{t('finance.col.type')}</th><th>{t('finance.col.detail')}</th><th>{t('finance.col.amount')}</th></tr></thead>
      <tbody>
        {fees.entries.map((entry, index) => <tr key={index}>
          <td>{date(entry.paid_on ?? entry.due_on ?? entry.recorded_on)}</td>
          <td>{label('finance.type', entry.entry_type)}</td>
          <td className="fee-detail">{entry.category}<small>{entry.description}</small></td>
          <td>{money(entry.amount_minor, entry.currency_code)}</td>
        </tr>)}
      </tbody>
    </table></div>}
  </section>
}

function Readiness({ readiness }: { readiness: NonNullable<ChildData['readiness']> }) {
  const { t } = useI18n()
  const paperName = usePaperName()
  return <section className="panel">
    <div className="panel-head">
      <div><h2>{t('parent.readinessTitle')}</h2><p>{t('parent.readinessHelp')}</p></div>
      {readiness.total && <TotalBadge total={readiness.total} />}
    </div>
    {readiness.status === 'need_more_evidence' && <p className="form-hint">{t('parent.needMore', { n: readiness.min_responses })}</p>}
    {readiness.total && <div className="readiness-total">
      <strong>{Math.round(readiness.total.predicted)}<small> / 600</small></strong>
      <div><p>{t('readiness.rangeLine', { low: Math.round(readiness.total.low), high: Math.round(readiness.total.high) })}</p></div>
    </div>}
    <PaperBars papers={readiness.papers} minResponses={readiness.min_responses} />
    {readiness.weak_topics.length > 0 && <div className="weak-topics">
      <h3>{t('readiness.weakTopics')}</h3>
      {readiness.weak_topics.map((topic) => <div className="weak-topic" key={topic.skill_id}>
        <span><strong>{topic.title}</strong><small>{paperName(topic.paper)} · {t('readiness.topicLine', { percent: Math.round(topic.percent), n: topic.responses })}</small></span>
      </div>)}
    </div>}
  </section>
}

export function ChildPage() {
  const { t, errorText } = useI18n()
  const { studentID = '' } = useParams()
  const { print, area } = usePrint<ReportCardData>()
  const path = `${CHILDREN_PATH}/${studentID}`
  const query = useQuery({ queryKey: [path], queryFn: () => api<ChildData>(path), meta: { silent: true } })
  const others = useQuery({ queryKey: [CHILDREN_PATH], queryFn: () => api<ChildSummary[]>(CHILDREN_PATH) }).data ?? []
  if (query.error) return <section className="panel"><p className="row-error">{errorText(query.error, 'error.load')}</p></section>
  if (!query.data) return <p className="empty">{t('common.loading')}</p>
  const { student, attendance, report_cards: cards, fees, readiness, learning_plan: plan } = query.data
  const firstName = student.legal_name.split(' ')[0]
  return <>
    <div className="learner-welcome">
      {others.length > 1 && <Link className="profile-back" to="/family/children">← {t('parent.allChildren')}</Link>}
      <h1>{student.legal_name}</h1>
      <p className="muted">{student.student_number}{student.class_name && ` · ${classLabel({ grade_level: student.grade_level, name: student.class_name })}`}</p>
    </div>
    <div className="operations-page">
      {cards.length > 0
        ? <HowTheyAreDoing card={cards[0]} name={firstName} plan={plan} />
        : <section className="panel"><div className="panel-head"><div><h2>{t('parent.doingTitle', { name: firstName })}</h2></div></div>{plan.has_evidence ? <LearningPlan plan={plan} readOnly /> : <p className="empty">{t('parent.noReportYet')}</p>}</section>}
      <AttendanceSummary summary={attendance} />
      {readiness && <Readiness readiness={readiness} />}
      {cards.map((card) => <section className="panel" key={card.term_id}>
        <div className="panel-head"><div><h2>{card.term_name} · {card.year_name}</h2></div><button className="secondary compact" onClick={() => print(card)}>{t('reports.printCard')}</button></div>
        <ReportCardView card={card} />
      </section>)}
      <Fees fees={fees} />
    </div>
    {area((card) => <ReportCardView card={card} />)}
  </>
}
