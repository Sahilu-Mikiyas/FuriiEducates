import { useQuery } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useAction } from '../../app/feedback'
import { DateField } from '../../calendar/DateField'
import { useDates } from '../../calendar/useDates'
import { MessageKey, useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useClasses, useRefresh } from '../../lib/queries'
import type { LessonPlan } from '../../lib/types'
import { ResourceList, useTeacherResources } from './Resources'

export function LessonPlanReview() {
  const { t, label } = useI18n()
  const { date } = useDates()
  const { busy, run } = useAction()
  const refresh = useRefresh()
  const classes = useClasses().data ?? []
  const plans = useQuery({ queryKey: ['/lesson-plans'], queryFn: () => api<LessonPlan[]>('/lesson-plans') }).data ?? []
  const review = (plan: LessonPlan, status: 'approved' | 'needs_revision') => run(async () => {
    await api(`/lesson-plans/${plan.id}/review`, { method: 'PATCH', body: JSON.stringify({ status }) })
    await refresh('/lesson-plans')
  })
  return <section className="panel">
    <div className="panel-head"><div><h2>{t('plans.reviewTitle')}</h2><p>{t('plans.reviewHelp')}</p></div><span className="count-pill">{t('plans.submittedCount', { n: plans.filter((p) => p.status === 'submitted').length })}</span></div>
    {plans.map((p) => {
      const classroom = classes.find((c) => c.id === p.class_id)
      return <article className="operation-card" key={p.id}>
        <div>
          <strong>{p.topic}</strong>
          <p>{p.teacher_name ? `${p.teacher_name} · ` : ''}{p.grade_level || classroom?.grade_level} · {p.class_name || classroom?.name} · {p.subject} · {date(p.lesson_date)} · {t('common.minutesShort', { n: p.duration_minutes })}</p>
          <p><b>{t('plans.objectives')}:</b> {p.objectives}</p>
          <p><b>{t('plans.assessment')}:</b> {p.assessment || t('common.notProvided')}</p>
          <span className="status-pill">{label('status', p.status)}</span>
        </div>
        {p.status === 'submitted' && <div className="operation-actions">
          <button className="secondary compact" disabled={busy} onClick={() => void review(p, 'needs_revision')}>{t('plans.requestRevision')}</button>
          <button className="primary compact" disabled={busy} onClick={() => void review(p, 'approved')}>{t('plans.approve')}</button>
        </div>}
      </article>
    })}
    {!plans.length && <p className="empty">{t('plans.reviewEmpty')}</p>}
  </section>
}

// The stored phase names stay in English because saved plans match on them.
const phases: { key: string; stored: string; title: MessageKey }[] = [
  { key: 'engage', stored: 'Engage', title: 'plans.phase.engage' },
  { key: 'teach', stored: 'Teach & model', title: 'plans.phase.teach' },
  { key: 'practice', stored: 'Guided & independent practice', title: 'plans.phase.practice' },
  { key: 'check', stored: 'Check for understanding', title: 'plans.phase.check' },
]

