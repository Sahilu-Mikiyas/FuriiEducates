import { FormEvent, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router'
import { useAction } from '../../app/feedback'
import { DateField } from '../../calendar/DateField'
import { ethiopianToGregorian, gregorianToEthiopian, parseISODate, todayISO, toISODate } from '../../calendar/ethiopian'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useRefresh } from '../../lib/queries'
import { AttendanceAlert, AttendanceReportData, ClassRoom, classLabel, RegisterData } from '../../lib/types'

const STATUSES = ['present', 'absent', 'late', 'excused'] as const
type Mark = { status: string; note: string }

// refreshAttendance covers every attendance list, which all share these first keys.
export const useRefreshAttendance = () => {
  const refresh = useRefresh()
  return () => refresh('/attendance/classes', '/attendance/overview', '/attendance/alerts', '/attendance/report')
}

// GuardianContact gives staff a number they can tap to call.
export function GuardianContact({ name, phone }: { name: string; phone: string }) {
  const { t } = useI18n()
  if (!phone) return <span className="attendance-nocontact">{t('attendance.noGuardian')}</span>
  return <span className="attendance-contact">{name} · <a href={`tel:${phone.replace(/[^\d+]/g, '')}`}>{phone}</a></span>
}

// Register is a class's attendance for one day. Marks stay in a draft for each
// class and date until saved, so switching class or date does not lose them.
export function Register({ classID, base }: { classID: string; base: '/admin' | '/teach' }) {
  const { t, label, errorText } = useI18n()
  const { busy, run } = useAction()
  const refreshAttendance = useRefreshAttendance()
  const [date, setDate] = useState(todayISO)
  const [drafts, setDrafts] = useState<Record<string, Record<string, Mark>>>({})
  const isFuture = date > todayISO()
  const query = useQuery({
    queryKey: ['/attendance/classes', classID, date],
    queryFn: () => api<RegisterData>(`/attendance/classes/${classID}?date=${date}`),
    enabled: Boolean(classID) && !isFuture,
    meta: { silent: true },
  })
  const rows = query.data?.students ?? []
  const draftKey = `${classID}|${date}`
  const draft = drafts[draftKey] ?? {}
  const changes = Object.keys(draft).length
  const markOf = (studentID: string, saved: Mark) => draft[studentID] ?? saved
  const marks = rows.map((row) => markOf(row.student_id, row))
  const unmarked = marks.filter((mark) => !mark.status).length

  const setMark = (studentID: string, saved: Mark, next: Partial<Mark>) => setDrafts((current) => {
    const forDay = current[draftKey] ?? {}
    return { ...current, [draftKey]: { ...forDay, [studentID]: { ...(forDay[studentID] ?? saved), ...next } } }
  })
  const markRestPresent = () => setDrafts((current) => {
    const forDay = { ...(current[draftKey] ?? {}) }
    for (const row of rows) if (!(forDay[row.student_id] ?? row).status) forDay[row.student_id] = { status: 'present', note: '' }
    return { ...current, [draftKey]: forDay }
  })
  const save = () => run(async () => {
    const records = rows.map((row) => { const { status, note } = markOf(row.student_id, row); return { student_id: row.student_id, status, note: status === 'present' ? '' : note } }).filter((record) => record.status)
    await api(`/attendance/classes/${classID}`, { method: 'PUT', body: JSON.stringify({ date, records }) })
    await refreshAttendance()
    setDrafts((current) => {
      const { [draftKey]: _saved, ...rest } = current
      return rest
    })
  }, t('attendance.saved'))

  return <div className="register">
    <div className="register-head">
      <DateField name="register_date" label={t('attendance.date')} defaultValue={date} onValue={setDate} />
      {query.data && <span className={`status-pill ${query.data.taken ? '' : 'draft'}`}>{query.data.taken ? t('attendance.taken') : t('attendance.notTaken')}</span>}
    </div>
    {isFuture && <p className="empty">{t('attendance.future')}</p>}
    {query.error && <p className="row-error">{errorText(query.error, 'error.load')}</p>}
    {!isFuture && query.isPending && Boolean(classID) && <p className="empty">{t('common.loading')}</p>}
    {query.data && !rows.length && <p className="empty">{t('common.noActiveStudents')}</p>}
    {rows.length > 0 && <>
      <ul className="register-list">
        {rows.map((row) => {
          const mark = markOf(row.student_id, row)
          return <li key={row.student_id} className="register-row">
            <div className="register-name"><Link to={`${base}/students/${row.student_id}`}>{row.student_name}</Link><small>{row.student_number}</small></div>
            <div className="register-marks" role="group" aria-label={row.student_name}>
              {STATUSES.map((status) => <button key={status} type="button" className={`register-mark ${status}`} aria-pressed={mark.status === status} onClick={() => setMark(row.student_id, row, { status })}>{label('attendance.status', status)}</button>)}
            </div>
            {mark.status && mark.status !== 'present' && <input className="register-note" value={mark.note} maxLength={500} placeholder={t('attendance.notePlaceholder')} aria-label={t('attendance.noteFor', { name: row.student_name })} onChange={(event) => setMark(row.student_id, row, { note: event.target.value })} />}
          </li>
        })}
      </ul>
      <div className="register-foot">
        <span>{unmarked > 0 ? t('attendance.unmarked', { n: unmarked }) : t('attendance.allMarked')}{changes > 0 && ` · ${t('attendance.unsaved')}`}</span>
        <div className="operation-actions">
          <button type="button" className="secondary compact" disabled={busy || unmarked === 0} onClick={markRestPresent}>{t('attendance.markRest')}</button>
          <button type="button" className="primary compact" disabled={busy || changes === 0} onClick={() => void save()}>{t('attendance.save')}</button>
        </div>
      </div>
    </>}
  </div>
}

