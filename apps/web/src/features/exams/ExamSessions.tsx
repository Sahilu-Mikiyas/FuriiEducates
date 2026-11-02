import { FormEvent, useCallback, useEffect, useState } from 'react'
import { useDates } from '../../calendar/useDates'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import type { RosterRow, SessionDetail, SessionSummary, TimelineEvent } from '../../lib/types'

const CONSOLE_REFRESH_MS = 10000

// ExamSessions is the invigilator console, for administrators and assigned teachers.
export function ExamSessions() {
  const { t, errorText } = useI18n()
  const [sessions, setSessions] = useState<SessionSummary[]>([])
  const [selected, setSelected] = useState('')
  const [error, setError] = useState('')
  const load = useCallback(() => api<SessionSummary[]>('/exam-sessions').then(setSessions).catch((e) => setError(errorText(e, 'error.load'))), [errorText])
  useEffect(() => { void load() }, [load])
  return <section className="panel exam-sessions">
    <div className="panel-head"><div><p className="eyebrow">{t('sessions.eyebrow')}</p><h2>{t('sessions.title')}</h2><p>{t('sessions.help')}</p></div><span className="count-pill">{t('sessions.assignedCount', { n: sessions.length })}</span></div>
    {error && <p className="form-error" role="alert">{error}</p>}
    {sessions.length > 0 && <label className="student-picker">{t('sessions.choose')}<select value={selected} onChange={(e) => setSelected(e.target.value)}>
      <option value="">{t('sessions.choosePlaceholder')}</option>
      {sessions.map((s) => <option key={s.assignment_id} value={s.assignment_id}>{s.title} · {s.grade_level} {s.class_name} · {t('sessions.submittedOf', { n: s.submitted, total: s.enrolled })}</option>)}
    </select></label>}
    {!sessions.length && !error && <p className="empty">{t('sessions.empty')}</p>}
    {selected && <SessionConsole key={selected} assignmentID={selected} onChanged={() => void load()} />}
  </section>
}

