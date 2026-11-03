import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useAction } from '../../app/feedback'
import { useDates } from '../../calendar/useDates'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { downloadCSV } from '../../lib/csv'
import { useRefresh } from '../../lib/queries'
import { ClassReportData, ClassRoom, classLabel, ReportCardData, ReportProblem } from '../../lib/types'
import { TermSelect, useClassTerm } from '../marks/TermSelect'
import { ReportCardView, usePrint } from './ReportCard'

const shown = (value: number | null | undefined) => (value === null || value === undefined ? '—' : String(value))
const CONDUCT = ['', 'A', 'B', 'C', 'D', 'F']

type PrintJob = { kind: 'cards'; cards: ReportCardData[] } | { kind: 'roster' }

// ClassReports is the roster of a class for a term, the conduct and comments
// that go on each card, and for administrators the step that issues them.
export function ClassReports({ classroom, canPublish }: { classroom: ClassRoom; canPublish: boolean }) {
  const { t, errorText } = useI18n()
  const { dateTime } = useDates()
  const { busy, run } = useAction()
  const refresh = useRefresh()
  const { terms, term, setPicked, loaded } = useClassTerm(classroom.academic_year_id)
  const [open, setOpen] = useState('')
  const [notes, setNotes] = useState<Record<string, { conduct: string; comment: string }>>({})
  const { print, area } = usePrint<PrintJob>()
  const termID = term?.id ?? ''
  const query = useQuery({
    queryKey: ['/report-cards', classroom.id, termID],
    queryFn: () => api<ClassReportData>(`/report-cards/classes/${classroom.id}?term_id=${termID}`),
    enabled: Boolean(termID),
    meta: { silent: true },
  })
  const report = query.data
  const cards = report?.cards ?? []
  const problems = report?.problems ?? []
  const subjects = cards[0]?.subjects ?? []
  const blocked = problems.some((problem) => problem.kind === 'weights')
  const noteChanges = Object.keys(notes).length
  const openCard = cards.find((card) => card.student_id === open)
  const refreshAll = () => refresh('/report-cards', '/marks', '/class-subjects', '/terms')

  const problemText = (problem: ReportProblem) => {
    if (problem.kind === 'no_items') return t('reports.problem.noItems', { subject: problem.subject })
    if (problem.kind === 'weights') return t('reports.problem.weights', { subject: problem.subject, n: problem.weight_total ?? 0 })
    return t('reports.problem.missing', { subject: problem.subject, n: problem.missing_marks ?? 0 })
  }
  const noteOf = (card: ReportCardData) => notes[card.student_id] ?? { conduct: card.conduct, comment: card.comment }
  const setNote = (card: ReportCardData, next: Partial<{ conduct: string; comment: string }>) => setNotes((current) => ({ ...current, [card.student_id]: { ...noteOf(card), ...next } }))

  const saveNotes = () => run(async () => {
    await api(`/report-cards/classes/${classroom.id}/notes`, { method: 'PUT', body: JSON.stringify({ term_id: termID, notes: Object.entries(notes).map(([student_id, note]) => ({ student_id, ...note })) }) })
    await refresh('/report-cards')
    setNotes({})
  }, t('reports.notesSaved'))
  const publish = () => {
    if (!window.confirm(problems.length ? t('reports.publishIncomplete', { n: problems.length }) : t('reports.publishConfirm', { n: cards.length }))) return
    void run(async () => {
      await api(`/report-cards/classes/${classroom.id}/publish`, { method: 'POST', body: JSON.stringify({ term_id: termID, allow_incomplete: problems.length > 0 }) })
      await refreshAll()
    }, t('reports.published'))
  }
  const unpublish = () => {
    const reason = window.prompt(t('reports.unpublishReason'))
    if (!reason?.trim()) return
    void run(async () => {
      await api(`/report-cards/classes/${classroom.id}/unpublish`, { method: 'POST', body: JSON.stringify({ term_id: termID, reason }) })
      await refreshAll()
    }, t('reports.unpublished'))
  }
  const rosterRows = () => [
    [t('students.id'), t('students.fullName'), ...subjects.map((subject) => subject.name), t('reports.total'), t('reports.average'), t('reports.grade'), t('reports.rank')],
    ...cards.map((card) => [card.student_number, card.student_name, ...card.subjects.map((subject) => shown(subject.total)), shown(card.total), shown(card.average), card.grade || '—', shown(card.rank)]),
  ]

  if (loaded && !terms.length) return <section className="panel"><p className="empty">{t('marks.noTerms')}</p></section>
  const rosterTable = <table className="roster-table">
    <thead><tr><th>{t('common.col.student')}</th>{subjects.map((subject) => <th key={subject.subject_id}>{subject.name}</th>)}<th>{t('reports.total')}</th><th>{t('reports.average')}</th><th>{t('reports.grade')}</th><th>{t('reports.rank')}</th></tr></thead>
    <tbody>
      {cards.map((card) => <tr key={card.student_id}>
        <td><button className="link-button" onClick={() => setOpen(card.student_id)}>{card.student_name}</button><small>{card.student_number}</small></td>
        {card.subjects.map((subject) => <td key={subject.subject_id} className={subject.total !== null && subject.total < card.pass_percent ? 'below-pass' : ''}>{shown(subject.total)}</td>)}
        <td>{shown(card.total)}</td><td><strong>{shown(card.average)}</strong></td><td>{card.grade || '—'}</td><td><strong>{shown(card.rank)}</strong></td>
      </tr>)}
      {report && !cards.length && <tr><td colSpan={subjects.length + 5} className="empty">{t('common.noActiveStudents')}</td></tr>}
    </tbody>
  </table>

  return <div className="operations-page">
    <section className="panel">
      <div className="panel-head">
        <div><h2>{t('reports.rosterTitle')}</h2><p>{t('reports.rosterHelp')}</p></div>
        {report && <span className={`status-pill ${report.published ? '' : 'draft'}`}>{report.published ? t('reports.statusPublished') : t('reports.statusDraft')}</span>}
      </div>
      <TermSelect terms={terms} term={term} setPicked={(id) => { setPicked(id); setOpen(''); setNotes({}) }} />
      {query.error && <p className="row-error">{errorText(query.error, 'error.load')}</p>}
      {report?.published && <p className="muted">{t('reports.publishedOn', { date: dateTime(report.published_at) })}</p>}
      {problems.length > 0 && <div className="report-problems">
        <h3 className="attendance-subhead">{t('reports.problemsTitle')}</h3>
        <ul>{problems.map((problem) => <li key={problem.subject_id} className={problem.kind === 'weights' ? 'row-error' : ''}>{problemText(problem)}</li>)}</ul>
      </div>}
      <div className="table-wrap">{rosterTable}</div>
      {cards.length > 0 && <div className="register-foot report-actions">
        <div className="operation-actions">
          <button className="secondary compact" onClick={() => print({ kind: 'cards', cards })}>{t('reports.printCards')}</button>
          <button className="secondary compact" onClick={() => print({ kind: 'roster' })}>{t('reports.printRoster')}</button>
          <button className="secondary compact" onClick={() => downloadCSV(`roster-${classroom.grade_level}-${classroom.name}-${term?.name}.csv`.replace(/\s+/g, '-'), rosterRows())}>{t('attendance.download')}</button>
        </div>
        {canPublish && <div className="operation-actions">
          {report?.published
            ? <button className="secondary compact" disabled={busy} onClick={unpublish}>{t('reports.unpublish')}</button>
            : <button className="primary compact" disabled={busy || blocked || noteChanges > 0} onClick={publish}>{t('reports.publish')}</button>}
        </div>}
      </div>}
    </section>

    {cards.length > 0 && !report?.published && <section className="panel">
      <div className="panel-head"><div><h2>{t('reports.notesTitle')}</h2><p>{t('reports.notesHelp')}</p></div></div>
      <ul className="report-notes">
        {cards.map((card) => {
          const note = noteOf(card)
          return <li key={card.student_id}>
            <strong>{card.student_name}</strong>
            <select aria-label={t('reports.conductFor', { name: card.student_name })} value={note.conduct} onChange={(event) => setNote(card, { conduct: event.target.value })}>
              {CONDUCT.map((grade) => <option key={grade} value={grade}>{grade || t('reports.conduct')}</option>)}
            </select>
            <input aria-label={t('reports.commentFor', { name: card.student_name })} placeholder={t('reports.comment')} maxLength={600} value={note.comment} onChange={(event) => setNote(card, { comment: event.target.value })} />
          </li>
        })}
      </ul>
      <div className="register-foot">
        <span>{noteChanges > 0 ? t('attendance.unsaved') : ''}</span>
        <button className="primary compact" disabled={busy || noteChanges === 0} onClick={() => void saveNotes()}>{t('reports.saveNotes')}</button>
      </div>
    </section>}

    {openCard && <section className="panel">
      <div className="panel-head"><div><h2>{openCard.student_name}</h2></div>
        <div className="operation-actions">
          <button className="secondary compact" onClick={() => print({ kind: 'cards', cards: [openCard] })}>{t('reports.printCard')}</button>
          <button className="secondary compact" onClick={() => setOpen('')}>{t('common.cancel')}</button>
        </div>
      </div>
      <ReportCardView card={openCard} draft={!report?.published} />
    </section>}

    {area((job) => (job.kind === 'cards'
      ? job.cards.map((card) => <ReportCardView key={card.student_id} card={card} draft={!report?.published} />)
      : <div className="print-roster">
          <h2>{cards[0]?.school_name}</h2>
          <p>{t('reports.rosterTitle')} · {classLabel(classroom)} · {term?.name} · {cards[0]?.year_name}</p>
          {!report?.published && <p className="report-draft">{t('reports.draftNotice')}</p>}
          {rosterTable}
        </div>))}
  </div>
}
