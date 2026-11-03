import { ReactNode, useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { useDates } from '../../calendar/useDates'
import { useI18n } from '../../i18n'
import { classLabel, ReportCardData } from '../../lib/types'

const shown = (value: number | null | undefined) => (value === null || value === undefined ? '—' : String(value))

// usePrint renders a job into the print area, then opens the browser's print
// dialog, which also saves as a PDF. The page itself is hidden while printing.
export function usePrint<Job>() {
  // Wrapped so that printing the same thing twice opens the dialog twice.
  const [job, setJob] = useState<{ value: Job } | null>(null)
  useEffect(() => {
    if (job === null) return
    const timer = window.setTimeout(() => window.print(), 100)
    return () => window.clearTimeout(timer)
  }, [job])
  const area = (content: (job: Job) => ReactNode) => (job === null ? null : createPortal(<div className="print-root">{content(job.value)}</div>, document.body))
  return { print: (value: Job) => setJob({ value }), area }
}

export function ReportCardView({ card, draft = false }: { card: ReportCardData; draft?: boolean }) {
  const { t } = useI18n()
  const { today } = useDates()
  const earlier = card.earlier_terms ?? []
  const hasYear = earlier.length > 0
  return <article className="report-card">
    <header className="report-card-head">
      <div><h2>{card.school_name}</h2><p>{t('reports.cardTitle')}</p></div>
      <div className="report-card-term"><strong>{card.term_name}</strong><span>{card.year_name}</span></div>
    </header>
    {draft && <p className="report-draft">{t('reports.draftNotice')}</p>}
    <dl className="report-card-who">
      <div><dt>{t('common.col.studentLabel')}</dt><dd>{card.student_name}</dd></div>
      <div><dt>{t('students.id')}</dt><dd>{card.student_number}</dd></div>
      <div><dt>{t('reports.class')}</dt><dd>{classLabel({ grade_level: card.grade_level, name: card.class_name })}</dd></div>
    </dl>
    <div className="table-wrap"><table className="report-card-table">
      <thead><tr>
        <th>{t('reports.subject')}</th>
        {earlier.map((term) => <th key={term.term_id}>{term.name}</th>)}
        <th>{card.term_name}</th>
        <th>{t('reports.grade')}</th>
        {hasYear && <th>{t('reports.yearAverage')}</th>}
      </tr></thead>
      <tbody>
        {card.subjects.map((subject) => <tr key={subject.subject_id}>
          <td>{subject.name}{!subject.counts_in_average && <small> {t('reports.notInAverage')}</small>}</td>
          {earlier.map((term) => <td key={term.term_id}>{shown(subject.earlier?.[term.term_id])}</td>)}
          <td className={subject.total !== null && subject.total < card.pass_percent ? 'below-pass' : ''}>{shown(subject.total)}</td>
          <td>{subject.grade || '—'}</td>
          {hasYear && <td>{shown(subject.year_average)}</td>}
        </tr>)}
      </tbody>
      <tfoot>
        <tr>
          <th>{t('reports.average')}</th>
          {earlier.map((term) => <td key={term.term_id}>{shown(term.average)}</td>)}
          <td><strong>{shown(card.average)}</strong></td>
          <td><strong>{card.grade || '—'}</strong></td>
          {hasYear && <td><strong>{shown(card.year_average)}</strong></td>}
        </tr>
        <tr>
          <th>{t('reports.rank')}</th>
          {earlier.map((term) => <td key={term.term_id}>{shown(term.rank)}</td>)}
          <td><strong>{card.rank ? t('reports.rankOf', { rank: card.rank, n: card.students_ranked }) : '—'}</strong></td>
          <td />
          {hasYear && <td><strong>{shown(card.year_rank)}</strong></td>}
        </tr>
      </tfoot>
    </table></div>
    {!card.complete && <p className="report-incomplete">{t('reports.incompleteNotice')}</p>}
    <div className="report-card-facts">
      <div><h3>{t('attendance.summaryTitle')}</h3><p>{t('reports.attendanceLine', { days: card.attendance.days_marked, present: card.attendance.present, late: card.attendance.late, absent: card.attendance.absent, excused: card.attendance.excused })}</p></div>
      <div><h3>{t('reports.conduct')}</h3><p>{card.conduct || '—'}</p></div>
      <div className="report-card-comment"><h3>{t('reports.comment')}</h3><p>{card.comment || '—'}</p></div>
    </div>
    <div className="report-card-signs">
      <span>{t('reports.signHomeroom')}</span>
      <span>{t('reports.signDirector')}</span>
      <span>{t('reports.signParent')}</span>
    </div>
    <p className="report-card-foot">{t('reports.passNote', { n: card.pass_percent })} · {t('reports.printedOn', { date: today() })}</p>
  </article>
}
