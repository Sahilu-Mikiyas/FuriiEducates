import { useAction } from '../../app/feedback'
import { DateField } from '../../calendar/DateField'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useAssessments, useClasses, useQuestions, useRefresh } from '../../lib/queries'
import { Assessment, classLabel } from '../../lib/types'

const isoTime = (value: FormDataEntryValue | null) => {
  const text = String(value || '')
  return text ? new Date(text).toISOString() : null
}

export default function AssessmentsPage() {
  const { t, label } = useI18n()
  const { busy, run, onSubmit } = useAction()
  const refresh = useRefresh()
  const assessments = useAssessments().data ?? []
  const classes = useClasses().data ?? []
  const questions = useQuestions().data ?? []
  const published = questions.filter((q) => q.status === 'published')

  const create = onSubmit(async (f) => {
    await api('/assessments', { method: 'POST', body: JSON.stringify({ title: f.get('title'), instructions: f.get('instructions'), duration_minutes: Number(f.get('duration_minutes')), question_ids: f.getAll('question_ids').map(String), shuffle_questions: f.get('shuffle_questions') === 'on', shuffle_options: f.get('shuffle_options') === 'on', locked_mode: f.get('locked_mode') === 'on' }) })
    await refresh('/assessments')
  }, t('assessments.saved'))

  const publish = (id: string) => run(async () => {
    await api(`/assessments/${id}/publish`, { method: 'POST', body: '{}' })
    await refresh('/assessments')
  }, t('assessments.published'))

  const assign = (assessment: Assessment) => onSubmit(async (f) => {
    const classID = String(f.get('class_id') || '')
    if (!classID) throw new Error(t('common.chooseClass'))
    await api(`/assessments/${assessment.id}/assignments`, { method: 'POST', body: JSON.stringify({ class_id: classID, opens_at: isoTime(f.get('opens_at')), due_at: isoTime(f.get('due_at')), requires_start_code: f.get('requires_start_code') === 'on' }) })
    await refresh('/assessments')
  }, t('assessments.assigned'))

  return <section className="panel assessment-panel" id="assessments">
    <div className="panel-head"><div><p className="eyebrow">{t('assessments.eyebrow')}</p><h2>{t('assessments.heading')}</h2><p>{t('assessments.help')}</p></div><span className="year-icon">✎</span></div>
    <form className="stack-form assessment-form" onSubmit={create}>
      <div className="assessment-fields">
        <label>{t('assessments.titleField')}<input name="title" placeholder={t('assessments.titleHint')} required maxLength={200} /></label>
        <label>{t('assessments.duration')}<input name="duration_minutes" type="number" min="5" max="240" step="1" defaultValue="45" required /></label>
        <label>{t('assessments.instructions')}<textarea name="instructions" rows={3} maxLength={5000} placeholder={t('assessments.instructionsHint')} /></label>
      </div>
      <div className="assessment-options">
        <strong>{t('assessments.security')}</strong>
        <label><input type="checkbox" name="shuffle_questions" /> {t('assessments.shuffleQuestions')}</label>
        <label><input type="checkbox" name="shuffle_options" /> {t('assessments.shuffleOptions')}</label>
        <label><input type="checkbox" name="locked_mode" /> {t('assessments.lockedMode')}</label>
      </div>
      <div className="assessment-question-picker">
        <strong>{t('assessments.chooseQuestions')}</strong>
        <div className="assessment-question-list">
          {published.map((q) => <label key={q.id}>
            <input type="checkbox" name="question_ids" value={q.id} />
            <span><b>{q.prompt?.text || t('questions.promptFallback')}</b><small>{label('questionType', q.question_type)}{q.skill_tags.length ? ` · ${q.skill_tags.map((tag) => tag.title).join(', ')}` : ''}</small></span>
          </label>)}
          {!published.length && <p className="empty">{t('assessments.noPublished')}</p>}
        </div>
      </div>
      <button className="primary compact" disabled={busy || !published.length}>{t('assessments.saveDraft')}</button>
    </form>
    <div className="assessment-list">
      {assessments.map((assessment) => {
        const unassigned = classes.filter((c) => !assessment.assigned_class_ids.includes(c.id))
        return <article key={assessment.id}>
          <div className="assessment-summary">
            <strong>{assessment.title}</strong>
            <small>
              {t('common.questionsCount', { n: assessment.question_count })} · {t('common.minutesCount', { n: assessment.duration_minutes })} · <span className={`status-pill ${assessment.status}`}>{label('status', assessment.status)}</span>
              {assessment.shuffle_questions || assessment.shuffle_options ? ` · ${t('assessments.shuffledTag')}` : ''}
              {assessment.locked_mode ? ` · ${t('assessments.lockedTag')}` : ''}
            </small>
            {assessment.instructions && <p>{assessment.instructions}</p>}
            <div className="assigned-classes">
              {assessment.assigned_class_ids.map((classID) => {
                const classroom = classes.find((c) => c.id === classID)
                return classroom ? <span key={classID}>{classLabel(classroom)}</span> : null
              })}
              {!assessment.assigned_class_ids.length && <small>{t('assessments.notAssigned')}</small>}
            </div>
          </div>
          <div className="assessment-actions">
            {assessment.status === 'draft' && <button className="secondary compact" disabled={busy || assessment.question_count === 0} onClick={() => void publish(assessment.id)}>{t('common.publish')}</button>}
            {assessment.status === 'published' && <form className="staff-assign assign-form" onSubmit={assign(assessment)}>
              <select name="class_id" defaultValue="" aria-label={t('assessments.assignClass')}>
                <option value="">{t('assessments.assignClass')}</option>
                {unassigned.map((c) => <option key={c.id} value={c.id}>{classLabel(c)}</option>)}
              </select>
              <DateField name="opens_at" label={t('assessments.opens')} withTime />
              <DateField name="due_at" label={t('assessments.due')} withTime />
              <label className="assign-check"><input type="checkbox" name="requires_start_code" /> {t('assessments.invigilated')}</label>
              <button className="secondary compact" disabled={busy || !unassigned.length}>{t('common.assign')}</button>
            </form>}
          </div>
        </article>
      })}
      {!assessments.length && <p className="empty">{t('assessments.empty')}</p>}
    </div>
  </section>
}
