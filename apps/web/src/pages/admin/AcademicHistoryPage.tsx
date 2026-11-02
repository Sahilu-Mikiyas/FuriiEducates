import { FormEvent, useEffect, useState } from 'react'
import { useAction, useFeedback } from '../../app/feedback'
import { DateField } from '../../calendar/DateField'
import { useDates } from '../../calendar/useDates'
import { useI18n } from '../../i18n'
import { api, apiURL, readAPIResponse } from '../../lib/api'
import { useAcademicRecords, useExternalExams, useRefresh, useStudents } from '../../lib/queries'
import type { ImportPreview } from '../../lib/types'

const optionalNumber = (value: FormDataEntryValue | null) => {
  const text = String(value ?? '')
  return text === '' ? null : Number(text)
}
const scoreText = (score: number | null, maximum: number | null) => (score === null ? '—' : `${score}${maximum === null ? '' : ` / ${maximum}`}`)

export default function AcademicHistoryPage() {
  const { t, label } = useI18n()
  const { date } = useDates()
  const { setNotice } = useFeedback()
  const { busy, run, onSubmit } = useAction()
  const refresh = useRefresh()
  const students = useStudents().data ?? []
  const [studentID, setStudentID] = useState('')
  const [preview, setPreview] = useState<ImportPreview | null>(null)
  const records = useAcademicRecords(studentID).data ?? []
  const exams = useExternalExams(studentID).data ?? []
  const recordsPath = `/students/${studentID}/academic-records`
  const examsPath = `/students/${studentID}/external-exams`

  useEffect(() => { setStudentID((current) => current || students[0]?.id || '') }, [students])

  const addRecord = onSubmit(async (f) => {
    await api(recordsPath, { method: 'POST', body: JSON.stringify({ school_name: f.get('school_name'), academic_year_label: f.get('academic_year_label'), grade_level: f.get('grade_level'), subject_name: f.get('subject_name'), score: optionalNumber(f.get('score')), maximum_score: optionalNumber(f.get('maximum_score')), grade_label: f.get('grade_label'), source_type: 'staff_entry' }) })
    await refresh(recordsPath)
  }, t('history.recordAdded'))

  const addExam = onSubmit(async (f) => {
    await api(examsPath, { method: 'POST', body: JSON.stringify({ exam_name: f.get('exam_name'), exam_provider: f.get('exam_provider'), exam_date: f.get('exam_date'), subject_or_section: f.get('subject_or_section'), score: optionalNumber(f.get('score')), maximum_score: optionalNumber(f.get('maximum_score')), score_label: f.get('score_label') }) })
    await refresh(examsPath)
  }, t('history.examAdded'))

  function previewCSV(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const file = new FormData(event.currentTarget).get('file') as File | null
    void run(async () => {
      if (!file || file.size === 0) throw new Error(t('history.chooseCSV'))
      const response = await fetch(apiURL('/import-jobs/preview'), { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'text/csv' }, body: await file.text() })
      const body = await readAPIResponse(response)
      if (!response.ok) throw new Error(body?.error?.message ?? t('history.previewFailed'))
      setPreview(body.data as ImportPreview)
      setNotice(t('history.previewReady'))
    })
  }

  const commitCSV = (item: ImportPreview) => run(async () => {
    await api(`/import-jobs/${item.id}/commit`, { method: 'POST', body: '{}' })
    setPreview({ ...item, status: 'committed', can_commit: false })
    await refresh(recordsPath)
  }, t('history.importDone'))

  return <section className="panel passport-panel" id="academic-passport">
    <div className="panel-head">
      <div><p className="eyebrow">{t('history.eyebrow')}</p><h2>{t('admin.title.academic-passport')}</h2><p>{t('admin.desc.academic-passport')}</p></div>
      <label className="student-picker">{t('history.student')}<select value={studentID} onChange={(e) => setStudentID(e.target.value)}>
        <option value="">{t('history.selectStudent')}</option>
        {students.map((s) => <option key={s.id} value={s.id}>{s.legal_name} · {s.student_number}</option>)}
      </select></label>
    </div>
    {!students.length && <p className="empty">{t('history.noStudents')}</p>}
    {Boolean(studentID) && <>
      <div className="passport-grid">
        <div className="passport-column">
          <h3>{t('history.records')} <span>{records.length}</span></h3>
          <form className="stack-form history-form" onSubmit={addRecord}>
            <div className="history-fields">
              <input name="school_name" placeholder={t('history.previousSchool')} aria-label={t('history.previousSchool')} required />
              <input name="academic_year_label" placeholder={t('classes.year')} aria-label={t('classes.year')} required />
              <input name="grade_level" placeholder={t('classes.grade')} aria-label={t('classes.grade')} required />
              <input name="subject_name" placeholder={t('plans.subject')} aria-label={t('plans.subject')} required />
              <input name="score" type="number" min="0" step="0.001" placeholder={t('history.score')} aria-label={t('history.score')} />
              <input name="maximum_score" type="number" min="0.001" step="0.001" placeholder={t('history.outOf')} aria-label={t('history.outOf')} />
              <input name="grade_label" placeholder={t('history.gradeLabel')} aria-label={t('history.gradeLabel')} />
            </div>
            <button className="secondary" disabled={busy}>{t('history.addRecord')} <span>→</span></button>
          </form>
          <div className="passport-list">
            {records.map((r) => <article key={r.id}>
              <div><strong>{r.subject_name}</strong><span>{r.school_name} · {r.grade_level} · {r.academic_year_label}</span></div>
              <b>{scoreText(r.score, r.maximum_score)}</b>
              <small>{r.grade_label || label('status', r.verification_status)}</small>
            </article>)}
            {!records.length && <p className="empty">{t('history.recordsEmpty')}</p>}
          </div>
        </div>
        <div className="passport-column">
          <h3>{t('history.exams')} <span>{exams.length}</span></h3>
          <form className="stack-form exam-form" onSubmit={addExam}>
            <div className="history-fields exam-fields">
              <input name="exam_name" placeholder={t('history.examName')} aria-label={t('history.examName')} required />
              <input name="exam_provider" placeholder={t('history.provider')} aria-label={t('history.provider')} />
              <DateField name="exam_date" label={t('history.examDate')} />
              <input name="subject_or_section" placeholder={t('history.section')} aria-label={t('history.section')} />
              <input name="score" type="number" min="0" step="0.001" placeholder={t('history.score')} aria-label={t('history.score')} />
              <input name="maximum_score" type="number" min="0.001" step="0.001" placeholder={t('history.outOf')} aria-label={t('history.outOf')} />
              <input name="score_label" placeholder={t('history.scoreLabel')} aria-label={t('history.scoreLabel')} />
            </div>
            <button className="secondary" disabled={busy}>{t('history.addExam')} <span>→</span></button>
          </form>
          <div className="passport-list">
            {exams.map((x) => <article key={x.id}>
              <div>
                <strong>{x.exam_name}{x.exam_provider ? ` · ${x.exam_provider}` : ''}</strong>
                <span>{x.subject_or_section || (x.exam_date ? date(x.exam_date) : t('history.externalExam'))}</span>
                {x.breakdowns.map((b) => <small key={b.section_name}>{b.section_name}: {b.score ?? '—'}{b.maximum_score === null ? '' : ` / ${b.maximum_score}`}</small>)}
              </div>
              <b>{x.score === null ? (x.score_label || '—') : scoreText(x.score, x.maximum_score)}</b>
            </article>)}
            {!exams.length && <p className="empty">{t('history.examsEmpty')}</p>}
          </div>
        </div>
      </div>
      <div className="csv-import">
        <div>
          <h3>{t('history.importTitle')}</h3>
          <p>{t('history.importHelp')}</p>
          <code>student_number, school_name, academic_year_label, grade_level, subject_name, score, maximum_score, grade_label, source_type</code>
        </div>
        <form onSubmit={previewCSV}>
          <input type="file" name="file" accept=".csv,text/csv" aria-label={t('history.importFile')} required />
          <button className="secondary" disabled={busy}>{t('history.preview')}</button>
        </form>
      </div>
      {preview && <div className="import-preview">
        <div><strong>{t('history.previewTitle')}</strong><span>{t('history.previewCounts', { valid: preview.valid_rows, errors: preview.error_rows })} · {label('status', preview.status)}</span></div>
        {preview.row_errors.map((row) => <p className="row-error" key={row.row}>{t('history.rowError', { row: row.row })}: {row.errors.join(' ')}</p>)}
        {preview.can_commit && preview.status === 'previewed' && <button className="primary compact" disabled={busy} onClick={() => void commitCSV(preview)}>{t('history.commit', { n: preview.valid_rows })}</button>}
      </div>}
    </>}
  </section>
}
