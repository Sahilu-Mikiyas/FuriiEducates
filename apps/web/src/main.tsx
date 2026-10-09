import { FormEvent, useCallback, useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import './style.css'

type Identity = { user_id: string; school_id: string; display_name: string; roles: string[] }
type School = { id: string; name: string; timezone: string; default_language: string }
type Year = { id: string; name: string; starts_on: string; ends_on: string }
type ClassRoom = { id: string; name: string; grade_level: string; academic_year_id: string; academic_year_name?: string }
type Student = { id: string; student_number: string; legal_name: string; preferred_language: string; status: string }
type AcademicRecord = { id: string; school_name: string; academic_year_label: string; grade_level: string; subject_name: string; score: number | null; maximum_score: number | null; grade_label: string; grading_scale_name: string; source_type: string; verification_status: string }
type ExternalExam = { id: string; exam_name: string; exam_provider: string; exam_date: string; subject_or_section: string; score: number | null; maximum_score: number | null; score_label: string; verification_status: string; breakdowns: { section_name: string; score: number | null; maximum_score: number | null }[] }
type ImportPreview = { id: string; status: string; row_count: number; valid_rows: number; error_rows: number; can_commit: boolean; row_errors: { row: number; errors: string[] }[] }
type Curriculum = { id: string; name: string; provider: string; country_code: string; version_label: string; language_code: string; source_reference: string; license_notes: string }
type Subject = { id: string; name: string; grade_level: string; subject_code: string }
type CurriculumNode = { id: string; parent_id: string; node_type: string; code: string; title: string; description: string; sort_order: number }
type Question = { id: string; question_type: string; prompt: { text?: string }; options: { id: string; text: string }[] | null; explanation: { text?: string } | null; difficulty: number | null; language_code: string; source_name: string; source_reference: string; license_notes: string; status: string; skill_tags: { skill_id: string; title: string; weight: number }[] }

const API_BASE_URL = (import.meta.env.VITE_API_BASE_URL ?? '').trim().replace(/\/+$/, '')
const apiURL = (path: string) => `${API_BASE_URL}/api/v1${path}`

async function readAPIResponse(response: Response) {
  const contentType = response.headers.get('content-type')?.toLowerCase() ?? ''
  if (!contentType.includes('json')) {
    throw new Error(`The school API returned a non-JSON page (HTTP ${response.status}). The Vercel frontend is not connected to the Go API yet. Deploy the API, set VITE_API_BASE_URL to its HTTPS URL, and redeploy the frontend.`)
  }
  try {
    return await response.json()
  } catch {
    throw new Error('The school API returned invalid JSON. Check the API deployment and its logs.')
  }
}

const api = async <T,>(path: string, init?: RequestInit): Promise<T> => {
  const headers = new Headers(init?.headers)
  headers.set('Content-Type', 'application/json')
  const response = await fetch(apiURL(path), {
    ...init,
    credentials: 'include',
    headers,
  })
  const body = await readAPIResponse(response)
  if (!response.ok) throw new Error(body?.error?.message ?? 'The request could not be completed.')
  return body.data as T
}

function App() {
  const [identity, setIdentity] = useState<Identity | null>(null)
  const [school, setSchool] = useState<School | null>(null)
  const [years, setYears] = useState<Year[]>([])
  const [classes, setClasses] = useState<ClassRoom[]>([])
  const [students, setStudents] = useState<Student[]>([])
  const [passportStudent, setPassportStudent] = useState('')
  const [academicRecords, setAcademicRecords] = useState<AcademicRecord[]>([])
  const [externalExams, setExternalExams] = useState<ExternalExam[]>([])
  const [importPreview, setImportPreview] = useState<ImportPreview | null>(null)
  const [curricula, setCurricula] = useState<Curriculum[]>([])
  const [subjects, setSubjects] = useState<Subject[]>([])
  const [curriculumNodes, setCurriculumNodes] = useState<CurriculumNode[]>([])
  const [questions, setQuestions] = useState<Question[]>([])
  const [curriculumID, setCurriculumID] = useState('')
  const [subjectID, setSubjectID] = useState('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const refresh = useCallback(async () => {
    const [schoolData, yearData, classData, studentData] = await Promise.all([
      api<School>('/school'), api<Year[]>('/academic-years'), api<ClassRoom[]>('/classes'), api<Student[]>('/students'),
    ])
    setSchool(schoolData); setYears(yearData); setClasses(classData); setStudents(studentData)
    setPassportStudent((current) => current || studentData[0]?.id || '')
  }, [])

  useEffect(() => {
    api<Identity>('/me').then(async (user) => { setIdentity(user); await refresh() })
      .catch(() => setIdentity(null)).finally(() => setLoading(false))
  }, [refresh])

  useEffect(() => {
    if (!passportStudent) { setAcademicRecords([]); setExternalExams([]); return }
    let ignore = false
    Promise.all([
      api<AcademicRecord[]>(`/students/${passportStudent}/academic-records`),
      api<ExternalExam[]>(`/students/${passportStudent}/external-exams`),
    ]).then(([records, exams]) => { if (!ignore) { setAcademicRecords(records); setExternalExams(exams) } })
      .catch((e) => { if (!ignore) setError(e instanceof Error ? e.message : 'Unable to load academic history.') })
    return () => { ignore = true }
  }, [passportStudent])

  useEffect(() => {
    if (!identity) return
    Promise.all([api<Curriculum[]>('/curricula'), api<Question[]>('/questions')]).then(([cs, qs]) => {
      setCurricula(cs); setQuestions(qs); setCurriculumID((current) => current || cs[0]?.id || '')
    }).catch((e) => setError(e instanceof Error ? e.message : 'Unable to load curriculum workspace.'))
  }, [identity])

  useEffect(() => {
    if (!curriculumID) { setSubjects([]); setSubjectID(''); return }
    api<Subject[]>(`/curricula/${curriculumID}/subjects`).then((items) => { setSubjects(items); setSubjectID((current) => items.some((x) => x.id === current) ? current : items[0]?.id || '') })
      .catch((e) => setError(e instanceof Error ? e.message : 'Unable to load subjects.'))
  }, [curriculumID])

  useEffect(() => {
    if (!subjectID) { setCurriculumNodes([]); return }
    api<CurriculumNode[]>(`/subjects/${subjectID}/nodes`).then(setCurriculumNodes)
      .catch((e) => setError(e instanceof Error ? e.message : 'Unable to load curriculum skills.'))
  }, [subjectID])

  async function submit(event: FormEvent<HTMLFormElement>, action: () => Promise<void>, message: string) {
    event.preventDefault(); setError(''); setNotice(''); setBusy(true)
    try { await action(); setNotice(message) } catch (e) { setError(e instanceof Error ? e.message : 'Something went wrong.') } finally { setBusy(false) }
  }

  async function login(event: FormEvent<HTMLFormElement>) {
    const form = new FormData(event.currentTarget)
    const user = await api<Identity>('/auth/login', { method: 'POST', body: JSON.stringify({ email: form.get('email'), password: form.get('password') }) })
    if (!user.roles.includes('school_admin')) {
      await api('/auth/logout', { method: 'POST' })
      throw new Error('This workspace is currently available to school administrators.')
    }
    setIdentity(user); await refresh()
  }

  async function logout() {
    await api('/auth/logout', { method: 'POST' }); setIdentity(null); setSchool(null); setYears([]); setClasses([]); setStudents([])
  }

  async function previewCSV(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setError(''); setNotice(''); setBusy(true)
    const form = event.currentTarget
    const file = (new FormData(form).get('file')) as File | null
    if (!file || file.size === 0) { setError('Choose a CSV file to preview.'); setBusy(false); return }
    try {
      const response = await fetch(apiURL('/import-jobs/preview'), { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'text/csv' }, body: await file.text() })
      const body = await readAPIResponse(response)
      if (!response.ok) throw new Error(body?.error?.message ?? 'Could not preview this CSV.')
      setImportPreview(body.data as ImportPreview)
      setNotice('Import preview ready. Review every row before committing.')
    } catch (e) { setError(e instanceof Error ? e.message : 'Unable to preview this CSV.') } finally { setBusy(false) }
  }

  async function commitCSV() {
    if (!importPreview) return
    setError(''); setNotice(''); setBusy(true)
    try {
      await api(`/import-jobs/${importPreview.id}/commit`, { method: 'POST', body: '{}' })
      setImportPreview({ ...importPreview, status: 'committed', can_commit: false })
      setNotice('Academic history import completed.')
      const records = await api<AcademicRecord[]>(`/students/${passportStudent}/academic-records`)
      setAcademicRecords(records)
    } catch (e) { setError(e instanceof Error ? e.message : 'Unable to commit the import.') } finally { setBusy(false) }
  }

  async function createCurriculum(event: FormEvent<HTMLFormElement>) {
    const form = event.currentTarget
    const f = new FormData(form)
    const created = await api<{ id: string }>('/curricula', { method: 'POST', body: JSON.stringify({ name: f.get('curriculum_name'), provider: f.get('provider'), country_code: f.get('country_code'), version_label: f.get('version_label'), language_code: f.get('language_code') || 'en', source_reference: f.get('curriculum_source'), license_notes: f.get('curriculum_license') }) })
    const items = await api<Curriculum[]>('/curricula'); setCurricula(items); setCurriculumID(created.id); form.reset()
  }

  async function createSubject(event: FormEvent<HTMLFormElement>) {
    const form = event.currentTarget; const f = new FormData(form)
    await api(`/curricula/${curriculumID}/subjects`, { method: 'POST', body: JSON.stringify({ name: f.get('subject_name'), grade_level: f.get('grade_level'), subject_code: f.get('subject_code') }) })
    setSubjects(await api<Subject[]>(`/curricula/${curriculumID}/subjects`)); form.reset()
  }

  async function createNode(event: FormEvent<HTMLFormElement>) {
    const form = event.currentTarget; const f = new FormData(form)
    await api(`/subjects/${subjectID}/nodes`, { method: 'POST', body: JSON.stringify({ parent_id: f.get('parent_id'), node_type: f.get('node_type'), code: f.get('node_code'), title: f.get('node_title'), description: f.get('node_description'), sort_order: Number(f.get('sort_order') || 0) }) })
    setCurriculumNodes(await api<CurriculumNode[]>(`/subjects/${subjectID}/nodes`)); form.reset()
  }

  async function createQuestion(event: FormEvent<HTMLFormElement>) {
    const form = event.currentTarget; const f = new FormData(form); const type = String(f.get('question_type'))
    const lines = String(f.get('options') || '').split('\n').map((line) => line.trim()).filter(Boolean)
    const options = lines.map((line) => { const [optionID, ...text] = line.split('|'); return { id: optionID.trim(), text: text.join('|').trim() } })
    let answerKey: unknown
    try { answerKey = JSON.parse(String(f.get('answer_key') || 'null')) } catch { throw new Error('Answer key must be valid JSON (for example ["A"] or {"value":12,"tolerance":0.5}).') }
    const selectedSkill = String(f.get('skill_id') || '')
    await api('/questions', { method: 'POST', body: JSON.stringify({ question_type: type, prompt: { text: f.get('prompt') }, options: type === 'single_choice' || type === 'multiple_choice' ? options : null, answer_key: answerKey, explanation: { text: f.get('explanation') }, difficulty: Number(f.get('difficulty')), language_code: f.get('language_code') || 'en', source_name: f.get('source_name'), source_reference: f.get('source_reference'), license_notes: f.get('license_notes'), skill_tags: selectedSkill ? [{ skill_id: selectedSkill, weight: 1 }] : [] }) })
    setQuestions(await api<Question[]>('/questions')); form.reset()
  }

  async function publishQuestion(questionID: string) {
    await api(`/questions/${questionID}/publish`, { method: 'POST', body: '{}' })
    setQuestions(await api<Question[]>('/questions'))
  }

  if (loading) return <main className="loading">Opening FURII School OS…</main>
  if (!identity) return <main className="login-shell">
    <div className="login-art"><Brand /><div className="login-copy"><p className="eyebrow">A SCHOOL THAT SEES EVERY LEARNER</p><h1>Build a stronger<br />academic foundation.</h1><p>Bring your school records, teaching, and assessment into one connected place.</p><div className="art-orbit orbit-one"/><div className="art-orbit orbit-two"/><div className="art-spark">✳</div></div><span className="login-footnote">FURII SCHOOL OS <span>·</span> MADE FOR LEARNING</span></div>
    <div className="login-pane"><form className="login-form" onSubmit={(e) => void submit(e, () => login(e), 'Welcome back.')}>
      <p className="eyebrow">ADMINISTRATOR ACCESS</p><h2>Welcome back</h2><p className="muted">Sign in to manage your school workspace.</p>
      <label>Email address<input name="email" type="email" autoComplete="username" placeholder="you@school.edu" required /></label>
      <label>Password<input name="password" type="password" autoComplete="current-password" placeholder="Enter your password" required /></label>
      {error && <p className="form-error" role="alert">{error}</p>}<button className="primary" disabled={busy}>{busy ? 'Signing in…' : 'Sign in'} <span>→</span></button>
      <p className="login-help">Need access? Ask your school administrator to provision your account.</p>
    </form></div>
  </main>

  return <div className="app-shell">
    <aside className="sidebar"><Brand /><div className="school-switch"><div className="school-avatar">{school?.name.slice(0,1) ?? 'F'}</div><div><strong>{school?.name ?? 'Your school'}</strong><span>School workspace</span></div><b>⌄</b></div>
      <p className="nav-heading">WORKSPACE</p><a className="nav-item active" href="#overview"><span>▦</span> Overview</a><a className="nav-item" href="#students"><span>♧</span> Students <small>{students.length}</small></a><a className="nav-item" href="#classes"><span>▤</span> Classes</a><a className="nav-item" href="#school"><span>⚙</span> School settings</a>
      <div className="sidebar-bottom"><div className="user-chip"><div className="user-avatar">{identity.display_name.slice(0,1).toUpperCase()}</div><div><strong>{identity.display_name}</strong><span>Administrator</span></div><button aria-label="Sign out" title="Sign out" onClick={() => void logout()}>↗</button></div></div>
    </aside>
    <main className="main-area" id="overview"><header className="topbar"><span>Workspace <b>/</b> Overview</span><div className="top-actions"><span className="connection"><i/> All changes saved</span><button className="icon-button" title="Help">?</button><div className="user-avatar small">{identity.display_name.slice(0,1).toUpperCase()}</div></div></header>
      <div className="content"><div className="page-title"><div><p className="eyebrow">SCHOOL OVERVIEW</p><h1>Good day, {identity.display_name.split(' ')[0]} <span className="wave">✳</span></h1><p className="muted">Here’s your school’s academic foundation at a glance.</p></div><div className="date-pill">{new Intl.DateTimeFormat('en', { weekday: 'short', month: 'short', day: 'numeric' }).format(new Date())}</div></div>
      {(error || notice) && <div className={error ? 'toast error-toast' : 'toast'} role="status">{error || notice}<button onClick={() => {setError('');setNotice('')}} aria-label="Dismiss">×</button></div>}
      <section className="metrics"><Metric icon="♧" label="Enrolled students" value={students.length.toString()} detail="Across your school"/><Metric icon="▤" label="Active classes" value={classes.length.toString()} detail="In all academic years"/><Metric icon="◷" label="Academic years" value={years.length.toString()} detail="School calendar"/><Metric icon="✳" label="Learning insights" value="—" detail="Available after assessments"/></section>
      <section className="work-grid"><div className="panel" id="students"><div className="panel-head"><div><h2>Students</h2><p>Manage your school’s student records.</p></div><span className="count-pill">{students.length} total</span></div>
        <form className="inline-form student-form" onSubmit={(e) => { const form=e.currentTarget; void submit(e, async () => { const f=new FormData(form); await api('/students',{method:'POST',body:JSON.stringify({student_number:f.get('student_number'),legal_name:f.get('legal_name'),preferred_language:f.get('preferred_language'),class_id:f.get('class_id')})}); form.reset(); await refresh() }, 'Student enrolled.') }}>
          <input name="student_number" placeholder="Student ID" aria-label="Student ID" required/><input name="legal_name" placeholder="Student full name" aria-label="Student full name" required/><select name="class_id" aria-label="Assign to class"><option value="">No class yet</option>{classes.map(c=><option key={c.id} value={c.id}>{c.grade_level} · {c.name}</option>)}</select><button className="primary compact" disabled={busy}>Add student</button>
        </form>
        <div className="table-wrap"><table><thead><tr><th>STUDENT</th><th>STUDENT ID</th><th>STATUS</th></tr></thead><tbody>{students.length ? students.map(s=><tr key={s.id}><td><div className="student-cell"><div className="student-avatar">{s.legal_name.slice(0,1).toUpperCase()}</div><strong>{s.legal_name}</strong></div></td><td>{s.student_number}</td><td><span className="status-pill">{s.status}</span></td></tr>) : <tr><td colSpan={3} className="empty">No students yet. Add your first student above.</td></tr>}</tbody></table></div>
      </div>
      <div className="right-stack"><section className="panel" id="classes"><div className="panel-head"><div><h2>Classes</h2><p>Set up a learning group.</p></div></div>
        <form className="stack-form" onSubmit={(e) => { const form=e.currentTarget; void submit(e, async () => { const f=new FormData(form); await api('/classes',{method:'POST',body:JSON.stringify({name:f.get('name'),grade_level:f.get('grade_level'),academic_year_id:f.get('academic_year_id')})}); form.reset(); await refresh() }, 'Class created.') }}><label>Class name<input name="name" placeholder="e.g. Section A" required/></label><div className="two-fields"><label>Grade<input name="grade_level" placeholder="Grade 9" required/></label><label>Academic year<select name="academic_year_id" required><option value="">Choose year</option>{years.map(y=><option key={y.id} value={y.id}>{y.name}</option>)}</select></label></div><button className="secondary" disabled={busy || !years.length}>{years.length ? 'Create class' : 'Add an academic year first'} <span>→</span></button></form>
        {classes.length>0&&<div className="mini-list">{classes.slice(0,4).map(c=><div key={c.id}><span className="class-icon">▤</span><strong>{c.grade_level} · {c.name}</strong><small>{c.academic_year_name ?? years.find(y=>y.id===c.academic_year_id)?.name}</small></div>)}</div>}
      </section>
      <section className="panel" id="school"><div className="panel-head"><div><h2>Academic year</h2><p>Define your school calendar.</p></div><span className="year-icon">◷</span></div>
        <form className="stack-form year-form" onSubmit={(e) => { const form=e.currentTarget; void submit(e, async () => { const f=new FormData(form); await api('/academic-years',{method:'POST',body:JSON.stringify({name:f.get('name'),starts_on:f.get('starts_on'),ends_on:f.get('ends_on')})}); form.reset(); await refresh() }, 'Academic year added.') }}><label>Year label<input name="name" placeholder="2026–2027" required/></label><div className="two-fields"><label>Starts<input name="starts_on" type="date" required/></label><label>Ends<input name="ends_on" type="date" required/></label></div><button className="secondary" disabled={busy}>Add academic year <span>→</span></button></form>
      </section></div></section>
      <section className="panel passport-panel" id="academic-passport"><div className="panel-head"><div><p className="eyebrow">ACADEMIC PASSPORT</p><h2>Academic history</h2><p>Keep prior school results and external exam evidence together.</p></div><label className="student-picker">Student<select value={passportStudent} onChange={(e)=>setPassportStudent(e.target.value)}><option value="">Select student</option>{students.map(s=><option key={s.id} value={s.id}>{s.legal_name} · {s.student_number}</option>)}</select></label></div>
        {!students.length&&<p className="empty">Add a student first to manage an Academic Passport.</p>}
        {!!passportStudent&&<><div className="passport-grid"><div className="passport-column"><h3>Previous school records <span>{academicRecords.length}</span></h3><form className="stack-form history-form" onSubmit={(e)=>{const form=e.currentTarget;void submit(e,async()=>{const f=new FormData(form);const score=String(f.get('score')??'');const max=String(f.get('maximum_score')??'');await api(`/students/${passportStudent}/academic-records`,{method:'POST',body:JSON.stringify({school_name:f.get('school_name'),academic_year_label:f.get('academic_year_label'),grade_level:f.get('grade_level'),subject_name:f.get('subject_name'),score:score===''?null:Number(score),maximum_score:max===''?null:Number(max),grade_label:f.get('grade_label'),source_type:'staff_entry'})});form.reset();setAcademicRecords(await api<AcademicRecord[]>(`/students/${passportStudent}/academic-records`))},'Historical result added.')}}>
          <div className="history-fields"><input name="school_name" placeholder="Previous school" aria-label="Previous school" required/><input name="academic_year_label" placeholder="Academic year" aria-label="Academic year" required/><input name="grade_level" placeholder="Grade" aria-label="Grade" required/><input name="subject_name" placeholder="Subject" aria-label="Subject" required/><input name="score" type="number" min="0" step="0.001" placeholder="Score" aria-label="Score"/><input name="maximum_score" type="number" min="0.001" step="0.001" placeholder="Out of" aria-label="Maximum score"/><input name="grade_label" placeholder="Grade label" aria-label="Grade label"/></div><button className="secondary" disabled={busy}>Add historical result <span>→</span></button></form>
          <div className="passport-list">{academicRecords.length?academicRecords.map(r=><article key={r.id}><div><strong>{r.subject_name}</strong><span>{r.school_name} · {r.grade_level} · {r.academic_year_label}</span></div><b>{r.score===null?'—':`${r.score}${r.maximum_score===null?'':` / ${r.maximum_score}`}`}</b><small>{r.grade_label||r.verification_status}</small></article>):<p className="empty">No historical results recorded yet.</p>}</div>
        </div><div className="passport-column"><h3>External examinations <span>{externalExams.length}</span></h3><form className="stack-form exam-form" onSubmit={(e)=>{const form=e.currentTarget;void submit(e,async()=>{const f=new FormData(form);const score=String(f.get('score')??'');const max=String(f.get('maximum_score')??'');await api(`/students/${passportStudent}/external-exams`,{method:'POST',body:JSON.stringify({exam_name:f.get('exam_name'),exam_provider:f.get('exam_provider'),exam_date:f.get('exam_date'),subject_or_section:f.get('subject_or_section'),score:score===''?null:Number(score),maximum_score:max===''?null:Number(max),score_label:f.get('score_label')})});form.reset();setExternalExams(await api<ExternalExam[]>(`/students/${passportStudent}/external-exams`))},'External exam result added.')}}>
          <div className="history-fields exam-fields"><input name="exam_name" placeholder="Exam name" aria-label="Exam name" required/><input name="exam_provider" placeholder="Provider" aria-label="Exam provider"/><input name="exam_date" type="date" aria-label="Exam date"/><input name="subject_or_section" placeholder="Subject or section" aria-label="Subject or section"/><input name="score" type="number" min="0" step="0.001" placeholder="Score" aria-label="Exam score"/><input name="maximum_score" type="number" min="0.001" step="0.001" placeholder="Out of" aria-label="Exam maximum score"/><input name="score_label" placeholder="Score label" aria-label="Score label"/></div><button className="secondary" disabled={busy}>Add exam result <span>→</span></button></form>
          <div className="passport-list">{externalExams.length?externalExams.map(x=><article key={x.id}><div><strong>{x.exam_name}{x.exam_provider?` · ${x.exam_provider}`:''}</strong><span>{x.subject_or_section||x.exam_date||'External examination'}</span>{x.breakdowns.map(b=><small key={b.section_name}>{b.section_name}: {b.score??'—'}{b.maximum_score===null?'':` / ${b.maximum_score}`}</small>)}</div><b>{x.score===null?(x.score_label||'—'):`${x.score}${x.maximum_score===null?'':` / ${x.maximum_score}`}`}</b></article>):<p className="empty">No external examinations recorded yet.</p>}</div>
        </div></div>
        <div className="csv-import"><div><h3>Import historical records</h3><p>Upload a CSV with the required column order. Preview all rows before committing.</p><code>student_number, school_name, academic_year_label, grade_level, subject_name, score, maximum_score, grade_label, source_type</code></div><form onSubmit={(e)=>void previewCSV(e)}><input type="file" name="file" accept=".csv,text/csv" aria-label="Historical records CSV" required/><button className="secondary" disabled={busy}>Preview CSV</button></form></div>
        {importPreview&&<div className="import-preview"><div><strong>Import preview</strong><span>{importPreview.valid_rows} valid · {importPreview.error_rows} rows with errors · {importPreview.status}</span></div>{importPreview.row_errors.map(row=><p className="row-error" key={row.row}>Row {row.row}: {row.errors.join(' ')}</p>)}{importPreview.can_commit&&importPreview.status==='previewed'&&<button className="primary compact" disabled={busy} onClick={()=>void commitCSV()}>Commit {importPreview.valid_rows} records</button>}</div>}
      </>}
      </section>
      <section className="panel curriculum-panel" id="curriculum"><div className="panel-head"><div><p className="eyebrow">CURRICULUM & QUESTION BANK</p><h2>Build curriculum-aware practice</h2><p>Add a curriculum version and skill hierarchy, then author reusable questions mapped to specific skills.</p></div><span className="year-icon">⌘</span></div>
        <div className="curriculum-grid"><div className="curriculum-column"><h3>Curriculum structure</h3>
          <form className="stack-form curriculum-form" onSubmit={(e)=>void submit(e,()=>createCurriculum(e),'Curriculum version created.')}><strong>New curriculum version</strong><div className="history-fields"><input name="curriculum_name" placeholder="Curriculum name" aria-label="Curriculum name" required/><input name="provider" placeholder="Provider / authority" aria-label="Provider" required/><input name="country_code" placeholder="Country code" aria-label="Country code"/><input name="version_label" placeholder="Version or syllabus year" aria-label="Version label" required/><input name="language_code" placeholder="Language code" aria-label="Language code" defaultValue="en"/><input name="curriculum_source" placeholder="Approved source URL or reference" aria-label="Curriculum source reference"/><input name="curriculum_license" placeholder="License / permission notes" aria-label="Curriculum license notes"/></div><button className="secondary" disabled={busy}>Add curriculum <span>→</span></button></form>
          {curricula.length>0&&<><label className="student-picker">Curriculum<select value={curriculumID} onChange={(e)=>setCurriculumID(e.target.value)}>{curricula.map(c=><option key={c.id} value={c.id}>{c.name} · {c.provider} · {c.version_label}</option>)}</select></label><form className="inline-form curriculum-form" onSubmit={(e)=>void submit(e,()=>createSubject(e),'Subject added.')}><input name="subject_name" placeholder="Subject name" aria-label="Subject name" required/><input name="grade_level" placeholder="Grade level" aria-label="Grade level"/><input name="subject_code" placeholder="Subject code" aria-label="Subject code"/><button className="secondary" disabled={busy}>Add subject</button></form></>}
          {subjects.length>0&&<><label className="student-picker">Subject<select value={subjectID} onChange={(e)=>setSubjectID(e.target.value)}>{subjects.map(s=><option key={s.id} value={s.id}>{s.name}{s.grade_level?` · ${s.grade_level}`:''}</option>)}</select></label><form className="stack-form curriculum-form" onSubmit={(e)=>void submit(e,()=>createNode(e),'Curriculum node added.')}><strong>Add topic, concept, skill, or objective</strong><div className="history-fields"><select name="node_type" aria-label="Node type"><option value="unit">Unit</option><option value="topic">Topic</option><option value="concept">Concept</option><option value="skill">Skill</option><option value="objective">Objective</option></select><select name="parent_id" aria-label="Parent node"><option value="">Top-level node</option>{curriculumNodes.map(n=><option key={n.id} value={n.id}>{n.node_type}: {n.title}</option>)}</select><input name="node_title" placeholder="Title" aria-label="Node title" required/><input name="node_code" placeholder="Curriculum code" aria-label="Node code"/><input name="sort_order" type="number" placeholder="Order" aria-label="Sort order" defaultValue="0"/><input name="node_description" placeholder="Description" aria-label="Node description"/></div><button className="secondary" disabled={busy}>Add curriculum node <span>→</span></button></form><div className="curriculum-node-list">{curriculumNodes.map(n=><article key={n.id} className={`node-${n.node_type}`}><span>{n.node_type}</span><strong>{n.title}</strong>{n.code&&<small>{n.code}</small>}</article>)}</div></>}
          {!curricula.length&&<p className="empty">Start with an approved curriculum definition. Add official content only when its source and usage rights are clear.</p>}
        </div><div className="curriculum-column"><h3>Question authoring <span>{questions.length}</span></h3>
          <form className="stack-form question-form" onSubmit={(e)=>void submit(e,()=>createQuestion(e),'Question saved as a draft.')}><select name="question_type" aria-label="Question type"><option value="single_choice">Single choice</option><option value="multiple_choice">Multiple choice</option><option value="numeric">Numeric answer</option><option value="text">Text response</option></select><textarea name="prompt" placeholder="Write the question prompt" aria-label="Question prompt" required rows={3}/><textarea name="options" placeholder={'Choice options, one per line: A|Option text'} aria-label="Choice options" rows={3}/><textarea name="answer_key" placeholder={'Server answer key JSON, e.g. ["A"] or {"value":12,"tolerance":0.5}'} aria-label="Answer key JSON" required rows={2}/><textarea name="explanation" placeholder="Explain the answer for permitted feedback" aria-label="Explanation" rows={2}/><div className="history-fields"><select name="skill_id" aria-label="Tag a curriculum skill"><option value="">Choose a skill tag</option>{curriculumNodes.filter(n=>n.node_type==='skill').map(n=><option key={n.id} value={n.id}>{n.title}</option>)}</select><select name="difficulty" aria-label="Difficulty"><option value="1">Difficulty 1 · introductory</option><option value="2">Difficulty 2</option><option value="3" selected>Difficulty 3 · standard</option><option value="4">Difficulty 4</option><option value="5">Difficulty 5 · advanced</option></select><input name="language_code" placeholder="Language" aria-label="Question language" defaultValue="en"/><input name="source_name" placeholder="Source name" aria-label="Question source" required/><input name="source_reference" placeholder="Source reference" aria-label="Source reference"/><input name="license_notes" placeholder="License / permission" aria-label="License notes" required/></div><p className="form-hint">For choice questions, use unique option IDs such as A and B; the answer key is a JSON array of correct IDs. One selected skill receives weight 1.0000.</p><button className="secondary" disabled={busy||!curriculumNodes.some(n=>n.node_type==='skill')}>Save question draft <span>→</span></button></form>
          <div className="question-list">{questions.map(q=><article key={q.id}><div className="question-card-head"><span className={`status-pill ${q.status}`}>{q.status}</span><small>{q.question_type.replace('_',' ')} · {q.difficulty===null?'Unrated':`Level ${q.difficulty}`}</small></div><strong>{q.prompt?.text||'Question prompt'}</strong>{q.options?.map(o=><span className="question-option" key={o.id}>{o.id}. {o.text}</span>)}<small>{q.skill_tags.map(t=>`${t.title} (${Number(t.weight).toFixed(2)})`).join(' · ')||'No skill tag yet'}</small>{q.source_name&&<small>Source: {q.source_name}{q.license_notes?` · ${q.license_notes}`:''}</small>}{q.status==='draft'&&<button className="secondary compact" disabled={busy||!q.skill_tags.length} onClick={()=>void submit(new Event('submit') as unknown as FormEvent<HTMLFormElement>,()=>publishQuestion(q.id),'Question published.')}>Publish approved question</button>}</article>)}{!questions.length&&<p className="empty">Your reusable question bank will appear here. Answer keys are never included in these question cards.</p>}</div>
        </div></div>
      </section>
      <section className="panel settings-panel"><div className="panel-head"><div><h2>School profile</h2><p>Keep your school details and regional settings up to date.</p></div><span className="settings-mark">✳</span></div>
        {school&&<form className="school-form" onSubmit={(e)=>{const form=e.currentTarget;void submit(e,async()=>{const f=new FormData(form);const updated=await api<School>('/school',{method:'PATCH',body:JSON.stringify({name:f.get('name'),timezone:f.get('timezone'),default_language:f.get('default_language')})});setSchool(updated)},'School profile saved.')}}><label>School name<input name="name" defaultValue={school.name} required/></label><label>Timezone<input name="timezone" defaultValue={school.timezone} required/></label><label>Default language<input name="default_language" defaultValue={school.default_language} required/></label><button className="secondary" disabled={busy}>Save profile</button></form>}
      </section><footer>FURII SCHOOL OS <span>·</span> ACADEMIC FOUNDATION</footer></div>
    </main>
  </div>
}

function Brand() { return <div className="brand"><div className="brand-mark">F</div><div><strong>FURII</strong><span>SCHOOL OS</span></div></div> }
function Metric({icon,label,value,detail}:{icon:string;label:string;value:string;detail:string}) { return <article className="metric"><div className="metric-top"><span>{label}</span><i>{icon}</i></div><strong>{value}</strong><small>{detail}</small></article> }

createRoot(document.getElementById('root')!).render(<App />)
