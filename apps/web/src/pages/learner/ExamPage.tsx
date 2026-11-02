import { FormEvent, useEffect, useRef, useState } from 'react'
import { useParams } from 'react-router'
import { useDates } from '../../calendar/useDates'
import { requestExamFullscreen, useExamLock } from '../../features/exams/exam-lock'
import { clearPendingAnswers, listPendingAnswers, removePendingAnswer, savePendingAnswer } from '../../features/exams/offlineAnswers'
import { MessageKey, useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useRefresh } from '../../lib/queries'
import type { LearnerAssessment } from '../../lib/types'

type AnswerValue = string | string[]
type SyncState = 'saved' | 'pending' | 'synced' | 'paused' | 'offline' | 'saving' | 'unavailable' | 'deadlineOffline' | 'submitted'

const ASSIGNMENTS_PATH = '/student/assessments'

const syncLabels: Record<SyncState, MessageKey> = {
  saved: 'exam.sync.saved', pending: 'exam.sync.pending', synced: 'exam.sync.synced', paused: 'exam.sync.paused', offline: 'exam.sync.offline',
  saving: 'exam.sync.saving', unavailable: 'exam.sync.unavailable', deadlineOffline: 'exam.sync.deadlineOffline', submitted: 'exam.sync.submitted',
}

// The key gives each assessment a fresh page, so nothing carries over between them.
export default function ExamRoute() {
  const { assignmentID = '' } = useParams()
  return <ExamPage key={assignmentID} assignmentID={assignmentID} />
}

