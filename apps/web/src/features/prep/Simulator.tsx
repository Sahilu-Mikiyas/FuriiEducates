import { ReactNode, useEffect, useState } from 'react'
import { useI18n } from '../../i18n'

export type SimQuestion = { id: string; prompt: string; options: { id: string; text: string }[] }
export type SimFeedback = { is_correct: boolean; correct_option: string; explanation?: string }

const letters = ['A', 'B', 'C', 'D', 'E', 'F']
const LOW_TIME_SECONDS = 300

export const formatClock = (seconds: number) => {
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const rest = String(seconds % 60).padStart(2, '0')
  return hours > 0 ? `${hours}:${String(minutes).padStart(2, '0')}:${rest}` : `${minutes}:${rest}`
}

// Simulator shows one question at a time with the controls of a computer-based
// exam: a clock, a question navigator, flag for review, and a submit screen that
// lists what is unanswered. It keeps no answers itself, so exams, practice and
// the computer-skills lessons can all use it.
export function Simulator({ title, questions, answers, flags = {}, secondsLeft, status, notice, busy = false, feedback, submitLabel, startAt = 0, onIndexChange, onAnswer, onToggleFlag, onSubmit, onConfirmOpen }: {
  title: string
  questions: SimQuestion[]
  answers: Record<string, string | undefined>
  flags?: Record<string, boolean>
  secondsLeft?: number | null
  status?: string
  notice?: ReactNode
  busy?: boolean
  feedback?: (questionID: string) => SimFeedback | null | undefined
  submitLabel?: string
  startAt?: number
  onIndexChange?: (index: number) => void
  onAnswer: (questionID: string, optionID: string) => void
  onToggleFlag?: (questionID: string) => void
  onSubmit: () => void
  onConfirmOpen?: () => void
}) {
  const { t } = useI18n()
  const [index, setIndex] = useState(startAt)
  const [confirming, setConfirming] = useState(false)
  const question = questions[index]

  useEffect(() => { onIndexChange?.(index) }, [index])

  const unanswered = questions.map((q, i) => (answers[q.id] ? -1 : i)).filter((i) => i >= 0)
  const flagged = questions.map((q, i) => (flags[q.id] ? i : -1)).filter((i) => i >= 0)
  const clock = secondsLeft === null || secondsLeft === undefined ? null : formatClock(secondsLeft)
  const goTo = (next: number) => { setConfirming(false); setIndex(next) }
  const label = submitLabel ?? t('sim.finish')
  const result = question ? feedback?.(question.id) : null

  return <div className="sim" role="application" aria-label={title}>
    <header className="sim-head">
      <strong>{title}</strong>
      <span className="sim-progress">{t('sim.answered', { n: questions.length - unanswered.length, total: questions.length })}</span>
      {status && <span className="sim-status">{status}</span>}
      {clock && <span className={`sim-clock ${(secondsLeft ?? 0) < LOW_TIME_SECONDS ? 'low' : ''}`} role="timer">{t('sim.timeLeft', { time: clock })}</span>}
    </header>
    {notice}
    <div className="sim-body">
      {confirming
        ? <section className="sim-confirm">
            <h2>{t('sim.confirmTitle')}</h2>
            <p>{t('sim.confirmSummary', { n: questions.length - unanswered.length, total: questions.length })}</p>
            {unanswered.length > 0 && <div>
              <strong>{t('sim.unanswered', { n: unanswered.length })}</strong>
              <div className="sim-jump">{unanswered.map((i) => <button key={i} type="button" onClick={() => goTo(i)}>{i + 1}</button>)}</div>
            </div>}
            {flagged.length > 0 && <div>
              <strong>{t('sim.flagged', { n: flagged.length })}</strong>
              <div className="sim-jump">{flagged.map((i) => <button key={i} type="button" onClick={() => goTo(i)}>{i + 1}</button>)}</div>
            </div>}
            <p className="form-hint">{t('sim.confirmWarning')}</p>
            <div className="sim-confirm-actions">
              <button type="button" className="secondary" onClick={() => setConfirming(false)}>{t('sim.keepWorking')}</button>
              <button type="button" className="primary compact" disabled={busy} onClick={onSubmit}>{busy ? t('common.saving') : label}</button>
            </div>
          </section>
        : question && <section className="sim-question">
            <p className="sim-number">{t('sim.questionOf', { n: index + 1, total: questions.length })}</p>
            <h2>{question.prompt}</h2>
            <div className="sim-options" role="radiogroup" aria-label={t('sim.options')}>
              {question.options.map((option, position) => {
                const selected = answers[question.id] === option.id
                const state = result ? (option.id === result.correct_option ? 'right' : selected ? 'wrong' : '') : ''
                return <label key={option.id} className={`sim-option ${selected ? 'selected' : ''} ${state}`}>
                  <input type="radio" name={`sim-${question.id}`} checked={selected} disabled={Boolean(result)} onChange={() => onAnswer(question.id, option.id)} />
                  <b>{letters[position]}</b>
                  <span>{option.text}</span>
                </label>
              })}
            </div>
            {result && <div className={`sim-feedback ${result.is_correct ? 'right' : 'wrong'}`} role="status">
              <strong>{result.is_correct ? t('sim.correct') : t('sim.incorrect')}</strong>
              {result.explanation && <p>{result.explanation}</p>}
            </div>}
          </section>}
    </div>
    <footer className="sim-foot">
      <div className="sim-buttons">
        <button type="button" className="secondary" disabled={index === 0 || confirming} onClick={() => goTo(index - 1)}>← {t('sim.previous')}</button>
        {onToggleFlag && question && <button type="button" className={`secondary sim-flag ${flags[question.id] ? 'on' : ''}`} disabled={confirming} aria-pressed={Boolean(flags[question.id])} onClick={() => onToggleFlag(question.id)}>⚑ {flags[question.id] ? t('exam.unflag') : t('exam.flag')}</button>}
        <button type="button" className="secondary" disabled={index >= questions.length - 1 || confirming} onClick={() => goTo(index + 1)}>{t('sim.next')} →</button>
        <button type="button" className="primary compact sim-finish" disabled={confirming} onClick={() => { setConfirming(true); onConfirmOpen?.() }}>{label}</button>
      </div>
      <nav className="sim-nav" aria-label={t('exam.questionsNav')}>
        {questions.map((q, i) => <button key={q.id} type="button" onClick={() => goTo(i)}
          className={`${i === index && !confirming ? 'current' : ''} ${answers[q.id] ? 'done' : ''} ${flags[q.id] ? 'marked' : ''}`}
          aria-current={i === index && !confirming ? 'true' : undefined}
          aria-label={flags[q.id] ? t('exam.goToFlagged', { n: i + 1 }) : t('exam.goTo', { n: i + 1 })}>{i + 1}{flags[q.id] ? '⚑' : ''}</button>)}
      </nav>
    </footer>
  </div>
}