// AttendanceAlerts lists students who need follow-up. Without a class it covers
// the school, which only administrators can ask for.
export function AttendanceAlerts({ classID, base }: { classID?: string; base: '/admin' | '/teach' }) {
  const { t } = useI18n()
  const schoolWide = classID === undefined
  const query = useQuery({
    queryKey: ['/attendance/alerts', classID ?? ''],
    queryFn: () => api<AttendanceAlert[]>(`/attendance/alerts${schoolWide ? '' : `?class_id=${classID}`}`),
    enabled: schoolWide || Boolean(classID),
  })
  const alerts = query.data ?? []
  return <section className="panel">
    <div className="panel-head"><div><h2>{t('attendance.alertsTitle')}</h2><p>{t('attendance.alertsHelp')}</p></div><span className="count-pill">{alerts.length}</span></div>
    <ul className="attendance-people">
      {alerts.map((alert) => <li key={alert.student_id}>
        <div><Link to={`${base}/students/${alert.student_id}`}><strong>{alert.student_name}</strong></Link><small>{classLabel({ grade_level: alert.grade_level, name: alert.class_name })}</small></div>
        <span className="attendance-tag absent">{alert.reason === 'streak' ? t('attendance.reasonStreak', { n: alert.current_streak }) : t('attendance.reasonRate', { absent: alert.days_absent, marked: alert.days_marked })}</span>
        <GuardianContact name={alert.guardian_name} phone={alert.guardian_phone} />
      </li>)}
      {query.isSuccess && !alerts.length && <li className="empty">{t('attendance.alertsEmpty')}</li>}
    </ul>
  </section>
}

// A name starting with one of these runs as a formula when a spreadsheet opens the file.
const csvCell = (value: string | number) => {
  const text = String(value)
  return `"${(/^[=+\-@\t\r]/.test(text) ? `'${text}` : text).replace(/"/g, '""')}"`
}

const attendanceRate = (row: { present: number; late: number; absent: number }) => {
  const days = row.present + row.late + row.absent
  return days ? Math.round(((row.present + row.late) / days) * 100) : null
}

