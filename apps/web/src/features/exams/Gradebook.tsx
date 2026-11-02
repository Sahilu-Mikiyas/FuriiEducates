import { useEffect, useState } from 'react'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import type { GradebookCell, GradebookData } from '../../lib/types'

function useCellText() {
  const { t } = useI18n()
  return (cell: GradebookCell | undefined) => {
    if (!cell) return t('status.not_started')
    if (cell.status === 'in_progress') return t('status.in_progress')
    if (cell.results_released) return `${cell.final_score ?? 0} / ${cell.final_max_score ?? 0}`
    return t('gradebook.soFar', { score: cell.score ?? 0, max: cell.max_score ?? 0, n: cell.pending_review })
  }
}

export function Gradebook({ classID }: { classID: string }) {
  const { t, errorText } = useI18n()
  const cellText = useCellText()
  const [book, setBook] = useState<GradebookData | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    if (!classID) { setBook(null); return }
    let ignore = false
    setError('')
    api<GradebookData>(`/gradebook?class_id=${encodeURIComponent(classID)}`)
      .then((data) => { if (!ignore) setBook(data) })
      .catch((e) => { if (!ignore) setError(errorText(e, 'error.load')) })
    return () => { ignore = true }
  }, [classID, errorText])

  function downloadCSV(data: GradebookData) {
    const quote = (value: string) => `"${value.replace(/"/g, '""')}"`
    const lines = [[t('gradebook.csvStudentID'), t('gradebook.csvStudent'), ...data.assignments.map((a) => a.title)].map(quote).join(',')]
    for (const student of data.students) {
      lines.push([student.student_number, student.student_name, ...data.assignments.map((a) => cellText(student.results[a.assignment_id]))].map(quote).join(','))
    }
    const link = document.createElement('a')
    link.href = URL.createObjectURL(new Blob([`﻿${lines.join('\r\n')}`], { type: 'text/csv;charset=utf-8' }))
    link.download = 'gradebook.csv'
    link.click()
    URL.revokeObjectURL(link.href)
  }

  return <section className="panel gradebook-panel">
    <div className="panel-head">
      <div><p className="eyebrow">{t('gradebook.eyebrow')}</p><h2>{t('gradebook.title')}</h2><p>{t('gradebook.help')}</p></div>
      {book && book.assignments.length > 0 && <button className="secondary compact" onClick={() => downloadCSV(book)}>{t('gradebook.download')}</button>}
    </div>
    {error && <p className="form-error" role="alert">{error}</p>}
    {book && !book.assignments.length && <p className="empty">{t('gradebook.noAssessments')}</p>}
    {book && book.assignments.length > 0 && <div className="table-wrap"><table>
      <thead><tr><th>{t('common.col.student')}</th>{book.assignments.map((a) => <th key={a.assignment_id}>{a.title}</th>)}</tr></thead>
      <tbody>
        {book.students.map((student) => <tr key={student.student_id}>
          <td><strong>{student.student_name}</strong><small>{student.student_number}</small></td>
          {book.assignments.map((a) => <td key={a.assignment_id}>{cellText(student.results[a.assignment_id])}</td>)}
        </tr>)}
        {!book.students.length && <tr><td colSpan={book.assignments.length + 1} className="empty">{t('common.noActiveStudents')}</td></tr>}
      </tbody>
    </table></div>}
  </section>
}