// TeacherPlanning is where a teacher plans lessons and shares files for one class.
export function TeacherPlanning({ classID }: { classID: string }) {
  const { t, label } = useI18n()
  const { date } = useDates()
  const { busy, run, onSubmit } = useAction()
  const refresh = useRefresh()
  const [editing, setEditing] = useState<LessonPlan | null>(null)
  const formArea = useRef<HTMLDivElement>(null)
  const plans = (useQuery({ queryKey: ['/teacher/lesson-plans'], queryFn: () => api<LessonPlan[]>('/teacher/lesson-plans') }).data ?? []).filter((p) => p.class_id === classID)
  const resources = useTeacherResources(classID)

  useEffect(() => setEditing(null), [classID])
  useEffect(() => { if (editing) formArea.current?.scrollIntoView({ behavior: 'smooth', block: 'start' }) }, [editing])

  const savePlan = (form: HTMLFormElement, status: 'draft' | 'submitted') => run(async () => {
    const f = new FormData(form)
    const body = {
      class_id: classID, subject: f.get('subject'), lesson_date: f.get('lesson_date'), duration_minutes: Number(f.get('duration_minutes')),
      topic: f.get('topic'), objectives: f.get('objectives'), prior_knowledge: f.get('prior_knowledge'), materials: f.get('materials'),
      lesson_phases: phases.map((phase) => ({ phase: phase.stored, minutes: Number(f.get(`${phase.key}_minutes`) || 0), activities: f.get(phase.key) })),
      differentiation: f.get('differentiation'), assessment: f.get('assessment'), homework: f.get('homework'), reflection: editing?.reflection || '', status,
    }
    await api(editing ? `/teacher/lesson-plans/${editing.id}` : '/teacher/lesson-plans', { method: editing ? 'PUT' : 'POST', body: JSON.stringify(body) })
    form.reset()
    setEditing(null)
    await refresh('/teacher/lesson-plans')
  }, status === 'submitted' ? t('plans.submitted') : t('plans.draftSaved'))

  const upload = onSubmit(async (values) => {
    await api(`/teacher/classes/${classID}/resources`, { method: 'POST', body: values })
    await refresh(`/teacher/classes/${classID}/resources`)
  }, t('resources.uploaded'))

  const phaseOf = (stored: string) => editing?.lesson_phases.find((phase) => phase.phase === stored)

  return <div className="operations-page">
    <section className="panel">
      <div className="panel-head"><div><p className="eyebrow">{t('plans.eyebrow')}</p><h2>{t('plans.title')}</h2><p>{t('plans.help')}</p></div></div>
      <div ref={formArea}>
        <h3>{editing ? t('plans.editing', { topic: editing.topic }) : t('plans.new')}</h3>
        {/* The key rebuilds the form with the chosen plan's values. */}
        <form key={editing?.id ?? 'new'} className="stack-form" onSubmit={(e) => { e.preventDefault(); void savePlan(e.currentTarget, 'draft') }}>
          <div className="history-fields">
            <label>{t('plans.subject')}<input name="subject" required defaultValue={editing?.subject} /></label>
            <DateField name="lesson_date" label={t('plans.lessonDate')} required defaultValue={editing?.lesson_date ?? new Date().toISOString().slice(0, 10)} />
            <label>{t('plans.duration')}<input name="duration_minutes" type="number" min="5" max="480" defaultValue={editing?.duration_minutes ?? 45} required /></label>
            <label>{t('plans.topic')}<input name="topic" required defaultValue={editing?.topic} /></label>
            <label className="span-all">{t('plans.objectivesLong')}<textarea name="objectives" required rows={2} defaultValue={editing?.objectives} /></label>
            <label>{t('plans.priorKnowledge')}<input name="prior_knowledge" defaultValue={editing?.prior_knowledge} /></label>
            <label>{t('plans.materials')}<input name="materials" placeholder={t('plans.materialsHint')} defaultValue={editing?.materials} /></label>
          </div>
          <h4>{t('plans.sequence')}</h4>
          <div className="history-fields">
            {phases.map((phase) => <fieldset className="phase-field" key={phase.key}>
              <legend>{t(phase.title)}</legend>
              <label>{t('plans.minutes')}<input name={`${phase.key}_minutes`} type="number" min="0" defaultValue={phaseOf(phase.stored)?.minutes ?? 5} /></label>
              <label>{t('plans.activities')}<textarea name={phase.key} rows={2} defaultValue={phaseOf(phase.stored)?.activities} /></label>
            </fieldset>)}
          </div>
          <div className="history-fields">
            <label>{t('plans.differentiation')}<textarea name="differentiation" rows={2} defaultValue={editing?.differentiation} /></label>
            <label>{t('plans.assessmentLong')}<textarea name="assessment" rows={2} defaultValue={editing?.assessment} /></label>
            <label>{t('plans.homework')}<textarea name="homework" rows={2} defaultValue={editing?.homework} /></label>
          </div>
          <div className="operation-actions">
            <button className="secondary" disabled={busy} type="submit">{t('plans.saveDraft')}</button>
            {editing && <button className="secondary" type="button" onClick={() => setEditing(null)}>{t('plans.cancelEdit')}</button>}
            <button className="primary" disabled={busy} type="button" onClick={(e) => { const form = e.currentTarget.form!; if (form.reportValidity()) void savePlan(form, 'submitted') }}>{t('plans.submitForReview')} →</button>
          </div>
        </form>
      </div>
    </section>
    <section className="panel">
      <div className="panel-head"><div><h2>{t('plans.mine')}</h2><p>{t('plans.mineHelp')}</p></div></div>
      {plans.map((p) => <article className="operation-card" key={p.id}><div>
        <strong>{p.topic}</strong>
        <p>{p.subject} · {date(p.lesson_date)} · {t('common.minutesShort', { n: p.duration_minutes })}</p>
        <p>{p.objectives}</p>
        <span className="status-pill">{label('status', p.status)}</span>
        {(p.status === 'draft' || p.status === 'needs_revision') && <button className="secondary compact" onClick={() => setEditing(p)}>{t('plans.edit')}</button>}
      </div></article>)}
      {!plans.length && <p className="empty">{t('plans.mineEmpty')}</p>}
    </section>
    <section className="panel">
      <div className="panel-head"><div><h2>{t('resources.shareTitle')}</h2><p>{t('resources.shareHelp')}</p></div></div>
      <form className="stack-form" onSubmit={upload}>
        <div className="history-fields">
          <label>{t('resources.fileTitle')}<input name="title" required maxLength={200} /></label>
          <label>{t('plans.subject')}<input name="subject" /></label>
          <label className="span-all">{t('finance.description')}<textarea name="description" rows={2} /></label>
          <label className="span-all">{t('resources.chooseFile')}<input name="file" type="file" required /></label>
        </div>
        <button className="primary compact" disabled={busy}>{busy ? t('resources.uploading') : t('resources.upload')} →</button>
      </form>
      <ResourceList resources={resources} onRemoved={() => refresh(`/teacher/classes/${classID}/resources`)} />
    </section>
  </div>
}