// The report opens on the current month in the calendar the school reads.
function monthStart(calendar: string) {
  const today = parseISODate(todayISO())!
  if (calendar === 'gregorian') return toISODate({ ...today, day: 1 })
  return toISODate(ethiopianToGregorian({ ...gregorianToEthiopian(today), day: 1 }))
}

// AttendanceReport totals a class over a period. Administrators choose the class
// from `classes`; a teacher's report follows the class in `classID`.
export function AttendanceReport({ classID, classes }: { classID?: string; classes?: ClassRoom[] }) {
  const { t, calendar, errorText } = useI18n()
  const [range, setRange] = useState(() => ({ from: monthStart(calendar), to: todayISO() }))
  const [picked, setPicked] = useState('')
  const reportClass = classID ?? (picked || classes?.[0]?.id || '')
  const query = useQuery({
    queryKey: ['/attendance/report', reportClass, range.from, range.to],
    queryFn: () => api<AttendanceReportData>(`/attendance/report?class_id=${reportClass}&from=${range.from}&to=${range.to}`),
    enabled: Boolean(reportClass),
    meta: { silent: true },
  })
  const rows = query.data?.students ?? []
  const apply = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const values = new FormData(event.currentTarget)
    setRange({ from: String(values.get('from')), to: String(values.get('to')) })
  }
  const download = () => {
    const headers = [t('students.id'), t('students.fullName'), t('attendance.status.present'), t('attendance.status.late'), t('attendance.status.absent'), t('attendance.status.excused'), t('attendance.col.rate')]
    const lines = [headers.map(csvCell).join(',')]
    for (const row of rows) {
      const rate = attendanceRate(row)
      lines.push([row.student_number, row.student_name, row.present, row.late, row.absent, row.excused, rate === null ? '' : `${rate}%`].map(csvCell).join(','))
    }
    const link = document.createElement('a')
    link.href = URL.createObjectURL(new Blob([`﻿${lines.join('\r\n')}`], { type: 'text/csv;charset=utf-8' }))
    link.download = `attendance-${range.from}-to-${range.to}.csv`
    link.click()
    URL.revokeObjectURL(link.href)
  }
  return <section className="panel">
    <div className="panel-head">
      <div><h2>{t('attendance.reportTitle')}</h2><p>{t('attendance.reportHelp')}</p></div>
      {query.data && <span className="count-pill">{t('attendance.daysTaken', { n: query.data.days_taken })}</span>}
    </div>
    <form className="attendance-range" onSubmit={apply}>
      {classes && <select aria-label={t('attendance.chooseClass')} value={reportClass} onChange={(event) => setPicked(event.target.value)}>
        {classes.map((c) => <option key={c.id} value={c.id}>{classLabel(c)}</option>)}
      </select>}
      <DateField name="from" label={t('attendance.from')} defaultValue={range.from} required />
      <DateField name="to" label={t('attendance.to')} defaultValue={range.to} required />
      <div className="operation-actions">
        <button className="secondary compact">{t('attendance.showReport')}</button>
        <button type="button" className="secondary compact" disabled={!rows.length} onClick={download}>{t('attendance.download')}</button>
      </div>
    </form>
    {query.error && <p className="row-error">{errorText(query.error, 'error.load')}</p>}
    <div className="table-wrap"><table>
      <thead><tr><th>{t('common.col.student')}</th><th>{t('attendance.status.present')}</th><th>{t('attendance.status.late')}</th><th>{t('attendance.status.absent')}</th><th>{t('attendance.status.excused')}</th><th>{t('attendance.col.rate')}</th></tr></thead>
      <tbody>
        {rows.map((row) => {
          const rate = attendanceRate(row)
          return <tr key={row.student_id}><td><strong>{row.student_name}</strong><small>{row.student_number}</small></td><td>{row.present}</td><td>{row.late}</td><td>{row.absent}</td><td>{row.excused}</td><td>{rate === null ? '—' : `${rate}%`}</td></tr>
        })}
        {query.isSuccess && !rows.length && <tr><td colSpan={6} className="empty">{t('common.noActiveStudents')}</td></tr>}
      </tbody>
    </table></div>
  </section>
}
