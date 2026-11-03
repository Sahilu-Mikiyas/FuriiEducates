import { ClipboardEvent, KeyboardEvent, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useAction } from '../../app/feedback'
import { useI18n } from '../../i18n'
import { api, APIError } from '../../lib/api'
import { useRefresh } from '../../lib/queries'
import type { ClassRoom, ClassSubject, MarkSheetData, MarkSheetItem, Term } from '../../lib/types'
import { TermSelect, useClassTerm } from './TermSelect'

const useRefreshMarks = () => {
  const refresh = useRefresh()
  return () => refresh('/marks', '/class-subjects', '/report-cards', '/terms')
}

// Marks is where a teacher sees what is left to do in a class and enters marks.
export function Marks({ classroom }: { classroom: ClassRoom }) {
  const { t } = useI18n()
  const { terms, term, setPicked, loaded } = useClassTerm(classroom.academic_year_id)
  const [subjectID, setSubjectID] = useState('')
  const termID = term?.id ?? ''
  const subjects = useQuery({
    queryKey: ['/class-subjects', classroom.id, termID],
    queryFn: () => api<ClassSubject[]>(`/classes/${classroom.id}/subjects?term_id=${termID}`),
    enabled: Boolean(termID),
  }).data ?? []
  const selected = subjects.find((subject) => subject.id === subjectID)

  if (loaded && !terms.length) return <section className="panel"><p className="empty">{t('marks.noTerms')}</p></section>
  return <div className="operations-page">
    <section className="panel">
      <div className="panel-head"><div><h2>{t('marks.subjectsTitle')}</h2><p>{t('marks.subjectsHelp')}</p></div></div>
      <TermSelect terms={terms} term={term} setPicked={setPicked} />
      <div className="table-wrap"><table>
        <thead><tr><th>{t('reports.subject').toUpperCase()}</th><th>{t('marks.col.teacher')}</th><th>{t('marks.col.weights')}</th><th>{t('marks.col.missing')}</th><th /></tr></thead>
        <tbody>
          {subjects.map((subject) => <tr key={subject.id} className={subject.id === subjectID ? 'selected-row' : ''}>
            <td><strong>{subject.name}</strong></td>
            <td>{subject.teacher_name || '—'}</td>
            <td><span className={`status-pill ${subject.weight_total === 100 ? '' : 'draft'}`}>{subject.weight_total} / 100</span></td>
            <td>{subject.items ? subject.missing_marks : '—'}</td>
            <td><button className="secondary compact" onClick={() => setSubjectID(subject.id)}>{t('attendance.open')}</button></td>
          </tr>)}
          {termID && !subjects.length && <tr><td colSpan={5} className="empty">{t('marks.noSubjects')}</td></tr>}
        </tbody>
      </table></div>
    </section>
    {selected && term && <MarkSheet key={`${selected.id}|${term.id}`} subject={selected} term={term} />}
  </div>
}

