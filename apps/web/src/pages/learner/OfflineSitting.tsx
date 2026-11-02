import { FormEvent, useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router'
import { useSession } from '../../app/session'
import { listPacks, listSittings, OfflinePack, OfflineSitting, saveSitting, syncOfflineSittings } from '../../features/prep/offlineStore'
import { usePaperName } from '../../features/prep/readiness'
import { Simulator } from '../../features/prep/Simulator'
import { useI18n } from '../../i18n'

const DEFAULT_MINUTES = 60

export default function OfflineSittingRoute() {
  const { packID = '' } = useParams()
  const { identity } = useSession()
  return identity ? <OfflineSittingPage key={packID} packID={packID} userID={identity.user_id} /> : null
}

// OfflineSittingPage runs downloaded practice with no connection. The device
// marks it for the student, and every change is saved straight away, so closing
// the app or losing power keeps the work. The server marks it again on upload.
function OfflineSittingPage({ packID, userID }: { packID: string; userID: string }) {
  const { t, errorText } = useI18n()
  const paperName = usePaperName()
  const [pack, setPack] = useState<OfflinePack | null>(null)
  const [sitting, setSitting] = useState<OfflineSitting | null>(null)
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState('')
  const [secondsLeft, setSecondsLeft] = useState<number | null>(null)
  const [uploaded, setUploaded] = useState(false)
  const sittingRef = useRef(sitting)
  sittingRef.current = sitting

  useEffect(() => {
    void (async () => {
      const found = (await listPacks(userID)).find((item) => item.id === packID) ?? null
      setPack(found)
      setSitting((await listSittings(userID)).find((item) => item.pack_id === packID) ?? null)
      setLoaded(true)
    })().catch((e) => { setError(errorText(e, 'error.load')); setLoaded(true) })
  }, [packID, userID, errorText])

  function persist(next: OfflineSitting) {
    setSitting(next)
    void saveSitting(next).catch((e) => setError(errorText(e, 'exam.localSaveFailed')))
  }

  function start(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!pack) return
    const form = new FormData(event.currentTarget)
    const mode = form.get('mode') === 'timed' ? 'timed' : 'drill'
    const minutes = Math.min(240, Math.max(5, Number(form.get('minutes')) || DEFAULT_MINUTES))
    persist({
      client_id: crypto.randomUUID(), pack_id: pack.id, user_id: userID, paper: pack.paper, mode, question_ids: pack.items.map((item) => item.question_id),
      started_at: new Date().toISOString(), deadline: mode === 'timed' ? Date.now() + minutes * 60000 : undefined, answers: {},
    })
  }

  function finish() {
    const current = sittingRef.current
    if (!current || current.finished_at) return
    persist({ ...current, finished_at: Date.now() })
    // saveSitting runs first because its transaction opens first.
    void syncOfflineSittings(userID).then((result) => setUploaded(result.uploaded > 0)).catch(() => undefined)
  }

  useEffect(() => {
    if (!sitting?.deadline || sitting.finished_at) return
    const tick = () => {
      const left = Math.max(0, Math.ceil(((sitting.deadline ?? 0) - Date.now()) / 1000))
      setSecondsLeft(left)
      if (left === 0) finish()
    }
    tick()
    const timer = setInterval(tick, 1000)
    return () => clearInterval(timer)
  }, [sitting?.deadline, sitting?.finished_at])

  const back = <Link className="primary compact prep-link" to="/learn/prep">{t('prep.backToPrep')} →</Link>

  if (!loaded) return <p className="empty" role="status">{t('common.loading')}</p>
  if (!pack) return <section className="panel"><p className="empty">{error || t('offline.packGone')}</p>{back}</section>
  const title = `${paperName(pack.paper)} · ${t('offline.badge')}`

  if (!sitting) {
    return <section className="panel skills-lesson">
      <p className="eyebrow">{t('offline.badge')}</p>
      <h1>{paperName(pack.paper)}</h1>
      <p>{t('offline.startHelp', { n: pack.items.length })}</p>
      <form className="stack-form offline-start" onSubmit={start}>
        <label className="bank-check"><input type="radio" name="mode" value="drill" defaultChecked /> {t('offline.modeDrill')}</label>
        <label className="bank-check"><input type="radio" name="mode" value="timed" /> {t('offline.modeTimed')}</label>
        <label>{t('plans.minutes')}<input name="minutes" type="number" min="5" max="240" defaultValue={DEFAULT_MINUTES} /></label>
        <button className="primary compact">{t('offline.start')} →</button>
      </form>
    </section>
  }

  const timed = sitting.mode === 'timed'
  const answerOf = (questionID: string) => sitting.answers[questionID]?.answer ?? undefined

  if (sitting.finished_at) {
    const counted = pack.items.filter((item) => timed || answerOf(item.question_id))
    const correct = counted.filter((item) => answerOf(item.question_id) === item.correct_option).length
    return <section className="panel prep-result">
      <p className="eyebrow">{title}</p>
      <h1>{t('prep.resultTitle')}</h1>
      <p className="prep-score"><strong>{correct}</strong> / {counted.length}</p>
      <p className="form-hint" role="status">{uploaded ? t('offline.uploadedOne') : t('offline.willUpload')}</p>
      <div className="operation-actions">{back}</div>
      <h2>{t('prep.reviewAnswers')}</h2>
      <div className="prep-review">
        {counted.map((item, index) => {
          const given = answerOf(item.question_id)
          const yours = item.options.find((option) => option.id === given)
          const right = item.options.find((option) => option.id === item.correct_option)
          const isRight = given === item.correct_option
          return <article key={item.question_id} className={isRight ? 'right' : 'wrong'}>
            <small>{t('exam.question', { n: index + 1 })}{item.topic ? ` · ${item.topic}` : ''}</small>
            <strong>{item.prompt}</strong>
            <p>{t('prep.yourAnswer')}: {yours?.text ?? t('prep.noAnswer')}</p>
            {!isRight && <p>{t('prep.rightAnswer')}: {right?.text}</p>}
            {item.explanation && <p className="form-hint">{item.explanation}</p>}
          </article>
        })}
      </div>
    </section>
  }

  const update = (questionID: string, answer: string | null, flagged: boolean) => {
    persist({ ...sitting, answers: { ...sitting.answers, [questionID]: { answer, flagged, answered_at: new Date().toISOString() } } })
  }

  return <Simulator
    title={title}
    questions={pack.items.map((item) => ({ id: item.question_id, prompt: item.prompt, options: item.options }))}
    answers={Object.fromEntries(pack.items.map((item) => [item.question_id, answerOf(item.question_id)]))}
    flags={timed ? Object.fromEntries(pack.items.map((item) => [item.question_id, sitting.answers[item.question_id]?.flagged ?? false])) : undefined}
    secondsLeft={timed ? secondsLeft : null}
    status={t('offline.savedHere')}
    notice={error ? <p className="form-error sim-error" role="alert">{error}</p> : undefined}
    feedback={timed ? undefined : (questionID) => {
      const given = answerOf(questionID)
      const item = pack.items.find((candidate) => candidate.question_id === questionID)
      return given && item ? { is_correct: given === item.correct_option, correct_option: item.correct_option, explanation: item.explanation } : null
    }}
    onAnswer={(questionID, optionID) => {
      // In practice the first answer stands, because the explanation shows at once.
      if (!timed && answerOf(questionID)) return
      update(questionID, optionID, sitting.answers[questionID]?.flagged ?? false)
    }}
    onToggleFlag={timed ? (questionID) => update(questionID, answerOf(questionID) ?? null, !(sitting.answers[questionID]?.flagged ?? false)) : undefined}
    onSubmit={finish}
  />
}
