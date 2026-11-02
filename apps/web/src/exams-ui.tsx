import { FormEvent, useCallback, useEffect, useState } from 'react'
import { api } from './api'

type ClassOption = { id: string; name: string; grade_level: string }

const errorText = (e: unknown, fallback: string) => (e instanceof Error ? e.message : fallback)
const when = (value: string | null | undefined) => (value ? new Date(value).toLocaleString() : '—')
const clock = (value: string | null | undefined) => (value ? new Date(value).toLocaleTimeString() : '—')

export function gradingGuideText(guide: unknown) {
  if (typeof guide === 'string') return guide
  if (guide && typeof guide === 'object' && 'guide' in guide) return String((guide as { guide: unknown }).guide)
  return JSON.stringify(guide)
}

// ---- Start screen shown to a learner before the exam timer begins ----

export function LearnerStartPanel({ assessment, busy, onStart }: {
  assessment: { question_count: number; duration_minutes: number; requires_start_code: boolean; session_open: boolean; locked_mode: boolean; opens_at: string | null; due_at: string | null; not_open_yet: boolean; past_due: boolean }
  busy: boolean
  onStart: (startCode: string) => void
}) {
  const [code, setCode] = useState('')
  const waitingForSession = assessment.requires_start_code && !assessment.session_open
  return <section className="panel learner-start">
    <p>This assessment has {assessment.question_count} questions and lasts {assessment.duration_minutes} minutes. You can submit it once. The timer starts when you press Start and cannot be paused.</p>
    {assessment.due_at && <p>Start before {when(assessment.due_at)}.</p>}
    {assessment.locked_mode && <p className="form-hint">This exam runs in full screen. Leaving the exam window is recorded and shown to your invigilator.</p>}
    {assessment.not_open_yet
      ? <p className="empty">This assessment opens at {when(assessment.opens_at)}.</p>
      : assessment.past_due
        ? <p className="empty">The time to start this assessment has passed. Ask your teacher if you need help.</p>
        : waitingForSession
          ? <p className="empty">Wait for your invigilator to open this exam session, then reload this page.</p>
          : <form className="start-code-form" onSubmit={(event) => { event.preventDefault(); onStart(code) }}>
              {assessment.requires_start_code && <label>Start code from your invigilator<input value={code} onChange={(e) => setCode(e.target.value.toUpperCase())} maxLength={6} autoComplete="off" autoCapitalize="characters" required /></label>}
              <button className="primary compact" disabled={busy}>{busy ? 'Starting…' : 'Start assessment'} <span>→</span></button>
            </form>}
  </section>
}

// ---- Invigilator console, for administrators and assigned teachers ----

type SessionSummary = { assignment_id: string; title: string; class_name: string; grade_level: string; duration_minutes: number; locked_mode: boolean; requires_start_code: boolean; session_open: boolean; opens_at: string | null; due_at: string | null; enrolled: number; in_progress: number; submitted: number }
type RosterRow = { student_id: string; student_number: string; student_name: string; attempt_id: string; status: string; deadline_at: string | null; submitted_at: string | null; answered: number; last_saved_at: string | null; integrity_flags: number; incidents: number }
type SessionDetail = { assignment_id: string; title: string; duration_minutes: number; question_count: number; locked_mode: boolean; requires_start_code: boolean; session_open: boolean; start_code: string | null; roster: RosterRow[] }
type TimelineEvent = { event_type: string; event_data: Record<string, unknown>; occurred_at: string }

const CONSOLE_REFRESH_MS = 10000

const statusLabel = (status: string) => status.replace(/_/g, ' ')

function describeEvent(event: TimelineEvent) {
  const data = event.event_data ?? {}
  switch (event.event_type) {
    case 'integrity_flag': return `Signal: ${String(data.type ?? '').replace(/_/g, ' ')}`
    case 'incident': return `Incident: ${String(data.note ?? '')}`
    case 'time_extended': return `Extra time: ${String(data.minutes ?? '')} minutes${data.reason ? ` (${String(data.reason)})` : ''}`
    case 'force_submitted': return 'Finished by invigilator'
    case 'deadline_submission': return 'Submitted at the deadline'
    default: return statusLabel(event.event_type)
  }
}