function SessionConsole({ assignmentID, onChanged }: { assignmentID: string; onChanged: () => void }) {
  const { t, label, errorText } = useI18n()
  const { time } = useDates()
  const [session, setSession] = useState<SessionDetail | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [action, setAction] = useState<{ type: 'extend' | 'incident' | 'finish' | 'timeline'; row: RosterRow } | null>(null)
  const [timeline, setTimeline] = useState<TimelineEvent[]>([])
  const path = `/exam-sessions/${assignmentID}`
  const refresh = useCallback(() => api<SessionDetail>(path).then(setSession).catch((e) => setError(errorText(e, 'error.load'))), [path, errorText])
  useEffect(() => {
    void refresh()
    const timer = setInterval(() => void refresh(), CONSOLE_REFRESH_MS)
    return () => clearInterval(timer)
  }, [refresh])

  function describeEvent(event: TimelineEvent) {
    const data = event.event_data ?? {}
    switch (event.event_type) {
      case 'integrity_flag': return t('sessions.event.signal', { type: label('signal', String(data.type ?? '')) })
      case 'incident': return t('sessions.event.incident', { note: String(data.note ?? '') })
      case 'time_extended': return t('sessions.event.extended', { n: String(data.minutes ?? '') }) + (data.reason ? ` (${String(data.reason)})` : '')
      case 'force_submitted': return t('sessions.event.finished')
      case 'deadline_submission': return t('sessions.event.deadline')
      default: return label('sessions.event', event.event_type)
    }
  }

  async function run(work: () => Promise<unknown>) {
    setBusy(true); setError('')
    try { await work(); await refresh(); onChanged() } catch (e) { setError(errorText(e, 'error.generic')) } finally { setBusy(false) }
  }
  async function showTimeline(row: RosterRow) {
    setAction({ type: 'timeline', row }); setTimeline([])
    try { setTimeline(await api<TimelineEvent[]>(`${path}/attempts/${row.attempt_id}/events`)) } catch (e) { setError(errorText(e, 'error.load')) }
  }
  function submitAction(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!action) return
    const values = new FormData(event.currentTarget)
    const attempt = `${path}/attempts/${action.row.attempt_id}`
    void run(async () => {
      if (action.type === 'extend') await api(`${attempt}/extend`, { method: 'POST', body: JSON.stringify({ minutes: Number(values.get('minutes')), reason: values.get('reason') }) })
      if (action.type === 'incident') await api(`${attempt}/incident`, { method: 'POST', body: JSON.stringify({ note: values.get('note') }) })
      if (action.type === 'finish') await api(`${attempt}/finish`, { method: 'POST', body: '{}' })
      setAction(null)
    })
  }
  if (!session) return error ? <p className="form-error" role="alert">{error}</p> : <p className="empty">{t('common.loading')}</p>
  const started = session.roster.filter((row) => row.status !== 'not_started').length
  const finished = session.roster.filter((row) => row.status === 'submitted').length
  const actionTitle = action?.type === 'extend' ? 'sessions.addTimeFor' : action?.type === 'incident' ? 'sessions.incidentFor' : 'sessions.finishFor'
  return <div className="session-console">
    {error && <p className="form-error" role="alert">{error}</p>}
    <div className="session-summary">
      <div>
        <strong>{session.title}</strong>
        <small>{t('common.questionsCount', { n: session.question_count })} · {t('common.minutesCount', { n: session.duration_minutes })}{session.locked_mode ? ` · ${t('assessments.lockedTag')}` : ''}</small>
        <small>{t('sessions.startedOf', { n: started, total: session.roster.length })} · {t('sessions.submittedCount', { n: finished })}</small>
      </div>
      {session.requires_start_code
        ? <div className="session-code">
            {session.session_open && session.start_code ? <><small>{t('sessions.startCode')}</small><b className="start-code">{session.start_code}</b></> : <small>{t('sessions.closedNote')}</small>}
            {session.session_open
              ? <button className="secondary compact" disabled={busy} onClick={() => void run(() => api(`${path}/close`, { method: 'POST', body: '{}' }))}>{t('sessions.close')}</button>
              : <button className="primary compact" disabled={busy} onClick={() => void run(() => api(`${path}/open`, { method: 'POST', body: '{}' }))}>{t('sessions.open')}</button>}
          </div>
        : <small>{t('sessions.noCode')}</small>}
    </div>
    <div className="table-wrap"><table>
      <thead><tr><th>{t('common.col.student')}</th><th>{t('common.col.status')}</th><th>{t('sessions.col.answered')}</th><th>{t('sessions.col.lastSave')}</th><th>{t('sessions.col.ends')}</th><th>{t('sessions.col.signals')}</th><th></th></tr></thead>
      <tbody>
        {session.roster.map((row) => <tr key={row.student_id}>
          <td><strong>{row.student_name}</strong><small>{row.student_number}</small></td>
          <td><span className="status-pill">{label('status', row.status)}</span></td>
          <td>{row.status === 'not_started' ? '—' : `${row.answered} / ${session.question_count}`}</td>
          <td>{time(row.last_saved_at)}</td>
          <td>{row.status === 'in_progress' ? time(row.deadline_at) : row.status === 'submitted' ? t('sessions.doneAt', { time: time(row.submitted_at) }) : '—'}</td>
          <td>{row.integrity_flags + row.incidents > 0 ? <span className="status-pill high">{t('sessions.signalCounts', { signals: row.integrity_flags, incidents: row.incidents })}</span> : '—'}</td>
          <td className="session-actions">{row.attempt_id && <>
            <button className="staff-toggle" onClick={() => void showTimeline(row)}>{t('sessions.timeline')}</button>
            <button className="staff-toggle" onClick={() => setAction({ type: 'incident', row })}>{t('sessions.incident')}</button>
            {row.status === 'in_progress' && <>
              <button className="staff-toggle" onClick={() => setAction({ type: 'extend', row })}>{t('sessions.addTime')}</button>
              <button className="staff-toggle" onClick={() => setAction({ type: 'finish', row })}>{t('sessions.finish')}</button>
            </>}
          </>}</td>
        </tr>)}
        {!session.roster.length && <tr><td colSpan={7} className="empty">{t('common.noActiveStudents')}</td></tr>}
      </tbody>
    </table></div>
    {action && action.type !== 'timeline' && <form className="stack-form session-action-form" onSubmit={submitAction}>
      <strong>{t(actionTitle, { name: action.row.student_name })}</strong>
      {action.type === 'extend' && <div className="two-fields">
        <label>{t('sessions.extraMinutes')}<input name="minutes" type="number" min="1" max="240" defaultValue="10" required /></label>
        <label>{t('sessions.reason')}<input name="reason" maxLength={500} placeholder={t('sessions.reasonHint')} /></label>
      </div>}
      {action.type === 'incident' && <label>{t('sessions.whatHappened')}<textarea name="note" rows={2} maxLength={2000} required /></label>}
      {action.type === 'finish' && <p className="form-hint">{t('sessions.finishWarning')}</p>}
      <div className="session-action-buttons">
        <button className="primary compact" disabled={busy}>{action.type === 'finish' ? t('sessions.finishExam') : t('common.save')}</button>
        <button type="button" className="secondary compact" onClick={() => setAction(null)}>{t('common.cancel')}</button>
      </div>
    </form>}
    {action?.type === 'timeline' && <div className="session-timeline">
      <div className="session-action-buttons"><strong>{t('sessions.timelineFor', { name: action.row.student_name })}</strong><button className="staff-toggle" onClick={() => setAction(null)}>{t('common.close')}</button></div>
      <ul>
        {timeline.map((event, index) => <li key={index}><time>{time(event.occurred_at)}</time> {describeEvent(event)}</li>)}
        {!timeline.length && <li>{t('sessions.noEvents')}</li>}
      </ul>
    </div>}
  </div>
}
