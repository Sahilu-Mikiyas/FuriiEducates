import { useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router'
import { DateField } from '../../calendar/DateField'
import { todayISO } from '../../calendar/ethiopian'
import { AttendanceAlerts, AttendanceReport, GuardianContact, Register } from '../../features/attendance/attendance'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useClasses } from '../../lib/queries'
import { AttendanceOverview, classLabel } from '../../lib/types'

// The office's day: which registers are in, who to call, and who keeps missing school.
export default function AttendancePage() {
  const { t } = useI18n()
  const classes = useClasses().data ?? []
  const [date, setDate] = useState(todayISO)
  const [registerClass, setRegisterClass] = useState('')
  const registerPanel = useRef<HTMLElement>(null)
  const isFuture = date > todayISO()
  const overview = useQuery({
    queryKey: ['/attendance/overview', date],
    queryFn: () => api<AttendanceOverview>(`/attendance/overview?date=${date}`),
    enabled: !isFuture,
  }).data
  const rows = overview?.classes ?? []
  const absentees = overview?.absentees ?? []
  const taken = rows.filter((row) => row.marked > 0).length
  const openRegister = (classID: string) => {
    setRegisterClass(classID)
    registerPanel.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }
  const selected = registerClass || classes[0]?.id || ''

  return <div className="operations-page">
    <section className="panel">
      <div className="panel-head">
        <div><h2>{t('attendance.dayTitle')}</h2><p>{t('attendance.dayHelp')}</p></div>
        <span className="count-pill">{t('attendance.registersIn', { taken, total: rows.length })}</span>
      </div>
      <div className="register-head"><DateField name="overview_date" label={t('attendance.date')} defaultValue={date} onValue={setDate} /></div>
      {isFuture && <p className="empty">{t('attendance.future')}</p>}
      <div className="table-wrap"><table>
        <thead><tr><th>{t('attendance.col.class')}</th><th>{t('common.col.status')}</th><th>{t('attendance.status.present')}</th><th>{t('attendance.status.late')}</th><th>{t('attendance.status.absent')}</th><th>{t('attendance.status.excused')}</th><th /></tr></thead>
        <tbody>
          {rows.map((row) => <tr key={row.class_id}>
            <td><strong>{classLabel({ grade_level: row.grade_level, name: row.name })}</strong><small>{t('teacher.learners', { n: row.students })}</small></td>
            <td><span className={`status-pill ${row.marked ? '' : 'draft'}`}>{row.marked ? t('attendance.taken') : t('attendance.notTaken')}</span></td>
            <td>{row.present}</td><td>{row.late}</td><td>{row.absent}</td><td>{row.excused}</td>
            <td><button className="secondary compact" onClick={() => openRegister(row.class_id)}>{t('attendance.open')}</button></td>
          </tr>)}
          {overview && !rows.length && <tr><td colSpan={7} className="empty">{t('attendance.noClasses')}</td></tr>}
        </tbody>
      </table></div>
    </section>

    <section className="panel">
      <div className="panel-head"><div><h2>{t('attendance.callTitle')}</h2><p>{t('attendance.callHelp')}</p></div><span className="count-pill">{absentees.length}</span></div>
      <ul className="attendance-people">
        {absentees.map((person) => <li key={`${person.student_id}:${person.class_name}`}>
          <div><Link to={`/admin/students/${person.student_id}`}><strong>{person.student_name}</strong></Link><small>{classLabel({ grade_level: person.grade_level, name: person.class_name })}{person.note && ` · ${person.note}`}</small></div>
          <GuardianContact name={person.guardian_name} phone={person.guardian_phone} />
        </li>)}
        {overview && !absentees.length && <li className="empty">{t('attendance.callEmpty')}</li>}
      </ul>
    </section>

    <section className="panel" ref={registerPanel}>
      <div className="panel-head"><div><h2>{t('attendance.registerTitle')}</h2><p>{t('attendance.registerHelpAdmin')}</p></div></div>
      <select className="register-class" aria-label={t('attendance.chooseClass')} value={selected} onChange={(event) => setRegisterClass(event.target.value)}>
        {classes.map((c) => <option key={c.id} value={c.id}>{classLabel(c)}</option>)}
      </select>
      {selected ? <Register classID={selected} base="/admin" /> : <p className="empty">{t('attendance.noClasses')}</p>}
    </section>

    <AttendanceAlerts base="/admin" />
    <AttendanceReport classes={classes} />
  </div>
}