export function ExamSessions() {
  const [sessions, setSessions] = useState<SessionSummary[]>([])
  const [selected, setSelected] = useState('')
  const [error, setError] = useState('')
  const load = useCallback(() => api<SessionSummary[]>('/exam-sessions').then(setSessions).catch((e) => setError(errorText(e, 'Unable to load exam sessions.'))), [])
  useEffect(() => { void load() }, [load])
  return <section className="panel exam-sessions">
    <div className="panel-head"><div><p className="eyebrow">INVIGILATION</p><h2>Exam sessions</h2><p>Open an exam, share the start code, and watch each student's progress.</p></div><span className="count-pill">{sessions.length} assigned</span></div>
    {error && <p className="form-error" role="alert">{error}</p>}
    {sessions.length > 0 && <label className="student-picker">Assessment and class<select value={selected} onChange={(e) => setSelected(e.target.value)}><option value="">Choose a session…</option>{sessions.map((s) => <option key={s.assignment_id} value={s.assignment_id}>{s.title} · {s.grade_level} {s.class_name} · {s.submitted}/{s.enrolled} submitted</option>)}</select></label>}
    {!sessions.length && !error && <p className="empty">No assessments are assigned to your classes yet.</p>}
    {selected && <SessionConsole key={selected} assignmentID={selected} onChanged={() => void load()} />}
  </section>
}