function MarkSheet({ subject, term }: { subject: ClassSubject; term: Term }) {
  const { t, errorText } = useI18n()
  const { busy, run } = useAction()
  const refreshMarks = useRefreshMarks()
  const [draft, setDraft] = useState<Record<string, string>>({})
  const query = useQuery({
    queryKey: ['/marks', subject.id, term.id],
    queryFn: () => api<MarkSheetData>(`/marks?class_subject_id=${subject.id}&term_id=${term.id}`),
    meta: { silent: true },
  })
  const sheet = query.data
  if (query.error) return <section className="panel"><p className="row-error">{errorText(query.error, 'error.load')}</p></section>
  if (!sheet) return <section className="panel"><p className="empty">{t('common.loading')}</p></section>
  const { items, students } = sheet
  const key = (studentID: string, itemID: string) => `${studentID}|${itemID}`
  const savedValue = (student: MarkSheetData['students'][number], itemID: string) => {
    const score = student.scores[itemID]
    return score === undefined ? '' : String(score)
  }
  const valueOf = (student: MarkSheetData['students'][number], itemID: string) => draft[key(student.student_id, itemID)] ?? savedValue(student, itemID)
  const invalid = (value: string, item: MarkSheetItem) => {
    if (value.trim() === '') return false
    const number = Number(value)
    return Number.isNaN(number) || number < 0 || number > item.max_score
  }
  const changes = students.flatMap((student) => items.flatMap((item) => {
    const value = draft[key(student.student_id, item.id)]
    return value === undefined || value.trim() === savedValue(student, item.id) ? [] : [{ student, item, value: value.trim() }]
  }))
  const problems = changes.filter((change) => invalid(change.value, change.item)).length
  const totalFor = (student: MarkSheetData['students'][number]) => {
    if (sheet.weight_total !== 100) return '—'
    let total = 0
    for (const item of items) {
      const value = valueOf(student, item.id).trim()
      if (value === '' || invalid(value, item)) return '—'
      total += (Number(value) / item.max_score) * item.weight
    }
    return String(Math.round(total * 100) / 100)
  }

  const save = () => run(async () => {
    await api('/marks', { method: 'PUT', body: JSON.stringify({
      class_subject_id: subject.id, term_id: term.id,
      marks: changes.map((change) => ({ student_id: change.student.student_id, item_id: change.item.id, score: change.value === '' ? null : Number(change.value) })),
    }) })
    await refreshMarks()
    setDraft({})
  }, t('marks.saved'))

  // Enter and the arrow keys move up and down a column, as in a spreadsheet.
  const move = (event: KeyboardEvent<HTMLInputElement>, row: number, column: number) => {
    const step = event.key === 'Enter' || event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0
    if (!step) return
    event.preventDefault()
    event.currentTarget.closest('table')?.querySelector<HTMLInputElement>(`input[data-cell="${row + step}:${column}"]`)?.focus()
  }
  // A block copied from a spreadsheet fills across and down from this cell.
  const paste = (event: ClipboardEvent<HTMLInputElement>, row: number, column: number) => {
    const text = event.clipboardData.getData('text/plain').replace(/\r/g, '').replace(/\n$/, '')
    if (!/[\t\n]/.test(text)) return
    event.preventDefault()
    const next: Record<string, string> = {}
    text.split('\n').forEach((line, down) => line.split('\t').forEach((cell, across) => {
      const student = students[row + down]
      const item = items[column + across]
      if (student && item) next[key(student.student_id, item.id)] = cell.trim()
    }))
    setDraft((current) => ({ ...current, ...next }))
  }

  return <section className="panel">
    <div className="panel-head">
      <div><h2>{subject.name} · {term.name}</h2><p>{t('marks.sheetHelp')}</p></div>
      <span className={`status-pill ${sheet.weight_total === 100 ? '' : 'draft'}`}>{sheet.weight_total} / 100</span>
    </div>
    {sheet.published && <p className="report-incomplete">{t('marks.locked')}</p>}
    {!sheet.published && !sheet.can_edit && <p className="report-incomplete">{t('marks.readOnly')}</p>}
    <MarkItems subject={subject} term={term} items={items} canEdit={sheet.can_edit} />
    {items.length > 0 && <>
      <div className="table-wrap marks-grid"><table>
        <thead><tr>
          <th>{t('common.col.student')}</th>
          {items.map((item) => <th key={item.id}>{item.title}<small>{t('marks.outOf', { max: item.max_score, weight: item.weight })}</small></th>)}
          <th>{t('marks.col.total')}</th>
        </tr></thead>
        <tbody>
          {students.map((student, row) => <tr key={student.student_id}>
            <td><strong>{student.student_name}</strong><small>{student.student_number}{!student.in_class && ` · ${t('marks.leftClass')}`}</small></td>
            {items.map((item, column) => {
              const value = valueOf(student, item.id)
              return <td key={item.id}><input
                className={invalid(value, item) ? 'mark-invalid' : ''}
                data-cell={`${row}:${column}`}
                inputMode="decimal"
                value={value}
                disabled={!sheet.can_edit}
                aria-label={`${student.student_name}, ${item.title}`}
                aria-invalid={invalid(value, item)}
                onChange={(event) => setDraft((current) => ({ ...current, [key(student.student_id, item.id)]: event.target.value }))}
                onKeyDown={(event) => move(event, row, column)}
                onPaste={(event) => paste(event, row, column)}
              /></td>
            })}
            <td><strong>{totalFor(student)}</strong></td>
          </tr>)}
          {!students.length && <tr><td colSpan={items.length + 2} className="empty">{t('common.noActiveStudents')}</td></tr>}
        </tbody>
      </table></div>
      {sheet.can_edit && <div className="register-foot">
        <span>{problems > 0 ? t('marks.invalidCells', { n: problems }) : changes.length > 0 ? t('marks.unsaved', { n: changes.length }) : t('marks.allSaved')}</span>
        <button className="primary compact" disabled={busy || !changes.length || problems > 0} onClick={() => void save()}>{t('marks.save')}</button>
      </div>}
    </>}
  </section>
}

