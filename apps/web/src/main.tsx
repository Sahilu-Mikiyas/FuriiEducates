import { FormEvent, MouseEvent, useCallback, useEffect, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { clearPendingAnswers, listPendingAnswers, removePendingAnswer, savePendingAnswer } from './offlineAnswers'
import './style.css'
import { MarketingLanding } from './marketing'

type Identity = { user_id: string; school_id: string; display_name: string; roles: string[] }
type School = { id: string; name: string; timezone: string; default_language: string }
type Year = { id: string; name: string; starts_on: string; ends_on: string }
type ClassRoom = { id: string; name: string; grade_level: string; academic_year_id: string; academic_year_name?: string }
type Student = { id: string; student_number: string; legal_name: string; preferred_language: string; status: string; has_account: boolean }
type AcademicRecord = { id: string; school_name: string; academic_year_label: string; grade_level: string; subject_name: string; score: number | null; maximum_score: number | null; grade_label: string; grading_scale_name: string; source_type: string; verification_status: string }
type ExternalExam = { id: string; exam_name: string; exam_provider: string; exam_date: string; subject_or_section: string; score: number | null; maximum_score: number | null; score_label: string; verification_status: string; breakdowns: { section_name: string; score: number | null; maximum_score: number | null }[] }
type ImportPreview = { id: string; status: string; row_count: number; valid_rows: number; error_rows: number; can_commit: boolean; row_errors: { row: number; errors: string[] }[] }
type Curriculum = { id: string; name: string; provider: string; country_code: string; version_label: string; language_code: string; source_reference: string; license_notes: string }
type Subject = { id: string; name: string; grade_level: string; subject_code: string }
type CurriculumNode = { id: string; parent_id: string; node_type: string; code: string; title: string; description: string; sort_order: number }
type Question = { id: string; question_type: string; prompt: { text?: string }; options: { id: string; text: string }[] | null; explanation: { text?: string } | null; difficulty: number | null; language_code: string; source_name: string; source_reference: string; license_notes: string; status: string; skill_tags: { skill_id: string; title: string; weight: number }[] }
type StaffMember = { id: string; email: string; display_name: string; status: string; class_ids: string[] }
type StaffInvitation = { id: string; email: string; role: string; created_at: string; expires_at: string }
type TeacherStudent = { id: string; student_number: string; legal_name: string; preferred_language: string; status: string }
type Assessment = { id: string; title: string; instructions: string; status: string; created_at: string; duration_minutes:number; question_count: number; question_ids: string[]; assigned_class_ids: string[] }
type LearnerAssignment = { assignment_id: string; title: string; instructions: string; class_name: string; grade_level: string; assigned_at: string; due_at: string; attempt_status: string; attempt_id: string }
type LearnerQuestion = { id: string; question_type: string; prompt: { text?: string }; options?: { id: string; text: string }[]; points: string; answer?: unknown;revision:number;flagged:boolean }
type LearnerAssessment = { assignment_id: string; title: string; instructions: string; status: string; attempt_id: string; deadline_at:string|null; score: number | null; max_score: number | null; results_released: boolean; final_score: number | null; final_max_score: number | null; manual_review_count: number; questions: (LearnerQuestion & { awarded_points?: string; feedback?: string })[] }
type TeacherReview = { attempt_id: string; question_id: string; assignment_id: string; assessment_title: string; student_id: string; student_number: string; student_name: string; prompt: { text?: string }; grading_guide?: unknown; answer: unknown; max_points: number; feedback: string; reviewed_at?: string }
type StudentLearningProfile = { algorithm_version: string; skills: {skill_id:string;title:string;estimated_mastery:number;confidence:number;evidence_count:number;confidence_band:string}[]; recommendations: {id:string;skill_id:string;skill_title:string;reason_code:string;evidence_summary:{estimated_mastery?:number;evidence_count?:number;confidence?:number};recommended_action:{title?:string;description?:string;target_mastery?:number};priority:string;status:string;pre_mastery:number|null;post_mastery:number|null;change_points:number|null}[] }
type TeacherLearningRow = { student_id:string;student_number:string;student_name:string;skill_id:string;skill_title:string;estimated_mastery:number;confidence:number;evidence_count:number;confidence_band:string;active_recommendations:number }
type LearningSummary = { assigned_assessments:number; submitted_attempts:number; average_score_percent:number; active_recommendations:number }

const API_BASE_URL = (import.meta.env.VITE_API_BASE_URL ?? '').trim().replace(/\/+$/, '')
const apiURL = (path: string) => `${API_BASE_URL}/api/v1${path}`

const adminPages = [
  { id: 'overview', label: 'Overview', icon: '▦', title: 'School overview', description: 'Your school’s academic foundation at a glance.' },
  { id: 'students', label: 'Students', icon: '♧', title: 'Students', description: 'Enroll learners and manage their school records.' },
  { id: 'classes', label: 'Classes', icon: '▤', title: 'Classes & calendar', description: 'Organize classes and academic years.' },
  { id: 'academic-passport', label: 'Academic history', icon: '▧', title: 'Academic history', description: 'Keep prior school results and external exam evidence together.' },
  { id: 'curriculum', label: 'Curriculum & questions', icon: '⌘', title: 'Curriculum & question bank', description: 'Build a curriculum map and a reusable, skill-aware question bank.' },
  { id: 'assessments', label: 'Assessments', icon: '✎', title: 'Assessments', description: 'Create, publish, and assign classroom assessments.' },
  { id: 'staff', label: 'Staff access', icon: '♙', title: 'Staff access', description: 'Invite teachers, assign classes, and manage access.' },
  { id: 'settings', label: 'School settings', icon: '⚙', title: 'School settings', description: 'Manage your school profile and regional settings.' },
] as const
type AdminPage = typeof adminPages[number]['id']
const adminPageFromPath = (path: string): AdminPage => {
  const slug = path.replace(/^\/admin\/?/, '').split('/')[0]
  return adminPages.some((page) => page.id === slug) ? slug as AdminPage : 'overview'
}

async function readAPIResponse(response: Response) {
  const contentType = response.headers.get('content-type')?.toLowerCase() ?? ''
  if (!contentType.includes('json')) {
    const responseURL = new URL(response.url)
    if (!API_BASE_URL && responseURL.origin === window.location.origin) {
      throw new Error('VITE_API_BASE_URL is missing or was not included in this frontend build. Set it in Vercel to https://furii-school-api.onrender.com, then redeploy the frontend.')
    }
    if (response.status === 404) {
      throw new Error(`The configured API returned HTTP 404 for ${responseURL.pathname}. Set VITE_API_BASE_URL to the Render API origin only (https://furii-school-api.onrender.com), then redeploy. If it is already set, the API deployment may not include this route.`)
    }
    throw new Error(`The API returned a non-JSON response (HTTP ${response.status}). Check that VITE_API_BASE_URL points to the Render API and review the Render logs.`)
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
  const invitationToken = new URLSearchParams(window.location.hash.replace(/^#/, '')).get('invite') ?? ''
  const studentInvitationToken = new URLSearchParams(window.location.hash.replace(/^#/, '')).get('student-invite') ?? ''
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
  const [assessments, setAssessments] = useState<Assessment[]>([])
  const [learningSummary, setLearningSummary] = useState<LearningSummary | null>(null)
  const [staff, setStaff] = useState<StaffMember[]>([])
  const [staffInvitations, setStaffInvitations] = useState<StaffInvitation[]>([])
  const [inviteLink, setInviteLink] = useState('')
  const [teacherClasses, setTeacherClasses] = useState<ClassRoom[]>([])
  const [learnerAssignments, setLearnerAssignments] = useState<LearnerAssignment[]>([])
  const [studentInviteLinks, setStudentInviteLinks] = useState<Record<string,string>>({})
  const [curriculumID, setCurriculumID] = useState('')
  const [subjectID, setSubjectID] = useState('')
  const [loading, setLoading] = useState(() => window.location.pathname !== '/')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [adminPage, setAdminPage] = useState<AdminPage>(() => adminPageFromPath(window.location.pathname))

  const navigateAdmin = (event: MouseEvent<HTMLAnchorElement>, page: AdminPage) => {
    event.preventDefault()
    const path = `/admin/${page}`
    if (window.location.pathname !== path) window.history.pushState({}, '', path)
    setAdminPage(page)
    window.scrollTo({ top: 0, behavior: 'auto' })
  }

  useEffect(() => {
    const onPopState = () => setAdminPage(adminPageFromPath(window.location.pathname))
    window.addEventListener('popstate', onPopState)
    return () => window.removeEventListener('popstate', onPopState)
  }, [])

  useEffect(() => {
    if (!identity?.roles.includes('school_admin')) return
    if (window.location.pathname === '/' || window.location.pathname === '/login') window.history.replaceState({}, '', '/admin/overview')
  }, [identity])

  const refresh = useCallback(async () => {
    const [schoolData, yearData, classData, studentData] = await Promise.all([
      api<School>('/school'), api<Year[]>('/academic-years'), api<ClassRoom[]>('/classes'), api<Student[]>('/students'),
    ])
    setSchool(schoolData); setYears(yearData); setClasses(classData); setStudents(studentData)
    setPassportStudent((current) => current || studentData[0]?.id || '')
  }, [])

  useEffect(() => {
    if (invitationToken || studentInvitationToken || window.location.pathname === '/') { setLoading(false); return }
    api<Identity>('/me').then(async (user) => { setIdentity(user); if (user.roles.includes('school_admin')) await refresh() })
      .catch(() => setIdentity(null)).finally(() => setLoading(false))
  }, [refresh, invitationToken, studentInvitationToken])

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
    if (!identity?.roles.includes('school_admin')) return
    Promise.all([api<Curriculum[]>('/curricula'), api<Question[]>('/questions')]).then(([cs, qs]) => {
      setCurricula(cs); setQuestions(qs); setCurriculumID((current) => current || cs[0]?.id || '')
    }).catch((e) => setError(e instanceof Error ? e.message : 'Unable to load curriculum workspace.'))
  }, [identity])

  useEffect(() => {
    if (!identity?.roles.includes('school_admin')) return
    Promise.all([api<StaffMember[]>('/staff'), api<StaffInvitation[]>('/staff/invitations')])
      .then(([members, invites]) => { setStaff(members); setStaffInvitations(invites) })
      .catch((e) => setError(e instanceof Error ? e.message : 'Unable to load staff access.'))
  }, [identity])

  useEffect(() => {
    if (!identity?.roles.includes('school_admin')) return
    api<Assessment[]>('/assessments').then(setAssessments)
      .catch((e) => setError(e instanceof Error ? e.message : 'Unable to load assessments.'))
  }, [identity])

  useEffect(() => {
    if (!identity?.roles.includes('school_admin')) return
    api<LearningSummary>('/school/learning-summary').then(setLearningSummary)
      .catch((e) => setError(e instanceof Error ? e.message : 'Unable to load learning summary.'))
  }, [identity])

  useEffect(() => {
    if (!identity?.roles.includes('teacher')) return
    api<ClassRoom[]>('/teacher/classes').then((items) => setTeacherClasses(items))
      .catch((e) => setError(e instanceof Error ? e.message : 'Unable to load your classes.'))
  }, [identity])

  useEffect(() => {
    if (!identity?.roles.includes('student')) return
    api<LearnerAssignment[]>('/student/assessments').then(setLearnerAssignments)
      .catch((e) => setError(e instanceof Error ? e.message : 'Unable to load your assessments.'))
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
    if (!user.roles.includes('school_admin') && !user.roles.includes('teacher') && !user.roles.includes('student')) {
      await api('/auth/logout', { method: 'POST' })
      throw new Error('This workspace is not enabled for your account role yet.')
    }
    setIdentity(user)
    if (user.roles.includes('school_admin')) await refresh()
  }

  async function logout() {
    await api('/auth/logout', { method: 'POST' }); setIdentity(null); setSchool(null); setYears([]); setClasses([]); setStudents([]); setTeacherClasses([])
  }

  async function createStaffInvitation(event: FormEvent<HTMLFormElement>) {
    const form = event.currentTarget
    const values = new FormData(form)
    const result = await api<{ id: string; token: string }>('/staff/invitations', { method: 'POST', body: JSON.stringify({ email: values.get('email') }) })
    setInviteLink(`${window.location.origin}/#invite=${encodeURIComponent(result.token)}`)
    setStaffInvitations(await api<StaffInvitation[]>('/staff/invitations'))
    form.reset()
  }

  async function assignTeacher(event: FormEvent<HTMLFormElement>, teacherID: string) {
    const form = event.currentTarget
    const classID = String(new FormData(form).get('class_id') || '')
    if (!classID) throw new Error('Choose a class to assign.')
    await api(`/classes/${classID}/teachers`, { method: 'POST', body: JSON.stringify({ teacher_id: teacherID }) })
    setStaff(await api<StaffMember[]>('/staff'))
    form.reset()
  }

  async function updateStaffStatus(teacher: StaffMember) {
    await api(`/staff/${teacher.id}`, { method: 'PATCH', body: JSON.stringify({ status: teacher.status === 'active' ? 'disabled' : 'active' }) })
    setStaff(await api<StaffMember[]>('/staff'))
  }

  async function revokeStaffInvitation(invitationID: string) {
    await api(`/staff/invitations/${invitationID}/revoke`, { method: 'POST', body: '{}' })
    setStaffInvitations(await api<StaffInvitation[]>('/staff/invitations'))
  }

  async function createAssessment(event: FormEvent<HTMLFormElement>) {
    const form=event.currentTarget,values=new FormData(form)
    const questionIDs=values.getAll('question_ids').map(String)
    await api('/assessments',{method:'POST',body:JSON.stringify({title:values.get('title'),instructions:values.get('instructions'),duration_minutes:Number(values.get('duration_minutes')),question_ids:questionIDs})})
    setAssessments(await api<Assessment[]>('/assessments'));form.reset()
  }

  async function publishAssessment(assessmentID:string) {
    await api(`/assessments/${assessmentID}/publish`,{method:'POST',body:'{}'})
    setAssessments(await api<Assessment[]>('/assessments'))
  }

  async function assignAssessment(event:FormEvent<HTMLFormElement>,assessment:Assessment) {
    const form=event.currentTarget,classID=String(new FormData(form).get('class_id')||'')
    if(!classID)throw new Error('Choose a class to assign.')
    await api(`/assessments/${assessment.id}/assignments`,{method:'POST',body:JSON.stringify({class_id:classID})})
    setAssessments(await api<Assessment[]>('/assessments'));form.reset()
  }

  async function createStudentInvitation(event:FormEvent<HTMLFormElement>,student:Student) {
    const form=event.currentTarget,email=String(new FormData(form).get('email')||'')
    const result=await api<{token:string}>(`/students/${student.id}/invitation`,{method:'POST',body:JSON.stringify({email})})
    setStudentInviteLinks((current)=>({...current,[student.id]:`${window.location.origin}/#student-invite=${encodeURIComponent(result.token)}`}))
    form.reset()
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

  if (invitationToken) return <InviteAcceptPage token={invitationToken} />
  if (studentInvitationToken) return <StudentInviteAcceptPage token={studentInvitationToken} />
  if (loading) return <main className="loading">Opening FURII School OS…</main>
  if (identity?.roles.includes('student')) return <LearnerWorkspace identity={identity} initialAssignments={learnerAssignments} onLogout={() => void logout()} />
  if (identity?.roles.includes('teacher')) return <TeacherWorkspace identity={identity} classes={teacherClasses} onLogout={() => void logout()} />
  if (!identity && window.location.pathname === '/') return <MarketingLanding />
  if (!identity) return <main className="login-shell">
    <div className="login-art"><Brand /><div className="login-copy"><p className="eyebrow">A SCHOOL THAT SEES EVERY LEARNER</p><h1>Build a stronger<br />academic foundation.</h1><p>Bring your school records, teaching, and assessment into one connected place.</p><div className="art-orbit orbit-one"/><div className="art-orbit orbit-two"/><div className="art-spark">✳</div></div><span className="login-footnote">FURII SCHOOL OS <span>·</span> MADE FOR LEARNING</span></div>
    <div className="login-pane"><form className="login-form" onSubmit={(e) => void submit(e, () => login(e), 'Welcome back.')}>
      <p className="eyebrow">SCHOOL STAFF ACCESS</p><h2>Welcome back</h2><p className="muted">Sign in to open your school workspace.</p>
      <label>Email address<input name="email" type="email" autoComplete="username" placeholder="you@school.edu" required /></label>
      <label>Password<input name="password" type="password" autoComplete="current-password" placeholder="Enter your password" required /></label>
      {error && <p className="form-error" role="alert">{error}</p>}<button className="primary" disabled={busy}>{busy ? 'Signing in…' : 'Sign in'} <span>→</span></button>
      <p className="login-help">Need access? Ask your school administrator to provision your account.</p>
    </form></div>
  </main>

  return <div className="app-shell">
    <aside className="sidebar"><Brand /><div className="school-switch"><div className="school-avatar">{school?.name.slice(0,1) ?? 'F'}</div><div><strong>{school?.name ?? 'Your school'}</strong><span>School workspace</span></div><b>⌄</b></div>
      <p className="nav-heading">WORKSPACE</p><nav className="admin-nav" aria-label="School workspace pages">{adminPages.map((page) => <a key={page.id} className={`nav-item ${adminPage === page.id ? 'active' : ''}`} href={`/admin/${page.id}`} aria-current={adminPage === page.id ? 'page' : undefined} onClick={(event) => navigateAdmin(event, page.id)}><span>{page.icon}</span>{page.label}{page.id === 'students' && <small>{students.length}</small>}</a>)}</nav>
      <div className="sidebar-bottom"><div className="user-chip"><div className="user-avatar">{identity.display_name.slice(0,1).toUpperCase()}</div><div><strong>{identity.display_name}</strong><span>Administrator</span></div><button aria-label="Sign out" title="Sign out" onClick={() => void logout()}>↗</button></div></div>
    </aside>
    <main className="main-area"><header className="topbar"><span>Workspace <b>/</b> {adminPages.find((page) => page.id === adminPage)?.label}</span><div className="top-actions"><span className="connection"><i/> All changes saved</span><button className="icon-button" title="Help">?</button><div className="user-avatar small">{identity.display_name.slice(0,1).toUpperCase()}</div></div></header>
      <div className="content"><div className="page-title"><div><p className="eyebrow">{adminPage === 'overview' ? 'SCHOOL OVERVIEW' : 'WORKSPACE'}</p><h1>{adminPage === 'overview' ? <>Good day, {identity.display_name.split(' ')[0]} <span className="wave">✳</span></> : adminPages.find((page) => page.id === adminPage)?.title}</h1><p className="muted">{adminPage === 'overview' ? 'Your school’s academic foundation at a glance.' : adminPages.find((page) => page.id === adminPage)?.description}</p></div>{adminPage === 'overview' && <div className="date-pill">{new Intl.DateTimeFormat('en', { weekday: 'short', month: 'short', day: 'numeric' }).format(new Date())}</div>}</div>
      {(error || notice) && <div className={error ? 'toast error-toast' : 'toast'} role="status">{error || notice}<button onClick={() => {setError('');setNotice('')}} aria-label="Dismiss">×</button></div>}
      {adminPage === 'overview' && <section className="metrics"><Metric icon="♧" label="Enrolled students" value={students.length.toString()} detail="Across your school"/><Metric icon="▤" label="Active classes" value={classes.length.toString()} detail="In all academic years"/><Metric icon="◷" label="Academic years" value={years.length.toString()} detail="School calendar"/><Metric icon="✳" label="Learning insights" value={learningSummary?`${learningSummary.average_score_percent}%`:'—'} detail={learningSummary?`${learningSummary.submitted_attempts} submitted · ${learningSummary.active_recommendations} active recommendations`:'Loading assessment summary'}/></section>}
      {adminPage === 'overview' && <nav className="overview-links" aria-label="Open a workspace page">{adminPages.filter((page) => page.id !== 'overview').map((page) => <a key={page.id} href={`/admin/${page.id}`} onClick={(event) => navigateAdmin(event, page.id)}><span>{page.icon}</span><strong>{page.label}</strong><small>{page.description}</small><b aria-hidden="true">→</b></a>)}</nav>}
      <section className={`work-grid ${adminPage === 'students' || adminPage === 'classes' ? 'single-column' : ''}`} hidden={adminPage !== 'students' && adminPage !== 'classes'}><div className="panel" id="students" hidden={adminPage !== 'students'}><div className="panel-head"><div><h2>Students</h2><p>Manage your school’s student records.</p></div><span className="count-pill">{students.length} total</span></div>
        <form className="inline-form student-form" onSubmit={(e) => { const form=e.currentTarget; void submit(e, async () => { const f=new FormData(form); await api('/students',{method:'POST',body:JSON.stringify({student_number:f.get('student_number'),legal_name:f.get('legal_name'),preferred_language:f.get('preferred_language'),class_id:f.get('class_id')})}); form.reset(); await refresh() }, 'Student enrolled.') }}>
          <input name="student_number" placeholder="Student ID" aria-label="Student ID" required/><input name="legal_name" placeholder="Student full name" aria-label="Student full name" required/><select name="class_id" aria-label="Assign to class"><option value="">No class yet</option>{classes.map(c=><option key={c.id} value={c.id}>{c.grade_level} · {c.name}</option>)}</select><button className="primary compact" disabled={busy}>Add student</button>
        </form>
        <div className="table-wrap"><table><thead><tr><th>STUDENT</th><th>STUDENT ID</th><th>STATUS</th><th>LEARNER LOGIN</th></tr></thead><tbody>{students.length ? students.map(s=><tr key={s.id}><td><div className="student-cell"><div className="student-avatar">{s.legal_name.slice(0,1).toUpperCase()}</div><strong>{s.legal_name}</strong></div></td><td>{s.student_number}</td><td><span className="status-pill">{s.status}</span></td><td>{s.has_account?<span className="status-pill">Account active</span>:<div className="student-invite-cell"><form onSubmit={(e)=>void submit(e,()=>createStudentInvitation(e,s),'Student activation link created. Share it privately with the learner.')}><input name="email" type="email" placeholder="Learner email" aria-label={`Email for ${s.legal_name}`} required/><button className="secondary compact" disabled={busy||s.status!=='active'}>Create invite</button></form>{studentInviteLinks[s.id]&&<div className="student-invite-link"><code>{studentInviteLinks[s.id]}</code><button onClick={()=>void navigator.clipboard.writeText(studentInviteLinks[s.id]).then(()=>setNotice('Student invitation link copied.')).catch(()=>setError('Could not access clipboard. Copy the link manually.'))}>Copy</button></div>}</div>}</td></tr>) : <tr><td colSpan={4} className="empty">No students yet. Add your first student above.</td></tr>}</tbody></table></div>
      </div>
      <div className="right-stack" hidden={adminPage !== 'classes'}><section className="panel" id="classes"><div className="panel-head"><div><h2>Classes</h2><p>Set up a learning group.</p></div></div>
        <form className="stack-form" onSubmit={(e) => { const form=e.currentTarget; void submit(e, async () => { const f=new FormData(form); await api('/classes',{method:'POST',body:JSON.stringify({name:f.get('name'),grade_level:f.get('grade_level'),academic_year_id:f.get('academic_year_id')})}); form.reset(); await refresh() }, 'Class created.') }}><label>Class name<input name="name" placeholder="e.g. Section A" required/></label><div className="two-fields"><label>Grade<input name="grade_level" placeholder="Grade 9" required/></label><label>Academic year<select name="academic_year_id" required><option value="">Choose year</option>{years.map(y=><option key={y.id} value={y.id}>{y.name}</option>)}</select></label></div><button className="secondary" disabled={busy || !years.length}>{years.length ? 'Create class' : 'Add an academic year first'} <span>→</span></button></form>
        {classes.length>0&&<div className="mini-list">{classes.slice(0,4).map(c=><div key={c.id}><span className="class-icon">▤</span><strong>{c.grade_level} · {c.name}</strong><small>{c.academic_year_name ?? years.find(y=>y.id===c.academic_year_id)?.name}</small></div>)}</div>}
      </section>
      <section className="panel" id="school"><div className="panel-head"><div><h2>Academic year</h2><p>Define your school calendar.</p></div><span className="year-icon">◷</span></div>
        <form className="stack-form year-form" onSubmit={(e) => { const form=e.currentTarget; void submit(e, async () => { const f=new FormData(form); await api('/academic-years',{method:'POST',body:JSON.stringify({name:f.get('name'),starts_on:f.get('starts_on'),ends_on:f.get('ends_on')})}); form.reset(); await refresh() }, 'Academic year added.') }}><label>Year label<input name="name" placeholder="2026–2027" required/></label><div className="two-fields"><label>Starts<input name="starts_on" type="date" required/></label><label>Ends<input name="ends_on" type="date" required/></label></div><button className="secondary" disabled={busy}>Add academic year <span>→</span></button></form>
      </section></div></section>
      <section className="panel passport-panel" id="academic-passport" hidden={adminPage !== 'academic-passport'}><div className="panel-head"><div><p className="eyebrow">ACADEMIC PASSPORT</p><h2>Academic history</h2><p>Keep prior school results and external exam evidence together.</p></div><label className="student-picker">Student<select value={passportStudent} onChange={(e)=>setPassportStudent(e.target.value)}><option value="">Select student</option>{students.map(s=><option key={s.id} value={s.id}>{s.legal_name} · {s.student_number}</option>)}</select></label></div>
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
      <section className="panel curriculum-panel" id="curriculum" hidden={adminPage !== 'curriculum'}><div className="panel-head"><div><p className="eyebrow">CURRICULUM & QUESTION BANK</p><h2>Build curriculum-aware practice</h2><p>Add a curriculum version and skill hierarchy, then author reusable questions mapped to specific skills.</p></div><span className="year-icon">⌘</span></div>
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
      <section className="panel assessment-panel" id="assessments" hidden={adminPage !== 'assessments'}><div className="panel-head"><div><p className="eyebrow">ASSESSMENT PLANNING</p><h2>Create and assign an assessment</h2><p>Build from published questions in your question bank, then assign the published assessment to a class.</p></div><span className="year-icon">✎</span></div>
        <form className="stack-form assessment-form" onSubmit={(e)=>void submit(e,()=>createAssessment(e),'Assessment saved as a draft.')}><div className="assessment-fields"><label>Assessment title<input name="title" placeholder="e.g. Unit 1 Mathematics Check" required maxLength={200}/></label><label>Duration (minutes)<input name="duration_minutes" type="number" min="5" max="240" step="1" defaultValue="45" required/></label><label>Instructions<textarea name="instructions" rows={3} maxLength={5000} placeholder="Instructions for learners (optional)"/></label></div><div className="assessment-question-picker"><strong>Choose published questions</strong><div className="assessment-question-list">{questions.filter(q=>q.status==='published').map(q=><label key={q.id}><input type="checkbox" name="question_ids" value={q.id}/><span><b>{q.prompt?.text||'Question'}</b><small>{q.question_type.replace('_',' ')}{q.skill_tags.length?` · ${q.skill_tags.map(t=>t.title).join(', ')}`:''}</small></span></label>)}{!questions.some(q=>q.status==='published')&&<p className="empty">Publish approved questions in the Question Bank first.</p>}</div></div><button className="primary compact" disabled={busy||!questions.some(q=>q.status==='published')}>Save assessment draft</button></form>
        <div className="assessment-list">{assessments.map(assessment=><article key={assessment.id}><div className="assessment-summary"><strong>{assessment.title}</strong><small>{assessment.question_count} questions · <span className={`status-pill ${assessment.status}`}>{assessment.status}</span></small>{assessment.instructions&&<p>{assessment.instructions}</p>}<div className="assigned-classes">{assessment.assigned_class_ids.map(classID=>{const c=classes.find(x=>x.id===classID);return c?<span key={classID}>{c.grade_level} · {c.name}</span>:null})}{!assessment.assigned_class_ids.length&&<small>Not assigned</small>}</div></div><div className="assessment-actions">{assessment.status==='draft'&&<button className="secondary compact" disabled={busy||assessment.question_count===0} onClick={()=>void submit(new Event('submit') as unknown as FormEvent<HTMLFormElement>,()=>publishAssessment(assessment.id),'Assessment published.')}>Publish</button>}{assessment.status==='published'&&<form className="staff-assign" onSubmit={(e)=>void submit(e,()=>assignAssessment(e,assessment),'Assessment assigned to class.')}><select name="class_id" defaultValue=""><option value="">Assign a class…</option>{classes.filter(c=>!assessment.assigned_class_ids.includes(c.id)).map(c=><option key={c.id} value={c.id}>{c.grade_level} · {c.name}</option>)}</select><button className="secondary compact" disabled={busy||!classes.some(c=>!assessment.assigned_class_ids.includes(c.id))}>Assign</button></form>}</div></article>)}{!assessments.length&&<p className="empty">Assessment drafts will appear here.</p>}</div>
      </section>
      <section className="panel staff-panel" id="staff" hidden={adminPage !== 'staff'}><div className="panel-head"><div><p className="eyebrow">ACCESS & PERMISSIONS</p><h2>Staff accounts</h2><p>Invite teachers, assign their classes, and disable access when needed.</p></div><span className="settings-mark">♙</span></div>
        <form className="inline-form staff-invite-form" onSubmit={(e)=>void submit(e,()=>createStaffInvitation(e),'Teacher invitation created. Share the link securely with the teacher.')}><input name="email" type="email" placeholder="teacher@school.edu" aria-label="Teacher email address" required/><button className="primary compact" disabled={busy}>Create teacher invite</button></form>
        {inviteLink&&<div className="invite-result"><div><strong>One-time invitation link</strong><small>Valid for 7 days. Anyone with this link can claim the teacher account.</small><code>{inviteLink}</code></div><button className="secondary compact" onClick={()=>void navigator.clipboard.writeText(inviteLink).then(()=>setNotice('Invitation link copied.')).catch(()=>setError('Could not access clipboard. Copy the link manually.'))}>Copy link</button></div>}
        <div className="staff-grid"><div><h3>Teachers <span>{staff.length}</span></h3><div className="staff-list">{staff.map(member=><article key={member.id}><div className="staff-person"><strong>{member.display_name}</strong><small>{member.email}</small><span className={`status-pill ${member.status==='active'?'':'disabled-status'}`}>{member.status}</span></div><form className="staff-assign" onSubmit={(e)=>void submit(e,()=>assignTeacher(e,member.id),'Class assignment saved.')}><select name="class_id" aria-label={`Assign a class to ${member.display_name}`} defaultValue=""><option value="">Assign class…</option>{classes.filter(c=>!member.class_ids.includes(c.id)).map(c=><option key={c.id} value={c.id}>{c.grade_level} · {c.name}</option>)}</select><button className="secondary compact" disabled={busy||!classes.some(c=>!member.class_ids.includes(c.id))}>Assign</button></form><div className="assigned-classes">{member.class_ids.map(id=>{const c=classes.find(x=>x.id===id);return c?<span key={id}>{c.grade_level} · {c.name}</span>:null})}{!member.class_ids.length&&<small>No classes assigned</small>}</div><button className="staff-toggle" disabled={busy} onClick={()=>void submit(new Event('submit') as unknown as FormEvent<HTMLFormElement>,()=>updateStaffStatus(member),member.status==='active'?'Teacher access disabled.':'Teacher access enabled.')}>{member.status==='active'?'Disable access':'Enable access'}</button></article>)}{!staff.length&&<p className="empty">No teacher accounts yet. Create an invitation above.</p>}</div></div>
          <div><h3>Pending invitations <span>{staffInvitations.length}</span></h3><div className="staff-list">{staffInvitations.map(invite=><article key={invite.id} className="pending-invite"><div className="staff-person"><strong>{invite.email}</strong><small>Teacher · expires {new Date(invite.expires_at).toLocaleDateString()}</small></div><button className="staff-toggle" disabled={busy} onClick={()=>void submit(new Event('submit') as unknown as FormEvent<HTMLFormElement>,()=>revokeStaffInvitation(invite.id),'Invitation revoked.')}>Revoke</button></article>)}{!staffInvitations.length&&<p className="empty">No pending invitations.</p>}</div></div>
        </div>
      </section>
      <section className="panel settings-panel" hidden={adminPage !== 'settings'}><div className="panel-head"><div><h2>School profile</h2><p>Keep your school details and regional settings up to date.</p></div><span className="settings-mark">✳</span></div>
        {school&&<form className="school-form" onSubmit={(e)=>{const form=e.currentTarget;void submit(e,async()=>{const f=new FormData(form);const updated=await api<School>('/school',{method:'PATCH',body:JSON.stringify({name:f.get('name'),timezone:f.get('timezone'),default_language:f.get('default_language')})});setSchool(updated)},'School profile saved.')}}><label>School name<input name="name" defaultValue={school.name} required/></label><label>Timezone<input name="timezone" defaultValue={school.timezone} required/></label><label>Default language<input name="default_language" defaultValue={school.default_language} required/></label><button className="secondary" disabled={busy}>Save profile</button></form>}
      </section><footer>FURII SCHOOL OS <span>·</span> ACADEMIC FOUNDATION</footer></div>
    </main>
  </div>
}

function InviteAcceptPage({token}:{token:string}) {
  const [busy,setBusy]=useState(false),[error,setError]=useState(''),[success,setSuccess]=useState('')
  async function accept(event:FormEvent<HTMLFormElement>) {
    event.preventDefault();setBusy(true);setError('');setSuccess('')
    const form=new FormData(event.currentTarget),password=String(form.get('password')||''),confirm=String(form.get('confirm_password')||'')
    if(password!==confirm){setError('The passwords do not match.');setBusy(false);return}
    try {const result=await api<{email:string}>('/staff/invitations/accept',{method:'POST',body:JSON.stringify({token,display_name:form.get('display_name'),password})});setSuccess(`Your teacher account for ${result.email} is ready. Sign in with the password you just created.`)}
    catch(e){setError(e instanceof Error?e.message:'Unable to accept this invitation.')}finally{setBusy(false)}
  }
  return <main className="invite-shell"><div className="invite-card"><Brand/><p className="eyebrow">TEACHER ACCOUNT</p><h1>Set up your account</h1><p className="muted">Choose your name and a strong password to accept this school invitation.</p>{success?<><p className="invite-success">{success}</p><a className="primary invite-login-link" href="/">Continue to sign in <span>→</span></a></>:<form className="stack-form invite-form" onSubmit={(e)=>void accept(e)}><label>Your name<input name="display_name" autoComplete="name" required maxLength={200}/></label><label>Create password<input name="password" type="password" autoComplete="new-password" minLength={12} required/><small>Use at least 12 characters.</small></label><label>Confirm password<input name="confirm_password" type="password" autoComplete="new-password" minLength={12} required/></label>{error&&<p className="form-error" role="alert">{error}</p>}<button className="primary" disabled={busy}>{busy?'Creating account…':'Accept invitation'} <span>→</span></button></form>}</div></main>
}

function StudentInviteAcceptPage({token}:{token:string}) {
  const [busy,setBusy]=useState(false),[error,setError]=useState(''),[success,setSuccess]=useState('')
  async function accept(event:FormEvent<HTMLFormElement>) {
    event.preventDefault();setBusy(true);setError('');setSuccess('')
    const form=new FormData(event.currentTarget),password=String(form.get('password')||''),confirm=String(form.get('confirm_password')||'')
    if(password!==confirm){setError('The passwords do not match.');setBusy(false);return}
    try {const result=await api<{email:string}>('/student-invitations/accept',{method:'POST',body:JSON.stringify({token,display_name:form.get('display_name'),password})});setSuccess(`Your learner account for ${result.email} is ready. Use the password you created to sign in.`)}
    catch(e){setError(e instanceof Error?e.message:'Unable to accept this invitation.')}finally{setBusy(false)}
  }
  return <main className="invite-shell"><div className="invite-card"><Brand/><p className="eyebrow">STUDENT ACCOUNT</p><h1>Set up your learner account</h1><p className="muted">Choose your name and a strong password to access your assigned assessments.</p>{success?<><p className="invite-success">{success}</p><a className="primary invite-login-link" href="/">Continue to sign in <span>→</span></a></>:<form className="stack-form invite-form" onSubmit={(e)=>void accept(e)}><label>Your name<input name="display_name" autoComplete="name" required maxLength={200}/></label><label>Create password<input name="password" type="password" autoComplete="new-password" minLength={12} required/><small>Use at least 12 characters.</small></label><label>Confirm password<input name="confirm_password" type="password" autoComplete="new-password" minLength={12} required/></label>{error&&<p className="form-error" role="alert">{error}</p>}<button className="primary" disabled={busy}>{busy?'Creating account…':'Accept invitation'} <span>→</span></button></form>}</div></main>
}

function LearnerWorkspace({identity,initialAssignments,onLogout}:{identity:Identity;initialAssignments:LearnerAssignment[];onLogout:()=>void}) {
  const [assignments,setAssignments]=useState(initialAssignments),[selected,setSelected]=useState(''),[assessment,setAssessment]=useState<LearnerAssessment|null>(null),[answers,setAnswers]=useState<Record<string,string|string[]>>({}),[busy,setBusy]=useState(false),[error,setError]=useState('')
  const [profile,setProfile]=useState<StudentLearningProfile|null>(null)
  const [revisions,setRevisions]=useState<Record<string,number>>({}),[flags,setFlags]=useState<Record<string,boolean>>({}),[syncState,setSyncState]=useState('saved'),[online,setOnline]=useState(navigator.onLine),[secondsLeft,setSecondsLeft]=useState(0)
  const answerTimers=useRef<Record<string,ReturnType<typeof setTimeout>>>({})
  const flushing=useRef(false),deadlineSubmitted=useRef(''),revisionRef=useRef<Record<string,number>>({})
  useEffect(()=>setAssignments(initialAssignments),[initialAssignments])
  useEffect(()=>{api<StudentLearningProfile>('/student/learning-profile').then(setProfile).catch(e=>setError(e instanceof Error?e.message:'Unable to load your learning profile.'))},[])
  useEffect(()=>{const update=()=>setOnline(navigator.onLine);window.addEventListener('online',update);window.addEventListener('offline',update);return()=>{window.removeEventListener('online',update);window.removeEventListener('offline',update)}},[])
  async function changeRecommendation(recommendationID:string,action:'accept'|'complete'){setBusy(true);setError('');try{await api(`/student/recommendations/${recommendationID}/${action}`,{method:'POST',body:'{}'});setProfile(await api<StudentLearningProfile>('/student/learning-profile'))}catch(e){setError(e instanceof Error?e.message:'Unable to update this recommendation.')}finally{setBusy(false)}}
  async function open(assignmentID:string){setSelected(assignmentID);setAssessment(null);setAnswers({});setError('');setRevisions({});revisionRef.current={};setFlags({});try{const data=await api<LearnerAssessment>(`/student/assessments/${assignmentID}`);setAssessment(data);const saved:Record<string,string|string[]>={},revisionMap:Record<string,number>={},flagMap:Record<string,boolean>={};for(const q of data.questions){revisionMap[q.id]=q.revision||0;flagMap[q.id]=q.flagged;if(q.answer!==undefined&&q.answer!==null&&q.answer!=='null'){const value=q.answer;saved[q.id]=Array.isArray(value)?value.map(String):String(value)}}if(data.attempt_id&&data.status==='in_progress'){const pending=await listPendingAnswers(data.attempt_id);for(const local of pending){if(local.revision>(revisionMap[local.question_id]??0)){revisionMap[local.question_id]=local.revision;flagMap[local.question_id]=local.flagged;if(local.answer!==null)saved[local.question_id]=local.answer as string|string[]}else await removePendingAnswer(local.attempt_id,local.question_id,local.idempotency_key)}}setAnswers(saved);setRevisions(revisionMap);revisionRef.current=revisionMap;setFlags(flagMap);if(data.status==='in_progress')setSyncState(online?'pending synchronization':'offline; saved on this device')}catch(e){setError(e instanceof Error?e.message:'Could not open this assessment.')}}
  async function start(){if(!selected)return;setBusy(true);setError('');try{await api(`/student/assessments/${selected}/start`,{method:'POST',body:'{}'});await open(selected)}catch(e){setError(e instanceof Error?e.message:'Could not start this assessment.')}finally{setBusy(false)}}
  async function flushPending(){if(flushing.current||!online||!assessment?.attempt_id||assessment.status!=='in_progress')return;flushing.current=true;try{for(let attempt=0;attempt<5;attempt++){const pending=await listPendingAnswers(assessment.attempt_id);if(!pending.length){setSyncState('all answers synchronized');break}let failed=false;for(const item of pending){try{const saved=await api<{revision:number}>(`/student/attempts/${item.attempt_id}/answers/${item.question_id}`,{method:'PUT',body:JSON.stringify({answer:item.answer,revision:item.revision,idempotency_key:item.idempotency_key,flagged:item.flagged})});await removePendingAnswer(item.attempt_id,item.question_id,item.idempotency_key);revisionRef.current={...revisionRef.current,[item.question_id]:saved.revision};setRevisions(current=>({...current,[item.question_id]:saved.revision}))}catch(e){failed=true;setSyncState(navigator.onLine?'sync paused; answer remains on this device':'offline; saved on this device');setError(e instanceof Error?e.message:'An answer is still waiting to synchronize.');break}}if(!failed)continue;if(attempt<4)await new Promise(resolve=>setTimeout(resolve,Math.min(500*2**attempt,8000)));else break}}catch(e){setSyncState('local recovery unavailable');setError(e instanceof Error?e.message:'This browser could not save an answer locally.')}finally{flushing.current=false}}
  useEffect(()=>{if(!online)return;const reconnect=()=>{void flushPending()};window.addEventListener('online',reconnect);if(assessment?.status==='in_progress')void flushPending();return()=>window.removeEventListener('online',reconnect)},[online,assessment?.attempt_id,assessment?.status])
  async function submitAnswers(){if(!assessment||!online){setError('Reconnect to the internet before submitting. Your saved answers remain on this device.');return}Object.values(answerTimers.current).forEach(clearTimeout);answerTimers.current={};setBusy(true);setError('');try{const result=await api<{auto_score:number;auto_max_score:number;manual_review_count:number;results_released:boolean;final_score:number|null;final_max_score:number|null}>(`/student/attempts/${assessment.attempt_id}/submit`,{method:'POST',body:JSON.stringify({answers})});await clearPendingAnswers(assessment.attempt_id);setAssessment({...assessment,status:'submitted',score:result.auto_score,max_score:result.auto_max_score,manual_review_count:result.manual_review_count,results_released:result.results_released,final_score:result.final_score,final_max_score:result.final_max_score});setSyncState('submitted');setAssignments(await api<LearnerAssignment[]>('/student/assessments'))}catch(e){setError(e instanceof Error?e.message:'Could not submit this assessment.')}finally{setBusy(false)}}
  async function submit(event:FormEvent<HTMLFormElement>){event.preventDefault();await submitAnswers()}
  function setAnswer(questionID:string,value:string|string[]|null,flagged=flags[questionID]??false){setAnswers(current=>({...current,[questionID]:value??''}));if(!assessment?.attempt_id||assessment.status!=='in_progress')return;const revision=(revisionRef.current[questionID]??0)+1,requestID=crypto.randomUUID();revisionRef.current={...revisionRef.current,[questionID]:revision};setRevisions(current=>({...current,[questionID]:revision}));setFlags(current=>({...current,[questionID]:flagged}));const pending={attempt_id:assessment.attempt_id,question_id:questionID,answer:value,revision,idempotency_key:requestID,flagged,saved_at:Date.now()};setSyncState(online?'saving on this device':'offline; saved on this device');void savePendingAnswer(pending).then(()=>{if(answerTimers.current[questionID])clearTimeout(answerTimers.current[questionID]);answerTimers.current[questionID]=setTimeout(()=>void flushPending(),online?450:1500)}).catch(e=>setError(e instanceof Error?e.message:'Unable to save this answer on this device.'))}
  function toggleFlag(questionID:string){setAnswer(questionID,answers[questionID]??null,!(flags[questionID]??false))}
  useEffect(()=>{if(!assessment?.deadline_at||assessment.status!=='in_progress')return;const tick=()=>{const left=Math.max(0,Math.ceil((Date.parse(assessment.deadline_at!)-Date.now())/1000));setSecondsLeft(left);if(left===0&&online&&deadlineSubmitted.current!==assessment.attempt_id){deadlineSubmitted.current=assessment.attempt_id;void submitAnswers()}if(left===0&&!online)setSyncState('deadline passed; reconnect to submit server-saved answers')};tick();const timer=setInterval(tick,1000);return()=>clearInterval(timer)},[assessment?.deadline_at,assessment?.status,online])
  return <div className="learner-shell"><aside className="learner-sidebar"><Brand/><p className="nav-heading">MY LEARNING</p><div className="learner-assignment-list">{assignments.map(a=><button key={a.assignment_id} className={selected===a.assignment_id?'selected':''} onClick={()=>void open(a.assignment_id)}><strong>{a.title}</strong><small>{a.grade_level} · {a.class_name}</small><span>{a.attempt_status==='not_started'?'Not started':a.attempt_status.replace('_',' ')}</span></button>)}{!assignments.length&&<p className="empty">No assessments have been assigned yet.</p>}</div><button className="teacher-logout" onClick={onLogout}>Sign out</button></aside><main className="learner-main"><header className="teacher-topbar"><span>Learning workspace</span><strong>{identity.display_name}</strong></header><div className="learner-content">{error&&<p className="form-error" role="alert">{error}</p>}{!assessment?<><div className="learner-welcome"><p className="eyebrow">STUDENT WORKSPACE</p><h1>Welcome, {identity.display_name.split(' ')[0]}</h1><p className="muted">Choose an assessment from your class to get started.</p></div>{profile&&<><section className="panel mastery-panel"><div className="panel-head"><div><p className="eyebrow">LEARNING PROFILE</p><h2>Skill mastery</h2><p>Early estimates from automatically graded responses. Confidence and evidence are shown separately.</p></div><span className="count-pill">{profile.algorithm_version}</span></div><div className="mastery-grid">{profile.skills.map(skill=><article className="mastery-card" key={skill.skill_id}><strong>{skill.title}</strong><span>{Math.round(skill.estimated_mastery)}% estimated mastery</span><div className="mastery-bar"><i style={{width:`${skill.estimated_mastery}%`}}/></div><small>{skill.evidence_count} responses · {skill.confidence_band} confidence</small></article>)}{!profile.skills.length&&<p className="empty">Skill estimates will appear after graded questions have curriculum skill tags.</p>}</div></section><section className="panel recommendation-panel"><div className="panel-head"><div><p className="eyebrow">NEXT STEPS</p><h2>Study recommendations</h2><p>Recommendations use a deterministic rule and show the evidence behind each priority.</p></div></div><div className="recommendation-list">{profile.recommendations.map(rec=><article className="recommendation-card" key={rec.id}><div className="recommendation-head"><strong>{rec.recommended_action.title||rec.skill_title}</strong><span className={`status-pill ${rec.priority}`}>{rec.priority}</span></div><p>{rec.recommended_action.description}</p><small>{rec.skill_title} · {rec.evidence_summary.evidence_count??0} responses · {Math.round(rec.evidence_summary.estimated_mastery??0)}% estimated mastery</small>{rec.status==='pending'&&<button className="secondary compact" disabled={busy} onClick={()=>void changeRecommendation(rec.id,'accept')}>Accept recommendation</button>}{rec.status==='accepted'&&<button className="secondary compact" disabled={busy} onClick={()=>void changeRecommendation(rec.id,'complete')}>Mark practice complete</button>}{rec.change_points!==null&&<small>Change after reassessment: {rec.change_points>0?'+':''}{rec.change_points} points.</small>}</article>)}{!profile.recommendations.length&&<p className="empty">No skill gaps meet the minimum evidence threshold yet.</p>}</div></section></>}</>:<><p className="eyebrow">{assessment.status==='submitted'?'SUBMITTED ASSESSMENT':'CLASS ASSESSMENT'}</p><h1>{assessment.title}</h1>{assessment.instructions&&<p className="learner-instructions">{assessment.instructions}</p>}{assessment.status==='not_started'&&<section className="panel learner-start"><p>This assessment has {assessment.questions.length} questions. You can submit it once.</p><button className="primary compact" disabled={busy} onClick={()=>void start()}>{busy?'Starting…':'Start assessment'} <span>→</span></button></section>}{assessment.status==='in_progress'&&<form className="learner-question-form" onSubmit={(e)=>void submit(e)}><div className="exam-status"><span>{online ? `Connected · ${syncState}` : `No connection · ${syncState}`}</span><strong>Time left: {Math.floor(secondsLeft/60)}:{String(secondsLeft%60).padStart(2,'0')}</strong></div><nav className="exam-question-nav" aria-label="Assessment questions">{assessment.questions.map((q,index)=><button type="button" key={q.id} className={flags[q.id]?'flagged-question':''} onClick={()=>document.getElementById(`question-${q.id}`)?.scrollIntoView({behavior:'smooth',block:'center'})} aria-label={`Go to question ${index+1}${flags[q.id]?", flagged for review":""}`}>{index+1}{flags[q.id]?' ⚑':''}</button>)}</nav><p className="form-hint">Answers are stored on this device first and sync when connected.</p>{assessment.questions.map((q,index)=><article id={`question-${q.id}`} key={q.id} className="panel learner-question"><div className="learner-question-title"><span>QUESTION {index+1}</span><small>{q.points} points <button type="button" className={flags[q.id]?'flag-toggle flagged':''} onClick={()=>toggleFlag(q.id)}>{flags[q.id]?'Unflag':'Flag for review'}</button></small></div><h2>{q.prompt?.text||'Question prompt'}</h2>{(q.question_type==='single_choice'||q.question_type==='multiple_choice')&&q.options?.map(option=>{const selectedValue=answers[q.id];const checked=Array.isArray(selectedValue)?selectedValue.includes(option.id):selectedValue===option.id;return <label className="learner-option" key={option.id}><input type={q.question_type==='single_choice'?'radio':'checkbox'} name={`answer-${q.id}`} checked={checked} onChange={()=>{if(q.question_type==='single_choice')setAnswer(q.id,option.id);else{const current=Array.isArray(answers[q.id])?answers[q.id] as string[]:[];setAnswer(q.id,checked?current.filter(x=>x!==option.id):[...current,option.id])}}}/><span>{option.text}</span></label>})}{q.question_type==='numeric'&&<label className="learner-text-answer">Your answer<input type="number" step="any" value={String(answers[q.id]??'')} onChange={e=>setAnswer(q.id,e.target.value)}/></label>}{q.question_type==='text'&&<label className="learner-text-answer">Your response<textarea rows={4} value={String(answers[q.id]??'')} onChange={e=>setAnswer(q.id,e.target.value)}/></label>}</article>)}<button className="primary learner-submit" disabled={busy||!online}>{busy?'Submitting…':'Submit assessment'} <span>→</span></button></form>}{assessment.status==='submitted'&&<section className="panel learner-result"><h2>{assessment.results_released?'Final result':'Assessment submitted'}</h2>{assessment.results_released?<p>Final score: <strong>{assessment.final_score??0} / {assessment.final_max_score??0}</strong></p>:<><p>Automatically graded so far: <strong>{assessment.score??0} / {assessment.max_score??0}</strong></p>{assessment.manual_review_count>0&&<p>{assessment.manual_review_count} written response(s) are waiting for teacher review.</p>}</>}{assessment.results_released&&assessment.questions.filter(q=>q.question_type==='text'&&q.feedback).map((q,index)=><div className="learner-feedback" key={q.id}><strong>Written response {index+1} · {q.awarded_points??0} / {q.points} points</strong><p>{q.feedback}</p></div>)}</section>}</>}</div></main></div>
}

function TeacherWorkspace({identity,classes,onLogout}:{identity:Identity;classes:ClassRoom[];onLogout:()=>void}) {
  const [selected,setSelected]=useState(''),[students,setStudents]=useState<TeacherStudent[]>([]),[reviews,setReviews]=useState<TeacherReview[]>([]),[learningRows,setLearningRows]=useState<TeacherLearningRow[]>([]),[error,setError]=useState(''),[loading,setLoading]=useState(false),[savingReview,setSavingReview]=useState('')
  useEffect(()=>{if(!classes.length){setSelected('');setStudents([]);return}setSelected((current)=>classes.some(c=>c.id===current)?current:classes[0].id)},[classes])
  useEffect(()=>{if(!selected){setStudents([]);setReviews([]);setLearningRows([]);return}let ignore=false;setLoading(true);setError('');Promise.all([api<TeacherStudent[]>(`/teacher/classes/${selected}/students`),api<TeacherReview[]>(`/teacher/reviews?class_id=${encodeURIComponent(selected)}`),api<TeacherLearningRow[]>(`/teacher/classes/${selected}/learning-profile`)]).then(([roster,queue,learning])=>{if(!ignore){setStudents(roster);setReviews(queue);setLearningRows(learning)}}).catch((e)=>{if(!ignore)setError(e instanceof Error?e.message:'Unable to load class information.')}).finally(()=>{if(!ignore)setLoading(false)});return()=>{ignore=true}},[selected])
  async function gradeReview(event:FormEvent<HTMLFormElement>,review:TeacherReview){event.preventDefault();const form=new FormData(event.currentTarget),points=Number(form.get('points'));setSavingReview(`${review.attempt_id}:${review.question_id}`);setError('');try{const result=await api<{results_released:boolean}>(`/teacher/attempts/${review.attempt_id}/answers/${review.question_id}`,{method:'PUT',body:JSON.stringify({points,feedback:form.get('feedback')})});setReviews(await api<TeacherReview[]>(`/teacher/reviews?class_id=${encodeURIComponent(selected)}`));if(result.results_released)setError('All written answers for this attempt are graded. The learner can now see the final result.')}catch(e){setError(e instanceof Error?e.message:'Unable to save this grade.')}finally{setSavingReview('')}}
  const classroom=classes.find(c=>c.id===selected)
  return <div className="teacher-shell"><aside className="teacher-sidebar"><Brand/><p className="nav-heading">TEACHING</p><div className="teacher-class-list">{classes.map(c=><button key={c.id} className={selected===c.id?'selected':''} onClick={()=>setSelected(c.id)}><span>{c.grade_level} · {c.name}</span><small>{c.academic_year_name}</small></button>)}</div><button className="teacher-logout" onClick={onLogout}>Sign out</button></aside><main className="teacher-main"><header className="teacher-topbar"><span>Teacher workspace</span><strong>{identity.display_name}</strong></header><div className="teacher-content"><p className="eyebrow">MY CLASSES</p><h1>{classroom?`${classroom.grade_level} · ${classroom.name}`:'No classes assigned yet'}</h1><p className="muted">{classroom?`Student roster · ${classroom.academic_year_name}`:'Ask your school administrator to assign you to a class.'}</p>{error&&<p className="form-error" role="alert">{error}</p>}{classroom&&<><section className="panel teacher-roster"><div className="panel-head"><div><h2>Students</h2><p>{loading?'Loading roster…':`${students.length} active students`}</p></div><span className="count-pill">{students.length} learners</span></div><div className="table-wrap"><table><thead><tr><th>STUDENT</th><th>STUDENT ID</th><th>LANGUAGE</th><th>STATUS</th></tr></thead><tbody>{students.map(s=><tr key={s.id}><td><strong>{s.legal_name}</strong></td><td>{s.student_number}</td><td>{s.preferred_language||'—'}</td><td><span className="status-pill">{s.status}</span></td></tr>)}{!loading&&!students.length&&<tr><td colSpan={4} className="empty">No active students are enrolled in this class yet.</td></tr>}</tbody></table></div></section><section className="panel teacher-review-panel"><div className="panel-head"><div><p className="eyebrow">LEARNING OVERVIEW</p><h2>Skill evidence</h2><p>Early estimates include response counts and confidence; low-evidence estimates need more assessment data.</p></div></div><div className="table-wrap"><table><thead><tr><th>STUDENT</th><th>SKILL</th><th>ESTIMATED MASTERY</th><th>CONFIDENCE</th><th>ACTIVE PLANS</th></tr></thead><tbody>{learningRows.map((row,index)=><tr key={`${row.student_id}:${row.skill_id||index}`}><td><strong>{row.student_name}</strong><small>{row.student_number}</small></td><td>{row.skill_title||'No skill evidence yet'}</td><td>{row.skill_id?`${Math.round(row.estimated_mastery)}%`:'—'}</td><td>{row.skill_id?`${row.confidence_band} · ${row.evidence_count} responses`:'—'}</td><td>{row.active_recommendations}</td></tr>)}{!loading&&!learningRows.length&&<tr><td colSpan={5} className="empty">No active students are enrolled in this class yet.</td></tr>}</tbody></table></div></section><section className="panel teacher-review-panel"><div className="panel-head"><div><p className="eyebrow">ASSESSMENT FOLLOW-UP</p><h2>Written answers to grade</h2><p>Scores are released to learners when every written response in an attempt has been reviewed.</p></div><span className="count-pill">{reviews.length} pending</span></div><div className="teacher-review-list">{reviews.map(review=>{const reviewID=`${review.attempt_id}:${review.question_id}`;let answer=review.answer;if(typeof answer==='string'){try{answer=JSON.parse(answer) as unknown}catch{}}return <article className="teacher-review-card" key={reviewID}><div className="review-meta"><strong>{review.student_name}</strong><span>{review.student_number} · {review.assessment_title}</span></div><h3>{review.prompt?.text||'Written response'}</h3>{Boolean(review.grading_guide)&&<p className="review-guide"><strong>Grading guide:</strong> {typeof review.grading_guide==='string'?review.grading_guide:JSON.stringify(review.grading_guide)}</p>}<div className="review-answer">{typeof answer==='string'?answer:JSON.stringify(answer)}</div><form className="review-grade-form" onSubmit={e=>void gradeReview(e,review)}><label>Points earned <span>out of {review.max_points}</span><input name="points" type="number" min="0" max={review.max_points} step="0.01" required/></label><label>Feedback for learner<textarea name="feedback" rows={2} maxLength={5000} defaultValue={review.feedback}/></label><button className="primary compact" disabled={savingReview===reviewID}>{savingReview===reviewID?'Saving…':'Save grade'}</button></form></article>})}{!reviews.length&&<p className="empty">No written answers are waiting for review in this class.</p>}</div></section></>}</div></main></div>
}

function Brand() { return <div className="brand"><div className="brand-mark">F</div><div><strong>FURII</strong><span>SCHOOL OS</span></div></div> }
function Metric({icon,label,value,detail}:{icon:string;label:string;value:string;detail:string}) { return <article className="metric"><div className="metric-top"><span>{label}</span><i>{icon}</i></div><strong>{value}</strong><small>{detail}</small></article> }

createRoot(document.getElementById('root')!).render(<App />)