function SessionConsole({ assignmentID, onChanged }: { assignmentID: string; onChanged: () => void }) {
  const [session, setSession] = useState<SessionDetail | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [action, setAction] = useState<{ type: 'extend' | 'incident' | 'finish' | 'timeline'; row: RosterRow } | null>(null)
  const [timeline, setTimeline] = useState<TimelineEvent[]>([])
  const path = `/exam-sessions/${assignmentID}`
  const refresh = useCallback(() => api<SessionDetail>(path).then(setSession).catch((e) => setError(errorText(e, 'Unable to load this session.'))), [path])
  useEffect(() => {
    void refresh()
    const timer = setInterval(() => void refresh(), CONSOLE_REFRESH_MS)
    return () => clearInterval(timer)
  }, [refresh])

  async function run(work: () => Promise<unknown>) {
    setBusy(true); setError('')
    try { await work(); await refresh(); onChanged() } catch (e) { setError(errorText(e, 'That action could not be completed.')) } finally { setBusy(false) }
  }
  async function showTimeline(row: RosterRow) {
    setAction({ type: 'timeline', row }); setTimeline([])
    try { setTimeline(await api<TimelineEvent[]>(`${path}/attempts/${row.attempt_id}/events`)) } catch (e) { setError(errorText(e, 'Unable to load the timeline.')) }
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
  if (!session) return error ? <p className="form-error" role="alert">{error}</p> : <p className="empty">Loading session…</p>
  const started = session.roster.filter((row) => row.status !== 'not_started').length
  const finished = session.roster.filter((row) => row.status === 'submitted').length
  return <div className="session-console">
    {error && <p className="form-error" role="alert">{error}</p>}
    <div className="session-summary">
      <div><strong>{session.title}</strong><small>{session.question_count} questions · {session.duration_minutes} minutes{session.locked_mode ? ' · locked mode' : ''}</small><small>{started} of {session.roster.length} started · {finished} submitted</small></div>
      {session.requires_start_code
        ? <div className="session-code">
            {session.session_open && session.start_code ? <><small>Start code</small><b className="start-code">{session.start_code}</b></> : <small>Session closed. Students cannot start.</small>}
            {session.session_open
              ? <button className="secondary compact" disabled={busy} onClick={() => void run(() => api(`${path}/close`, { method: 'POST', body: '{}' }))}>Close session</button>
              : <button className="primary compact" disabled={busy} onClick={() => void run(() => api(`${path}/open`, { method: 'POST', body: '{}' }))}>Open session</button>}
          </div>
        : <small>No start code: students can start when it opens.</small>}
    </div>
    <div className="table-wrap"><table><thead><tr><th>STUDENT</th><th>STATUS</th><th>ANSWERED</th><th>LAST SAVE</th><th>ENDS</th><th>SIGNALS</th><th></th></tr></thead><tbody>
      {session.roster.map((row) => <tr key={row.student_id}>
        <td><strong>{row.student_name}</strong><small>{row.student_number}</small></td>
        <td><span className="status-pill">{statusLabel(row.status)}</span></td>
        <td>{row.status === 'not_started' ? '—' : `${row.answered} / ${session.question_count}`}</td>
        <td>{clock(row.last_saved_at)}</td>
        <td>{row.status === 'in_progress' ? clock(row.deadline_at) : row.status === 'submitted' ? `Done ${clock(row.submitted_at)}` : '—'}</td>
        <td>{row.integrity_flags + row.incidents > 0 ? <span className="status-pill high">{row.integrity_flags} signals · {row.incidents} incidents</span> : '—'}</td>
        <td className="session-actions">{row.attempt_id && <>
          <button className="staff-toggle" onClick={() => void showTimeline(row)}>Timeline</button>
          <button className="staff-toggle" onClick={() => setAction({ type: 'incident', row })}>Incident</button>
          {row.status === 'in_progress' && <><button className="staff-toggle" onClick={() => setAction({ type: 'extend', row })}>Add time</button><button className="staff-toggle" onClick={() => setAction({ type: 'finish', row })}>Finish</button></>}
        </>}</td>
      </tr>)}
      {!session.roster.length && <tr><td colSpan={7} className="empty">No active students are enrolled in this class.</td></tr>}
    </tbody></table></div>
    {action && action.type !== 'timeline' && <form className="stack-form session-action-form" onSubmit={submitAction}>
      <strong>{action.type === 'extend' ? 'Add time for' : action.type === 'incident' ? 'Record an incident for' : 'Finish the exam for'} {action.row.student_name}</strong>
      {action.type === 'extend' && <div className="two-fields"><label>Extra minutes<input name="minutes" type="number" min="1" max="240" defaultValue="10" required /></label><label>Reason<input name="reason" maxLength={500} placeholder="Power cut, device failure…" /></label></div>}
      {action.type === 'incident' && <label>What happened<textarea name="note" rows={2} maxLength={2000} required /></label>}
      {action.type === 'finish' && <p className="form-hint">This ends the exam now and grades only the answers already saved. It cannot be undone.</p>}
      <div className="session-action-buttons"><button className="primary compact" disabled={busy}>{action.type === 'finish' ? 'Finish exam' : 'Save'}</button><button type="button" className="secondary compact" onClick={() => setAction(null)}>Cancel</button></div>
    </form>}
    {action?.type === 'timeline' && <div className="session-timeline"><div className="session-action-buttons"><strong>Timeline for {action.row.student_name}</strong><button className="staff-toggle" onClick={() => setAction(null)}>Close</button></div>
      <ul>{timeline.map((event, index) => <li key={index}><time>{clock(event.occurred_at)}</time> {describeEvent(event)}</li>)}{!timeline.length && <li>No events yet.</li>}</ul>
    </div>}
  </div>
}

// ---- Gradebook ----

type GradebookCell = { status: string; score: number | null; max_score: number | null; final_score: number | null; final_max_score: number | null; results_released: boolean; pending_review: number }
type GradebookData = { assignments: { assignment_id: string; title: string; max_points: number; due_at: string | null }[]; students: { student_id: string; student_number: string; student_name: string; results: Record<string, GradebookCell> }[] }

function cellText(cell: GradebookCell | undefined) {
  if (!cell) return 'Not started'
  if (cell.status === 'in_progress') return 'In progress'
  if (cell.results_released) return `${cell.final_score ?? 0} / ${cell.final_max_score ?? 0}`
  return `${cell.score ?? 0} / ${cell.max_score ?? 0} so far, ${cell.pending_review} to grade`
}

function downloadCSV(book: GradebookData) {
  const quote = (value: string) => `"${value.replace(/"/g, '""')}"`
  const lines = [['Student ID', 'Student', ...book.assignments.map((a) => a.title)].map(quote).join(',')]
  for (const student of book.students) {
    lines.push([student.student_number, student.student_name, ...book.assignments.map((a) => cellText(student.results[a.assignment_id]))].map(quote).join(','))
  }
  const link = document.createElement('a')
  link.href = URL.createObjectURL(new Blob([`﻿${lines.join('\r\n')}`], { type: 'text/csv;charset=utf-8' }))
  link.download = 'gradebook.csv'
  link.click()
  URL.revokeObjectURL(link.href)
}

export function Gradebook({ classID }: { classID: string }) {
  const [book, setBook] = useState<GradebookData | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    if (!classID) { setBook(null); return }
    let ignore = false
    setError('')
    api<GradebookData>(`/gradebook?class_id=${encodeURIComponent(classID)}`)
      .then((data) => { if (!ignore) setBook(data) })
      .catch((e) => { if (!ignore) setError(errorText(e, 'Unable to load the gradebook.')) })
    return () => { ignore = true }
  }, [classID])
  return <section className="panel gradebook-panel">
    <div className="panel-head"><div><p className="eyebrow">RESULTS</p><h2>Gradebook</h2><p>Each student's result on every assessment assigned to this class.</p></div>{book && book.assignments.length > 0 && <button className="secondary compact" onClick={() => downloadCSV(book)}>Download CSV</button>}</div>
    {error && <p className="form-error" role="alert">{error}</p>}
    {book && !book.assignments.length && <p className="empty">No assessments are assigned to this class yet.</p>}
    {book && book.assignments.length > 0 && <div className="table-wrap"><table><thead><tr><th>STUDENT</th>{book.assignments.map((a) => <th key={a.assignment_id}>{a.title}</th>)}</tr></thead><tbody>
      {book.students.map((student) => <tr key={student.student_id}><td><strong>{student.student_name}</strong><small>{student.student_number}</small></td>{book.assignments.map((a) => <td key={a.assignment_id}>{cellText(student.results[a.assignment_id])}</td>)}</tr>)}
      {!book.students.length && <tr><td colSpan={book.assignments.length + 1} className="empty">No active students are enrolled in this class.</td></tr>}
    </tbody></table></div>}
  </section>
}

