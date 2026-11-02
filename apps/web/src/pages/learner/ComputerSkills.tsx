import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { useAction } from '../../app/feedback'
import { Simulator, SimQuestion } from '../../features/prep/Simulator'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useRefresh } from '../../lib/queries'
import { PREP_HUB_PATH } from './PrepHome'

const lessonCount = 6
const CLICK_TARGETS = 3
const TIME_DEMO_SECONDS = 15

// ComputerSkills teaches the controls of a computer-based exam by having the
// student use them. Nothing here is graded. The sample questions only exist on
// this page. Next stays locked until the lesson's task is done.
export default function ComputerSkills() {
  const { t } = useI18n()
  const navigate = useNavigate()
  const refresh = useRefresh()
  const { busy, run } = useAction()
  const [lesson, setLesson] = useState(0)
  const [done, setDone] = useState(false)
  const [clicks, setClicks] = useState(0)
  const [answers, setAnswers] = useState<Record<string, string | undefined>>({})
  const [flags, setFlags] = useState<Record<string, boolean>>({})
  const [index, setIndex] = useState(0)
  const [timeLeft, setTimeLeft] = useState(TIME_DEMO_SECONDS)
  const [timeRunning, setTimeRunning] = useState(false)
  const [firstAnswer, setFirstAnswer] = useState('')

  const questions: SimQuestion[] = useMemo(() => [
    { id: 'q1', prompt: t('skills.q1'), options: [{ id: 'a', text: t('skills.q1a') }, { id: 'b', text: t('skills.q1b') }, { id: 'c', text: t('skills.q1c') }, { id: 'd', text: t('skills.q1d') }] },
    { id: 'q2', prompt: t('skills.q2'), options: [{ id: 'a', text: '3' }, { id: 'b', text: '5' }, { id: 'c', text: '7' }, { id: 'd', text: '9' }] },
    { id: 'q3', prompt: t('skills.q3'), options: [{ id: 'a', text: t('skills.q3a') }, { id: 'b', text: t('skills.q3b') }, { id: 'c', text: t('skills.q3c') }, { id: 'd', text: t('skills.q3d') }] },
  ], [t])

  // Each lesson starts clean, so its task means what it says.
  useEffect(() => {
    setDone(false); setClicks(0); setAnswers({}); setFlags({}); setIndex(0); setFirstAnswer('')
    setTimeLeft(TIME_DEMO_SECONDS); setTimeRunning(false)
  }, [lesson])

  useEffect(() => {
    if (!timeRunning) return
    const timer = setInterval(() => setTimeLeft((left) => {
      if (left <= 1) { setTimeRunning(false); setDone(true); return 0 }
      return left - 1
    }), 1000)
    return () => clearInterval(timer)
  }, [timeRunning])

  function chooseAnswer(questionID: string, optionID: string) {
    setAnswers((current) => ({ ...current, [questionID]: optionID }))
    if (lesson !== 2) return
    if (!firstAnswer) setFirstAnswer(optionID)
    else if (optionID !== firstAnswer) setDone(true)
  }

  function clickTarget() {
    const next = clicks + 1
    setClicks(next)
    if (next >= CLICK_TARGETS) setDone(true)
  }

  const finishAll = () => run(async () => {
    await api('/student/prep/onboarding', { method: 'POST', body: '{}' })
    await refresh(PREP_HUB_PATH)
    navigate('/learn/prep')
  })

  const titles = [t('skills.l1'), t('skills.l2'), t('skills.l3'), t('skills.l4'), t('skills.l5'), t('skills.l6')]
  const bodies = [t('skills.l1Body'), t('skills.l2Body'), t('skills.l3Body'), t('skills.l4Body'), t('skills.l5Body'), t('skills.l6Body')]

  return <section className="panel skills-lesson">
    <p className="eyebrow">{t('skills.eyebrow', { n: lesson + 1, total: lessonCount })}</p>
    <h1>{titles[lesson]}</h1>
    <p>{bodies[lesson]}</p>

    {lesson === 0 && <div className="skills-mouse" style={{ justifyContent: ['flex-start', 'center', 'flex-end'][Math.min(clicks, 2)] }}>
      {!done && <button className="primary compact" onClick={clickTarget}>{t('skills.clickMe')}</button>}
      {done && <p className="invite-success" role="status">{t('skills.done')}</p>}
    </div>}

    {lesson === 1 && <div className="skills-scroll" tabIndex={0} onScroll={(e) => {
      const box = e.currentTarget
      if (box.scrollTop + box.clientHeight >= box.scrollHeight - 8) setDone(true)
    }}>
      {[1, 2, 3, 4, 5, 6, 7, 8].map((n) => <p key={n}>{t('skills.scrollLine', { n })}</p>)}
      <p><strong>{t('skills.scrollEnd')}</strong></p>
    </div>}

    {lesson >= 2 && lesson <= 4 && <div className="skills-sim"><Simulator
      key={lesson}
      title={t('skills.simTitle')}
      questions={questions}
      answers={answers}
      flags={flags}
      onAnswer={chooseAnswer}
      onIndexChange={(next) => {
        setIndex(next)
        if (lesson === 3 && next === questions.length - 1) setDone(true)
      }}
      onToggleFlag={(id) => {
        setFlags((current) => ({ ...current, [id]: !current[id] }))
        if (lesson === 4 && id === 'q2') setDone(true)
      }}
      onSubmit={() => undefined}
    /></div>}
    {lesson === 3 && !done && <p className="form-hint">{t('skills.onQuestion', { n: index + 1 })}</p>}

    {lesson === 5 && <div className="skills-sim"><Simulator
      title={t('skills.simTitle')}
      questions={questions.slice(0, 2)}
      answers={answers}
      flags={flags}
      secondsLeft={timeLeft}
      submitLabel={t('skills.finishDemo')}
      onAnswer={chooseAnswer}
      onToggleFlag={(id) => setFlags((current) => ({ ...current, [id]: !current[id] }))}
      onConfirmOpen={() => setDone(true)}
      onSubmit={() => setDone(true)}
    /></div>}
    {lesson === 5 && !timeRunning && timeLeft === TIME_DEMO_SECONDS && <button className="secondary compact" onClick={() => setTimeRunning(true)}>{t('skills.startClock')}</button>}
    {lesson === 5 && timeLeft === 0 && <p className="invite-success" role="status">{t('skills.timeUp')}</p>}

    <div className="skills-foot">
      <Link to="/learn/prep" className="skills-skip">{t('skills.later')}</Link>
      <div className="operation-actions">
        {lesson > 0 && <button className="secondary" onClick={() => setLesson(lesson - 1)}>← {t('sim.previous')}</button>}
        {lesson < lessonCount - 1
          ? <button className="primary compact" disabled={!done} onClick={() => setLesson(lesson + 1)}>{done ? t('sim.next') : t('skills.doTask')} →</button>
          : <button className="primary compact" disabled={!done || busy} onClick={() => void finishAll()}>{t('skills.finishAll')} →</button>}
      </div>
    </div>
  </section>
}