function MarkItems({ subject, term, items, canEdit }: { subject: ClassSubject; term: Term; items: MarkSheetItem[]; canEdit: boolean }) {
  const { t } = useI18n()
  const { busy, run, onSubmit } = useAction()
  const refreshMarks = useRefreshMarks()
  const [editing, setEditing] = useState('')
  const body = (f: FormData) => ({ title: f.get('title'), weight: Number(f.get('weight')), max_score: Number(f.get('max_score')) })
  const add = onSubmit(async (f) => {
    await api('/marks/items', { method: 'POST', body: JSON.stringify({ class_subject_id: subject.id, term_id: term.id, ...body(f) }) })
    await refreshMarks()
  }, t('marks.itemAdded'))
  const update = (item: MarkSheetItem) => onSubmit(async (f) => {
    await api(`/marks/items/${item.id}`, { method: 'PUT', body: JSON.stringify({ ...body(f) }) })
    await refreshMarks()
    setEditing('')
  }, t('marks.itemSaved'), false)
  const remove = (item: MarkSheetItem) => run(async () => {
    try {
      await api(`/marks/items/${item.id}`, { method: 'DELETE' })
    } catch (error) {
      if (!(error instanceof APIError) || error.code !== 'ITEM_HAS_MARKS') throw error
      if (!window.confirm(t('marks.deleteWithMarks', { title: item.title }))) return
      await api(`/marks/items/${item.id}?with_marks=true`, { method: 'DELETE' })
    }
    await refreshMarks()
  })
  if (!canEdit) return null
  const fields = (item?: MarkSheetItem) => <>
    <label>{t('marks.itemTitle')}<input name="title" defaultValue={item?.title} maxLength={120} placeholder={t('marks.itemTitleHint')} required /></label>
    <label>{t('marks.itemWeight')}<input name="weight" type="number" min="0.01" max="100" step="0.01" defaultValue={item?.weight} required /></label>
    <label>{t('marks.itemMax')}<input name="max_score" type="number" min="0.01" max="1000" step="0.01" defaultValue={item?.max_score} required /></label>
  </>
  return <div className="mark-items">
    {items.length > 0 && <ul className="guardian-list">
      {items.map((item) => <li key={item.id}>
        {editing === item.id
          ? <form className="stack-form mark-item-form" onSubmit={update(item)}>
              {fields(item)}
              <div className="operation-actions">
                <button className="primary compact" disabled={busy}>{t('common.save')}</button>
                <button type="button" className="secondary compact" onClick={() => setEditing('')}>{t('common.cancel')}</button>
              </div>
            </form>
          : <>
              <div className="guardian-who"><strong>{item.title}</strong><small>{t('marks.outOf', { max: item.max_score, weight: item.weight })}</small></div>
              <div className="operation-actions">
                <button className="secondary compact" onClick={() => setEditing(item.id)}>{t('guardian.edit')}</button>
                <button className="secondary compact" disabled={busy} onClick={() => void remove(item)}>{t('guardian.remove')}</button>
              </div>
            </>}
      </li>)}
    </ul>}
    <form className="stack-form mark-item-form" onSubmit={add}>
      {fields()}
      <button className="secondary compact" disabled={busy}>{t('marks.addItem')}</button>
    </form>
  </div>
}