// ExamPage saves every answer on this device first and syncs it to the server,
// so a lost connection or a closed tab cannot lose work.
function ExamPage({ assignmentID }: { assignmentID: string }) {
  const { t, errorText } = useI18n()
  const refresh = useRefresh()
  const [assessment, setAssessment] = useState<LearnerAssessment | null>(null)
  const [answers, setAnswers] = useState<Record<string, AnswerValue>>({})
  const [flags, setFlags] = useState<Record<string, boolean>>({})
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [syncState, setSyncState] = useState<SyncState>('saved')
  const [online, setOnline] = useState(navigator.onLine)
  const [secondsLeft, setSecondsLeft] = useState(0)
  const answerTimers = useRef<Record<string, ReturnType<typeof setTimeout>>>({})
  const answersRef = useRef(answers)
  answersRef.current = answers
  const flushing = useRef(false)
  const deadlineSubmitted = useRef('')
  const revisionRef = useRef<Record<string, number>>({})
  const examLock = useExamLock(assessment?.attempt_id ?? '', assessment?.status === 'in_progress' && Boolean(assessment.locked_mode))

  async function load() {
    setError('')
    try {
      const data = await api<LearnerAssessment>(`/student/assessments/${assignmentID}`)
      setAssessment(data)
      const saved: Record<string, AnswerValue> = {}
      const revisions: Record<string, number> = {}
      const flagged: Record<string, boolean> = {}
      for (const q of data.questions) {
        revisions[q.id] = q.revision || 0
        flagged[q.id] = q.flagged
        if (q.answer !== undefined && q.answer !== null && q.answer !== 'null') {
          saved[q.id] = Array.isArray(q.answer) ? q.answer.map(String) : String(q.answer)
        }
      }
      if (data.attempt_id && data.status === 'in_progress') {
        for (const local of await listPendingAnswers(data.attempt_id)) {
          if (local.revision > (revisions[local.question_id] ?? 0)) {
            revisions[local.question_id] = local.revision
            flagged[local.question_id] = local.flagged
            if (local.answer !== null) saved[local.question_id] = local.answer as AnswerValue
          } else {
            await removePendingAnswer(local.attempt_id, local.question_id, local.idempotency_key)
          }
        }
      }
      setAnswers(saved)
      revisionRef.current = revisions
      setFlags(flagged)
      if (data.status === 'in_progress') setSyncState(online ? 'pending' : 'offline')
    } catch (e) { setError(errorText(e, 'error.load')) }
  }

  useEffect(() => { void load() }, [assignmentID])

  // Pick up extra time or an invigilator finishing the exam without a reload.
  useEffect(() => {
    if (assessment?.status !== 'in_progress') return
    const timer = setInterval(() => {
      void api<LearnerAssessment>(`/student/assessments/${assignmentID}`)
        .then((data) => setAssessment((current) => (!current ? current : data.status !== 'in_progress' ? data : { ...current, deadline_at: data.deadline_at })))
        .catch(() => undefined)
    }, 30000)
    return () => clearInterval(timer)
  }, [assignmentID, assessment?.status])

  useEffect(() => {
    const update = () => setOnline(navigator.onLine)
    window.addEventListener('online', update)
    window.addEventListener('offline', update)
    return () => { window.removeEventListener('online', update); window.removeEventListener('offline', update) }
  }, [])

  async function start(startCode = '') {
    if (assessment?.locked_mode) requestExamFullscreen()
    setBusy(true); setError('')
    try {
      await api(`/student/assessments/${assignmentID}/start`, { method: 'POST', body: JSON.stringify({ start_code: startCode }) })
      await load()
      await refresh(ASSIGNMENTS_PATH)
    } catch (e) { setError(errorText(e, 'error.generic')) } finally { setBusy(false) }
  }

  async function flushPending() {
    if (flushing.current || !online || !assessment?.attempt_id || assessment.status !== 'in_progress') return
    flushing.current = true
    try {
      for (let attempt = 0; attempt < 5; attempt++) {
        const pending = await listPendingAnswers(assessment.attempt_id)
        if (!pending.length) { setSyncState('synced'); break }
        let failed = false
        for (const item of pending) {
          try {
            const saved = await api<{ revision: number }>(`/student/attempts/${item.attempt_id}/answers/${item.question_id}`, { method: 'PUT', body: JSON.stringify({ answer: item.answer, revision: item.revision, idempotency_key: item.idempotency_key, flagged: item.flagged }) })
            await removePendingAnswer(item.attempt_id, item.question_id, item.idempotency_key)
            revisionRef.current = { ...revisionRef.current, [item.question_id]: saved.revision }
          } catch (e) {
            failed = true
            setSyncState(navigator.onLine ? 'paused' : 'offline')
            setError(errorText(e, 'exam.syncWaiting'))
            break
          }
        }
        if (!failed) continue
        if (attempt < 4) await new Promise((resolve) => setTimeout(resolve, Math.min(500 * 2 ** attempt, 8000)))
        else break
      }
    } catch (e) {
      setSyncState('unavailable')
      setError(errorText(e, 'exam.localSaveFailed'))
    } finally { flushing.current = false }
  }

  useEffect(() => {
    if (!online) return
    const reconnect = () => { void flushPending() }
    window.addEventListener('online', reconnect)
    if (assessment?.status === 'in_progress') void flushPending()
    return () => window.removeEventListener('online', reconnect)
  }, [online, assessment?.attempt_id, assessment?.status])

  async function submitAnswers() {
    if (!assessment || !online) { setError(t('exam.reconnectToSubmit')); return }
    Object.values(answerTimers.current).forEach(clearTimeout)
    answerTimers.current = {}
    setBusy(true); setError('')
    try {
      const result = await api<{ auto_score: number; auto_max_score: number; manual_review_count: number; results_released: boolean; final_score: number | null; final_max_score: number | null }>(`/student/attempts/${assessment.attempt_id}/submit`, { method: 'POST', body: JSON.stringify({ answers: answersRef.current }) })
      await clearPendingAnswers(assessment.attempt_id)
      setAssessment({ ...assessment, status: 'submitted', score: result.auto_score, max_score: result.auto_max_score, manual_review_count: result.manual_review_count, results_released: result.results_released, final_score: result.final_score, final_max_score: result.final_max_score })
      setSyncState('submitted')
      await refresh(ASSIGNMENTS_PATH)
    } catch (e) { setError(errorText(e, 'error.generic')) } finally { setBusy(false) }
  }

  function setAnswer(questionID: string, value: AnswerValue | null, flagged = flags[questionID] ?? false) {
    setAnswers((current) => ({ ...current, [questionID]: value ?? '' }))
    if (!assessment?.attempt_id || assessment.status !== 'in_progress') return
    const revision = (revisionRef.current[questionID] ?? 0) + 1
    revisionRef.current = { ...revisionRef.current, [questionID]: revision }
    setFlags((current) => ({ ...current, [questionID]: flagged }))
    setSyncState(online ? 'saving' : 'offline')
    void savePendingAnswer({ attempt_id: assessment.attempt_id, question_id: questionID, answer: value, revision, idempotency_key: crypto.randomUUID(), flagged, saved_at: Date.now() })
      .then(() => {
        if (answerTimers.current[questionID]) clearTimeout(answerTimers.current[questionID])
        answerTimers.current[questionID] = setTimeout(() => void flushPending(), online ? 450 : 1500)
      })
      .catch((e) => setError(errorText(e, 'exam.localSaveFailed')))
  }

  useEffect(() => {
    if (!assessment?.deadline_at || assessment.status !== 'in_progress') return
    const tick = () => {
      const left = Math.max(0, Math.ceil((Date.parse(assessment.deadline_at!) - Date.now()) / 1000))
      setSecondsLeft(left)
      if (left === 0 && online && deadlineSubmitted.current !== assessment.attempt_id) {
        deadlineSubmitted.current = assessment.attempt_id
        void flushPending().finally(() => void submitAnswers())
      }
      if (left === 0 && !online) setSyncState('deadlineOffline')
    }
    tick()
    const timer = setInterval(tick, 1000)
    return () => clearInterval(timer)
  }, [assessment?.deadline_at, assessment?.status, online])

  if (!assessment) return error ? <p className="form-error" role="alert">{error}</p> : <p className="empty" role="status">{t('common.loading')}</p>

  return <>
    {error && <p className="form-error" role="alert">{error}</p>}
    <p className="eyebrow">{assessment.status === 'submitted' ? t('exam.submittedEyebrow') : t('exam.eyebrow')}</p>
    <h1>{assessment.title}</h1>
    {assessment.instructions && <p className="learner-instructions">{assessment.instructions}</p>}
    {assessment.status === 'not_started' && <StartPanel assessment={assessment} busy={busy} onStart={(code) => void start(code)} />}
    {assessment.status === 'in_progress' && <form className="learner-question-form" onSubmit={(e: FormEvent<HTMLFormElement>) => { e.preventDefault(); void submitAnswers() }}>
      {examLock.needsFullscreen && <div className="exam-lock-banner" role="alert">
        <span>{t('exam.lockNotice')}</span>
        <button type="button" className="primary compact" onClick={() => requestExamFullscreen()}>{t('exam.returnFullscreen')}</button>
      </div>}
      <div className="exam-status">
        <span>{online ? t('exam.connected', { state: t(syncLabels[syncState]) }) : t('exam.disconnected', { state: t(syncLabels[syncState]) })}</span>
        <strong>{t('exam.timeLeft', { time: `${Math.floor(secondsLeft / 60)}:${String(secondsLeft % 60).padStart(2, '0')}` })}</strong>
      </div>
      <nav className="exam-question-nav" aria-label={t('exam.questionsNav')}>
        {assessment.questions.map((q, index) => <button type="button" key={q.id} className={flags[q.id] ? 'flagged-question' : ''}
          onClick={() => document.getElementById(`question-${q.id}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' })}
          aria-label={flags[q.id] ? t('exam.goToFlagged', { n: index + 1 }) : t('exam.goTo', { n: index + 1 })}>{index + 1}{flags[q.id] ? ' ⚑' : ''}</button>)}
      </nav>
      <p className="form-hint">{t('exam.storedFirst')}</p>
      {assessment.questions.map((q, index) => <article id={`question-${q.id}`} key={q.id} className="panel learner-question">
        <div className="learner-question-title">
          <span>{t('exam.question', { n: index + 1 })}</span>
          <small>{t('exam.points', { n: q.points })} <button type="button" className={flags[q.id] ? 'flag-toggle flagged' : ''} onClick={() => setAnswer(q.id, answers[q.id] ?? null, !(flags[q.id] ?? false))}>{flags[q.id] ? t('exam.unflag') : t('exam.flag')}</button></small>
        </div>
        <h2>{q.prompt?.text || t('questions.promptFallback')}</h2>
        {(q.question_type === 'single_choice' || q.question_type === 'multiple_choice') && q.options?.map((option) => {
          const selected = answers[q.id]
          const checked = Array.isArray(selected) ? selected.includes(option.id) : selected === option.id
          return <label className="learner-option" key={option.id}>
            <input type={q.question_type === 'single_choice' ? 'radio' : 'checkbox'} name={`answer-${q.id}`} checked={checked} onChange={() => {
              if (q.question_type === 'single_choice') { setAnswer(q.id, option.id); return }
              const current = Array.isArray(selected) ? selected : []
              setAnswer(q.id, checked ? current.filter((x) => x !== option.id) : [...current, option.id])
            }} />
            <span>{option.text}</span>
          </label>
        })}
        {q.question_type === 'numeric' && <label className="learner-text-answer">{t('exam.yourAnswer')}<input type="number" step="any" value={String(answers[q.id] ?? '')} onChange={(e) => setAnswer(q.id, e.target.value)} /></label>}
        {q.question_type === 'text' && <label className="learner-text-answer">{t('exam.yourResponse')}<textarea rows={4} value={String(answers[q.id] ?? '')} onChange={(e) => setAnswer(q.id, e.target.value)} /></label>}
      </article>)}
      <button className="primary learner-submit" disabled={busy || !online}>{busy ? t('exam.submitting') : t('exam.submit')} <span>→</span></button>
    </form>}
    {assessment.status === 'submitted' && <section className="panel learner-result">
      <h2>{assessment.results_released ? t('exam.finalResult') : t('exam.submitted')}</h2>
      {assessment.results_released
        ? <p>{t('exam.finalScore')}: <strong>{assessment.final_score ?? 0} / {assessment.final_max_score ?? 0}</strong></p>
        : <>
            <p>{t('exam.autoScore')}: <strong>{assessment.score ?? 0} / {assessment.max_score ?? 0}</strong></p>
            {assessment.manual_review_count > 0 && <p>{t('exam.waitingReview', { n: assessment.manual_review_count })}</p>}
          </>}
      {assessment.results_released && assessment.questions.filter((q) => q.question_type === 'text' && q.feedback).map((q, index) => <div className="learner-feedback" key={q.id}>
        <strong>{t('exam.writtenLine', { n: index + 1, awarded: q.awarded_points ?? 0, points: q.points })}</strong>
        <p>{q.feedback}</p>
      </div>)}
    </section>}
  </>
}

// StartPanel is shown before the exam timer begins.
function StartPanel({ assessment, busy, onStart }: { assessment: LearnerAssessment; busy: boolean; onStart: (startCode: string) => void }) {
  const { t } = useI18n()
  const { dateTime } = useDates()
  const [code, setCode] = useState('')
  const waitingForSession = assessment.requires_start_code && !assessment.session_open
  return <section className="panel learner-start">
    <p>{t('exam.startIntro', { questions: assessment.question_count, minutes: assessment.duration_minutes })}</p>
    {assessment.due_at && <p>{t('exam.startBefore', { time: dateTime(assessment.due_at) })}</p>}
    {assessment.locked_mode && <p className="form-hint">{t('exam.lockedHint')}</p>}
    {assessment.not_open_yet
      ? <p className="empty">{t('exam.opensAt', { time: dateTime(assessment.opens_at) })}</p>
      : assessment.past_due
        ? <p className="empty">{t('exam.pastDue')}</p>
        : waitingForSession
          ? <p className="empty">{t('exam.waitForInvigilator')}</p>
          : <form className="start-code-form" onSubmit={(event) => { event.preventDefault(); onStart(code) }}>
              {assessment.requires_start_code && <label>{t('exam.startCode')}<input value={code} onChange={(e) => setCode(e.target.value.toUpperCase())} maxLength={6} autoComplete="off" autoCapitalize="characters" required /></label>}
              <button className="primary compact" disabled={busy}>{busy ? t('exam.starting') : t('exam.start')} <span>→</span></button>
            </form>}
  </section>
}
