import { useDates } from '../../calendar/useDates'
import { useI18n } from '../../i18n'
import type { AttendanceSummaryData } from '../../lib/types'

// AttendanceSummary shows one student's last 30 days, to the student and to staff.
// It is kept apart from the register so the learner workspace stays small.
export function AttendanceSummary({ summary }: { summary: AttendanceSummaryData }) {
  const { t, label } = useI18n()
  const { date } = useDates()
  const counts = [['present', summary.present], ['late', summary.late], ['absent', summary.absent], ['excused', summary.excused]] as const
  return <section className="panel">
    <div className="panel-head"><div><h2>{t('attendance.summaryTitle')}</h2><p>{t('attendance.summaryHelp')}</p></div></div>
    {summary.days_marked === 0 && !summary.recent.length
      ? <p className="empty">{t('attendance.summaryEmpty')}</p>
      : <>
          <div className="attendance-counts">
            {counts.map(([status, count]) => <div key={status} className={`attendance-count ${status}`}><strong>{count}</strong><span>{label('attendance.status', status)}</span></div>)}
          </div>
          {summary.recent.length > 0 && <>
            <h3 className="attendance-subhead">{t('attendance.recentTitle')}</h3>
            <ul className="attendance-recent">
              {summary.recent.map((day) => <li key={day.date}>
                <span>{date(day.date)}</span>
                <span className={`attendance-tag ${day.status}`}>{label('attendance.status', day.status)}</span>
                {day.note && <small>{day.note}</small>}
              </li>)}
            </ul>
          </>}
        </>}
  </section>
}
