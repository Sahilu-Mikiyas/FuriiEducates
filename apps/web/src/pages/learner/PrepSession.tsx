import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router'
import { fullscreenSupported, requestExamFullscreen } from '../../features/exams/exam-lock'
import { clearPendingAnswers, listPendingAnswers, removePendingAnswer, savePendingAnswer } from '../../features/exams/offlineAnswers'
import { usePaperName } from '../../features/prep/readiness'
import { SimFeedback, Simulator } from '../../features/prep/Simulator'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useRefresh } from '../../lib/queries'
import type { PrepSessionView } from '../../lib/types'
import { PREP_HUB_PATH } from './PrepHome'

const SYNC_RETRY_MS = 5000

export default function PrepSessionRoute() {
  const { sessionID = '' } = useParams()
  return <PrepSessionPage key={sessionID} sessionID={sessionID} />
}

// PrepSessionPage runs one sitting. Practice answers go straight to the server,
// which replies with the explanation. Timed answers save on the device first and
// sync, as they do in school exams, so a dropped connection loses nothing.
function PrepSessionPage({ sessionID }: { sessionID: string }) {
  const { t, label, errorText } = useI18n()
  const paperName = usePaperName()
  const refresh = useRefresh()
  const [session, setSession] = useState<PrepSessionView | null>(null)
  const [answers, setAnswers] = useState<Record<string, string | undefined>>({})
  const [flags, setFlags] = useState<Record<string, boolean>>({})
  const [feedback, setFeedback] = useState<Record<string, SimFeedback>>({})
  const [secondsLeft, setSecondsLeft] = useState<number | null>(null)
  const [online, setOnline] = useState(navigator.onLine)
  const [pendingCount, setPendingCount] = useState(0)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const revision = useRef<Record<string, number>>({})
  const flushing = useRef(false)
  const finishing = useRef(false)
  const answersRef = useRef(answers)
  answersRef.current = answers
  const flagsRef = useRef(flags)
  flagsRef.current = flags
  const timed = session?.mode === 'timed'
  const path = `/student/prep/sessions/${sessionID}`

  async function load() {
    try {
      const view = await api<PrepSessionView>(path)
      const saved: Record<string, string | undefined> = {}
      const marked: Record<string, boolean> = {}
      const known: Record<string, SimFeedback> = {}
      for (const item of view.items) {
        if (item.answer) saved[item.question_id] = item.answer
        marked[item.question_id] = item.flagged
        if (item.correct_option && item.is_correct !== undefined) known[item.question_id] = { is_correct: item.is_correct, correct_option: item.correct_option, explanation: item.explanation }
      }
      if (view.status === 'in_progress' && view.mode === 'timed') {
        for (const local of await listPendingAnswers(sessionID)) {
          revision.current[local.question_id] = local.revision
          marked[local.question_id] = local.flagged
          saved[local.question_id] = (local.answer as string | null) ?? undefined
        }
      }
      setSession(view)
      setAnswers(saved)
      setFlags(marked)
      setFeedback(known)
      if (view.seconds_left !== null) setSecondsLeft(view.seconds_left)
    } catch (e) { setError(errorText(e, 'error.load')) }
  }

  useEffect(() => { void load() }, [sessionID])

  useEffect(() => {
    const update = () => setOnline(navigator.onLine)
    window.addEventListener('online', update)
    window.addEventListener('offline', update)
    return () => { window.removeEventListener('online', update); window.removeEventListener('offline', update) }
  }, [])

  async function flushPending() {
    if (flushing.current || !navigator.onLine) return
    flushing.current = true
    try {
      for (const item of await listPendingAnswers(sessionID)) {
        await api(`${path}/answers/${item.question_id}`, { method: 'PUT', body: JSON.stringify({ answer: item.answer, flagged: item.flagged }) })
        await removePendingAnswer(item.attempt_id, item.question_id, item.idempotency_key)
      }
      setError('')
    } catch (e) {
      if (!(e instanceof TypeError)) setError(errorText(e, 'exam.syncWaiting'))
    } finally {
      flushing.current = false
      setPendingCount((await listPendingAnswers(sessionID).catch(() => [])).length)
    }
  }

  useEffect(() => {
    if (!timed || session?.status !== 'in_progress') return
    void flushPending()
    const timer = setInterval(() => void flushPending(), SYNC_RETRY_MS)
    window.addEventListener('online', flushPending)
    return () => { clearInterval(timer); window.removeEventListener('online', flushPending) }
  }, [timed, session?.status])

  async function finish() {
    if (finishing.current) return
    finishing.current = true
    setBusy(true); setError('')
    try {
      if (timed) {
        await flushPending()
        if ((await listPendingAnswers(sessionID)).length > 0) throw new Error(t('exam.reconnectToSubmit'))
      }
      const view = await api<PrepSessionView>(`${path}/finish`, { method: 'POST', body: '{}' })
      if (timed) await clearPendingAnswers(sessionID)
      setSession(view)
      if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined)
      await refresh(PREP_HUB_PATH, '/student/learning-profile')
    } catch (e) {
      setError(errorText(e, 'error.generic'))
      finishing.current = false
    } finally { setBusy(false) }
  }

  // The server's deadline is the official one. Counting down from the time it
  // reports means a wrong device clock cannot change a student's time.
  useEffect(() => {
    if (!timed || session?.status !== 'in_progress' || session.seconds_left === null) return
    const endsAt = Date.now() + session.seconds_left * 1000
    const tick = () => {
      const left = Math.max(0, Math.ceil((endsAt - Date.now()) / 1000))
      setSecondsLeft(left)
      if (left === 0) void finish()
    }
    tick()
    const timer = setInterval(tick, 1000)
    return () => clearInterval(timer)
  }, [timed, session?.status, session?.seconds_left])

  function saveTimed(questionID: string, answer: string | null, flagged: boolean) {
    const next = (revision.current[questionID] ?? 0) + 1
    revision.current[questionID] = next
    void savePendingAnswer({ attempt_id: sessionID, question_id: questionID, answer, revision: next, idempotency_key: crypto.randomUUID(), flagged, saved_at: Date.now() })
      .then(() => { setPendingCount((count) => count + 1); return flushPending() })
      .catch((e) => setError(errorText(e, 'exam.localSaveFailed')))
  }

  async function answerPractice(questionID: string, optionID: string) {
    setError('')
    setAnswers((current) => ({ ...current, [questionID]: optionID }))
    try {
      const result = await api<SimFeedback & { answer: string }>(`${path}/answers/${questionID}`, { method: 'PUT', body: JSON.stringify({ answer: optionID, flagged: false }) })
      setAnswers((current) => ({ ...current, [questionID]: result.answer }))
      setFeedback((current) => ({ ...current, [questionID]: result }))
    } catch (e) {
      setAnswers((current) => ({ ...current, [questionID]: undefined }))
      setError(errorText(e, 'prep.answerFailed'))
    }
  }

  function onAnswer(questionID: string, optionID: string) {
    if (!timed) { void answerPractice(questionID, optionID); return }
    setAnswers((current) => ({ ...current, [questionID]: optionID }))
    saveTimed(questionID, optionID, flagsRef.current[questionID] ?? false)
  }

  function onToggleFlag(questionID: string) {
    const flagged = !(flagsRef.current[questionID] ?? false)
    setFlags((current) => ({ ...current, [questionID]: flagged }))
    saveTimed(questionID, answersRef.current[questionID] ?? null, flagged)
  }

  if (!session) return error ? <p className="form-error" role="alert">{error}</p> : <p className="empty" role="status">{t('common.loading')}</p>

  const title = `${paperName(session.paper)} · ${label('prep.mode', session.mode)}`

  if (session.status === 'finished') {
    return <section className="panel prep-result">
      <p className="eyebrow">{title}</p>
      <h1>{t('prep.resultTitle')}</h1>
      <p className="prep-score"><strong>{session.correct_count ?? 0}</strong> / {timed ? session.question_count : session.answered_count}{timed && <small> · {t('prep.scoreOutOf100', { n: Math.round(session.score_out_of_100 ?? 0) })}</small>}</p>
      {!timed && session.answered_count < session.question_count && <p className="form-hint">{t('prep.skippedNote', { n: session.question_count - session.answered_count })}</p>}
      <div className="operation-actions"><Link className="primary compact prep-link" to="/learn/prep">{t('prep.backToPrep')} →</Link></div>
      <h2>{t('prep.reviewAnswers')}</h2>
      <div className="prep-review">
        {session.items.filter((item) => timed || item.answer).map((item) => {
          const yours = item.options.find((option) => option.id === item.answer)
          const right = item.options.find((option) => option.id === item.correct_option)
          return <article key={item.question_id} className={item.is_correct ? 'right' : 'wrong'}>
            <small>{t('exam.question', { n: item.position })}{item.topic ? ` · ${item.topic}` : ''}</small>
            <strong>{item.prompt}</strong>
            <p>{t('prep.yourAnswer')}: {yours?.text ?? t('prep.noAnswer')}</p>
            {!item.is_correct && <p>{t('prep.rightAnswer')}: {right?.text}</p>}
            {item.explanation && <p className="form-hint">{item.explanation}</p>}
          </article>
        })}
      </div>
    </section>
  }

  const connection = !timed ? undefined : !online ? t('prep.offlineSaved') : pendingCount > 0 ? t('exam.sync.pending') : t('exam.sync.synced')
  return <Simulator
    title={title}
    questions={session.items.map((item) => ({ id: item.question_id, prompt: item.prompt, options: item.options }))}
    answers={answers}
    flags={timed ? flags : undefined}
    secondsLeft={timed ? secondsLeft : null}
    status={connection}
    busy={busy}
    notice={<>
      {error && <p className="form-error sim-error" role="alert">{error}</p>}
      {timed && !document.fullscreenElement && fullscreenSupported() && <div className="sim-notice"><span>{t('sim.fullscreenHint')}</span><button type="button" className="secondary compact" onClick={() => requestExamFullscreen()}>{t('sim.fullscreen')}</button></div>}
    </>}
    feedback={timed ? undefined : (questionID) => feedback[questionID]}
    onAnswer={onAnswer}
    onToggleFlag={timed ? onToggleFlag : undefined}
    onSubmit={() => void finish()}
  />
}
