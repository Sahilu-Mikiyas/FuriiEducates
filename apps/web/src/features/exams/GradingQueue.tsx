import { FormEvent, useCallback, useEffect, useState } from 'react'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import type { TeacherReview } from '../../lib/types'

export function gradingGuideText(guide: unknown) {
  if (typeof guide === 'string') return guide
  if (guide && typeof guide === 'object' && 'guide' in guide) return String((guide as { guide: unknown }).guide)
  return JSON.stringify(guide)
}

function answerText(answer: unknown) {
  let value = answer
  if (typeof value === 'string') {
    try { value = JSON.parse(value) as unknown } catch { /* Stored as plain text. */ }
  }
  return typeof value === 'string' ? value : JSON.stringify(value)
}

// GradingQueue lists written answers waiting for a grade. Teachers and
// administrators use different API paths but grade the same way.
export function GradingQueue({ classID, listPath, gradePath, help, onGraded }: {
  classID: string
  listPath: '/reviews' | '/teacher/reviews'
  gradePath: (review: TeacherReview) => string
  help: string
  onGraded?: () => void
}) {
  const { t, errorText } = useI18n()
  const [reviews, setReviews] = useState<TeacherReview[]>([])
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [saving, setSaving] = useState('')
  const load = useCallback(() => api<TeacherReview[]>(`${listPath}?class_id=${encodeURIComponent(classID)}`).then(setReviews).catch((e) => setError(errorText(e, 'error.load'))), [classID, listPath, errorText])
  useEffect(() => { void load() }, [load])

  async function grade(event: FormEvent<HTMLFormElement>, review: TeacherReview) {
    event.preventDefault()
    const values = new FormData(event.currentTarget)
    const id = `${review.attempt_id}:${review.question_id}`
    setSaving(id); setError(''); setNotice('')
    try {
      const result = await api<{ results_released?: boolean }>(gradePath(review), { method: 'PUT', body: JSON.stringify({ points: Number(values.get('points')), feedback: values.get('feedback') }) })
      await load()
      onGraded?.()
      if (result?.results_released) setNotice(t('grading.released'))
    } catch (e) { setError(errorText(e, 'error.generic')) } finally { setSaving('') }
  }

  return <section className="panel teacher-review-panel">
    <div className="panel-head"><div><p className="eyebrow">{t('grading.eyebrow')}</p><h2>{t('grading.title')}</h2><p>{help}</p></div><span className="count-pill">{t('grading.pending', { n: reviews.length })}</span></div>
    {error && <p className="form-error" role="alert">{error}</p>}
    {notice && <p className="invite-success" role="status">{notice}</p>}
    <div className="teacher-review-list">
      {reviews.map((review) => {
        const id = `${review.attempt_id}:${review.question_id}`
        return <article className="teacher-review-card" key={id}>
          <div className="review-meta"><strong>{review.student_name}</strong><span>{review.student_number} · {review.assessment_title}</span></div>
          <h3>{review.prompt?.text || t('grading.writtenResponse')}</h3>
          {Boolean(review.grading_guide) && <p className="review-guide"><strong>{t('grading.guide')}:</strong> {gradingGuideText(review.grading_guide)}</p>}
          <div className="review-answer">{answerText(review.answer)}</div>
          <form className="review-grade-form" onSubmit={(e) => void grade(e, review)}>
            <label>{t('grading.points')} <span>{t('grading.outOf', { n: review.max_points })}</span><input name="points" type="number" min="0" max={review.max_points} step="0.01" required /></label>
            <label>{t('grading.feedback')}<textarea name="feedback" rows={2} maxLength={5000} defaultValue={review.feedback} /></label>
            <button className="primary compact" disabled={saving === id}>{saving === id ? t('common.saving') : t('grading.save')}</button>
          </form>
        </article>
      })}
      {!reviews.length && !error && <p className="empty">{t('grading.empty')}</p>}
    </div>
  </section>
}
