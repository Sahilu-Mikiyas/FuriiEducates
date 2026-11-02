import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router'
import { useAction } from '../../app/feedback'
import { useSession } from '../../app/session'
import { OfflinePanel } from '../../features/prep/OfflinePanel'
import { PaperBars, TotalBadge, usePaperName } from '../../features/prep/readiness'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import type { PrepHub, PrepMode } from '../../lib/types'

export const PREP_HUB_PATH = '/student/prep'

export default function PrepHome() {
  const { t, label } = useI18n()
  const navigate = useNavigate()
  const paperName = usePaperName()
  const { busy, run } = useAction()
  const { identity } = useSession()
  const query = useQuery({ queryKey: [PREP_HUB_PATH], queryFn: () => api<PrepHub>(PREP_HUB_PATH), networkMode: 'always', retry: 1, meta: { silent: true } })
  const hub = query.data

  const startPractice = (mode: PrepMode, paper: string, skillID?: string) => run(async () => {
    const session = await api<{ id: string }>('/student/prep/sessions', { method: 'POST', body: JSON.stringify({ mode, paper, skill_id: skillID ?? null }) })
    navigate(`/learn/prep/session/${session.id}`)
  })
  const startMock = () => run(async () => {
    await api('/student/prep/mocks', { method: 'POST', body: '{}' })
    await query.refetch()
  })
  const startMockPaper = (mockID: string, paper: string) => run(async () => {
    const session = await api<{ id: string }>(`/student/prep/mocks/${mockID}/papers`, { method: 'POST', body: JSON.stringify({ paper }) })
    navigate(`/learn/prep/session/${session.id}`)
  })

  if (!hub && query.isPending) return <p className="empty" role="status">{t('common.loading')}</p>
  // With no connection, downloaded practice still works.
  if (!hub) {
    return <>
      <div className="learner-welcome">
        <p className="eyebrow">{t('prep.eyebrow')}</p>
        <h1>{t('offline.unreachableTitle')}</h1>
        <p className="muted">{t('offline.unreachableHelp')}</p>
      </div>
      {identity && <OfflinePanel userID={identity.user_id} papers={[]} hubPath={PREP_HUB_PATH} />}
    </>
  }

  const readiness = hub.readiness
  const openMock = hub.mocks.find((mock) => mock.status === 'in_progress')
  const finishedMocks = hub.mocks.filter((mock) => mock.status === 'finished')
  const hasQuestions = hub.papers.some((paper) => paper.available > 0)

  return <>
    <div className="learner-welcome">
      <p className="eyebrow">{t('prep.eyebrow')}</p>
      <h1>{t('prep.title')}</h1>
      <p className="muted">{t('prep.help')}</p>
    </div>

    {!hub.onboarded && <section className="panel prep-callout">
      <div><h2>{t('prep.onboardTitle')}</h2><p>{t('prep.onboardHelp')}</p></div>
      <Link className="primary compact prep-link" to="/learn/prep/computer-skills">{t('prep.onboardStart')} →</Link>
    </section>}

    {hub.active_session.id && <section className="panel prep-callout">
      <div>
        <h2>{t('prep.resumeTitle')}</h2>
        <p>{label('prep.mode', hub.active_session.mode ?? '')} · {paperName(hub.active_session.paper ?? '')}</p>
      </div>
      <Link className="primary compact prep-link" to={`/learn/prep/session/${hub.active_session.id}`}>{t('prep.resume')} →</Link>
    </section>}

    <section className="panel">
      <div className="panel-head">
        <div><p className="eyebrow">{t('readiness.eyebrow')}</p><h2>{t('readiness.title')}</h2></div>
        {readiness.total && <TotalBadge total={readiness.total} />}
      </div>
      {readiness.status === 'no_stream' && <p className="form-hint">{t('readiness.noStream')}</p>}
      {readiness.status === 'need_more_evidence' && <p className="form-hint">{t('readiness.needMore', { n: readiness.min_responses })}</p>}
      {readiness.total && <div className="readiness-total">
        <strong>{Math.round(readiness.total.predicted)}<small> / 600</small></strong>
        <div>
          <p>{t('readiness.rangeLine', { low: Math.round(readiness.total.low), high: Math.round(readiness.total.high) })}</p>
          <p>{readiness.total.distance_to_pass_line >= 0
            ? t('readiness.aboveLine', { n: Math.round(readiness.total.distance_to_pass_line), line: readiness.total.pass_line })
            : t('readiness.belowLine', { n: Math.round(-readiness.total.distance_to_pass_line), line: readiness.total.pass_line })}</p>
        </div>
      </div>}
      <PaperBars papers={readiness.papers} minResponses={readiness.min_responses} />
      {readiness.weak_topics.length > 0 && <div className="weak-topics">
        <h3>{t('readiness.weakTopics')}</h3>
        {readiness.weak_topics.map((topic) => <div className="weak-topic" key={topic.skill_id}>
          <span><strong>{topic.title}</strong><small>{paperName(topic.paper)} · {t('readiness.topicLine', { percent: Math.round(topic.percent), n: topic.responses })}</small></span>
          <button className="secondary compact" disabled={busy} onClick={() => void startPractice('drill', topic.paper, topic.skill_id)}>{t('prep.practiceTopic')}</button>
        </div>)}
      </div>}
      <p className="form-hint">{t('readiness.disclaimer')}</p>
    </section>

    <section className="panel">
      <div className="panel-head"><div><h2>{t('prep.practiceTitle')}</h2><p>{t('prep.practiceHelp')}</p></div></div>
      {!hasQuestions && <p className="empty">{t('prep.noQuestions')}</p>}
      <div className="prep-papers">
        {hub.papers.map((paper) => <article className="prep-paper" key={paper.paper}>
          <div><strong>{paperName(paper.paper)}</strong><small>{t('prep.available', { n: paper.available })}</small></div>
          <div className="prep-paper-actions">
            <button className="secondary compact" disabled={busy || !paper.available} onClick={() => void startPractice('drill', paper.paper)}>{t('prep.mode.drill')}</button>
            <button className="secondary compact" disabled={busy || !paper.available} onClick={() => void startPractice('weak', paper.paper)}>{t('prep.mode.weak')}</button>
            <button className="secondary compact" disabled={busy || !paper.due_review} onClick={() => void startPractice('review', paper.paper)}>{t('prep.reviewCount', { n: paper.due_review })}</button>
            <button className="secondary compact" disabled={busy || !paper.available} onClick={() => void startPractice('timed', paper.paper)}>{t('prep.mode.timed')}</button>
          </div>
        </article>)}
      </div>
    </section>

    {identity && <OfflinePanel userID={identity.user_id} papers={hub.papers} hubPath={PREP_HUB_PATH} />}

    <section className="panel">
      <div className="panel-head">
        <div><h2>{t('prep.mockTitle')}</h2><p>{t('prep.mockHelp')}</p></div>
        {!openMock && hub.stream && <button className="primary compact" disabled={busy || !hasQuestions} onClick={() => void startMock()}>{t('prep.mockStart')}</button>}
      </div>
      {!hub.stream && <p className="form-hint">{t('readiness.noStream')}</p>}
      {openMock && <div className="prep-papers">
        {openMock.papers.map((paper) => <article className="prep-paper" key={paper.paper}>
          <div><strong>{paperName(paper.paper)}</strong><small>{label('status', paper.status)}{paper.score !== undefined ? ` · ${Math.round(paper.score)} / 100` : ''}</small></div>
          {paper.status !== 'finished' && <button className="primary compact" disabled={busy} onClick={() => void startMockPaper(openMock.id, paper.paper)}>{paper.status === 'in_progress' ? t('prep.resume') : t('prep.mockPaperStart')}</button>}
        </article>)}
      </div>}
      {finishedMocks.length > 0 && <div className="prep-mock-history">
        <h3>{t('prep.mockHistory')}</h3>
        {finishedMocks.map((mock) => <p key={mock.id}><strong>{Math.round(mock.total ?? 0)} / 600</strong></p>)}
      </div>}
    </section>
  </>
}