// ---- Administrator results page: gradebook plus a grading queue, so results
// are never stuck when a class has no teacher ----

type ReviewItem = { attempt_id: string; question_id: string; assessment_title: string; student_number: string; student_name: string; prompt: { text?: string }; grading_guide?: unknown; answer: unknown; max_points: number; feedback: string }

function GradingQueue({ classID, onGraded }: { classID: string; onGraded: () => void }) {
  const [reviews, setReviews] = useState<ReviewItem[]>([])
  const [error, setError] = useState('')
  const [saving, setSaving] = useState('')
  const load = useCallback(() => api<ReviewItem[]>(`/reviews?class_id=${encodeURIComponent(classID)}`).then(setReviews).catch((e) => setError(errorText(e, 'Unable to load written answers.'))), [classID])
  useEffect(() => { void load() }, [load])
  async function grade(event: FormEvent<HTMLFormElement>, review: ReviewItem) {
    event.preventDefault()
    const values = new FormData(event.currentTarget)
    const id = `${review.attempt_id}:${review.question_id}`
    setSaving(id); setError('')
    try {
      await api(`/attempts/${review.attempt_id}/answers/${review.question_id}/grade`, { method: 'PUT', body: JSON.stringify({ points: Number(values.get('points')), feedback: values.get('feedback') }) })
      await load(); onGraded()
    } catch (e) { setError(errorText(e, 'Unable to save this grade.')) } finally { setSaving('') }
  }
  return <section className="panel teacher-review-panel">
    <div className="panel-head"><div><p className="eyebrow">GRADING</p><h2>Written answers to grade</h2><p>Scores reach learners when every written response in an attempt has been graded. Teachers normally do this; use it here when a class has no teacher.</p></div><span className="count-pill">{reviews.length} pending</span></div>
    {error && <p className="form-error" role="alert">{error}</p>}
    <div className="teacher-review-list">
      {reviews.map((review) => {
        const id = `${review.attempt_id}:${review.question_id}`
        return <article className="teacher-review-card" key={id}>
          <div className="review-meta"><strong>{review.student_name}</strong><span>{review.student_number} · {review.assessment_title}</span></div>
          <h3>{review.prompt?.text || 'Written response'}</h3>
          {Boolean(review.grading_guide) && <p className="review-guide"><strong>Grading guide:</strong> {gradingGuideText(review.grading_guide)}</p>}
          <div className="review-answer">{typeof review.answer === 'string' ? review.answer : JSON.stringify(review.answer)}</div>
          <form className="review-grade-form" onSubmit={(e) => void grade(e, review)}>
            <label>Points earned <span>out of {review.max_points}</span><input name="points" type="number" min="0" max={review.max_points} step="0.01" required /></label>
            <label>Feedback for learner<textarea name="feedback" rows={2} maxLength={5000} defaultValue={review.feedback} /></label>
            <button className="primary compact" disabled={saving === id}>{saving === id ? 'Saving…' : 'Save grade'}</button>
          </form>
        </article>
      })}
      {!reviews.length && !error && <p className="empty">No written answers are waiting for review in this class.</p>}
    </div>
  </section>
}

export function ResultsWorkspace({ classes }: { classes: ClassOption[] }) {
  const [classID, setClassID] = useState('')
  const [version, setVersion] = useState(0)
  useEffect(() => { setClassID((current) => (classes.some((c) => c.id === current) ? current : classes[0]?.id ?? '')) }, [classes])
  if (!classes.length) return <section className="panel"><p className="empty">Create a class first. Results appear here once assessments are assigned.</p></section>
  return <div className="operations-page">
    <section className="panel"><label className="student-picker">Class<select value={classID} onChange={(e) => setClassID(e.target.value)}>{classes.map((c) => <option key={c.id} value={c.id}>{c.grade_level} · {c.name}</option>)}</select></label></section>
    {classID && <Gradebook key={`${classID}:${version}`} classID={classID} />}
    {classID && <GradingQueue key={classID} classID={classID} onGraded={() => setVersion((v) => v + 1)} />}
  </div>
}
