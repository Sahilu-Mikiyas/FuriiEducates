import { lazy, ReactNode, Suspense, useEffect } from 'react'
import { Navigate, Route, Routes } from 'react-router'
import { PasswordResetPage } from '../features/account/account'
import { ExamSessions } from '../features/exams/ExamSessions'
import { FinanceWorkspace } from '../features/operations/Finance'
import { LessonPlanReview } from '../features/operations/LessonPlans'
import { useI18n } from '../i18n'
import type { Identity } from '../lib/types'
import InvitePage from '../pages/public/InvitePage'
import LoginPage from '../pages/public/LoginPage'
import { homePath, useSession } from './session'
import { Loading } from './shared'

// Each workspace loads only when opened, which matters on school connections.
// The learner workspace, exam page included, loads as one file so an exam never
// waits on another download once a student has signed in.
const MarketingLanding = lazy(() => import('../marketing').then((m) => ({ default: m.MarketingLanding })))
const AdminLayout = lazy(() => import('../pages/admin/AdminLayout'))
const OverviewPage = lazy(() => import('../pages/admin/OverviewPage'))
const StudentsPage = lazy(() => import('../pages/admin/StudentsPage'))
const AttendancePage = lazy(() => import('../pages/admin/AttendancePage'))
const StudentProfile = lazy(() => import('../features/students/StudentProfile'))
const ReportSetupPage = lazy(() => import('../pages/admin/ReportSetupPage'))
const AdminMarksPage = lazy(() => import('../pages/admin/MarksPage').then((m) => ({ default: m.AdminMarksPage })))
const AdminReportCardsPage = lazy(() => import('../pages/admin/MarksPage').then((m) => ({ default: m.AdminReportCardsPage })))
const ClassesPage = lazy(() => import('../pages/admin/ClassesPage'))
const AcademicHistoryPage = lazy(() => import('../pages/admin/AcademicHistoryPage'))
const CurriculumPage = lazy(() => import('../pages/admin/CurriculumPage'))
const AssessmentsPage = lazy(() => import('../pages/admin/AssessmentsPage'))
const ResultsPage = lazy(() => import('../pages/admin/ResultsPage'))
const StaffPage = lazy(() => import('../pages/admin/StaffPage'))
const SetupRequestsPage = lazy(() => import('../pages/admin/SetupRequestsPage'))
const SettingsPage = lazy(() => import('../pages/admin/SettingsPage'))
const ExamPrepPage = lazy(() => import('../pages/admin/ExamPrepPage'))
const QuestionBankPage = lazy(() => import('../features/prep/QuestionBank').then((m) => ({ default: m.QuestionBank })))
const MockDaysPage = lazy(() => import('../features/prep/MockDays').then((m) => ({ default: m.MockDays })))
const TeacherLayout = lazy(() => import('../pages/teacher/TeacherLayout'))
const ClassPage = lazy(() => import('../pages/teacher/ClassPage'))
const TeacherAttendancePage = lazy(() => import('../pages/teacher/AttendancePage'))
const TeacherMarksPage = lazy(() => import('../pages/teacher/MarksPages').then((m) => ({ default: m.TeacherMarksPage })))
const TeacherReportCardsPage = lazy(() => import('../pages/teacher/MarksPages').then((m) => ({ default: m.TeacherReportCardsPage })))
const teacherPage = (name: 'TeacherReadinessPage' | 'TeacherSessionsPage' | 'TeacherGradebookPage' | 'TeacherAnswersPage' | 'TeacherPlanningPage' | 'TeacherAccountPage') =>
  lazy(() => import('../pages/teacher/pages').then((m) => ({ default: m[name] })))
const TeacherSessionsPage = teacherPage('TeacherSessionsPage')
const TeacherReadinessPage = teacherPage('TeacherReadinessPage')
const TeacherGradebookPage = teacherPage('TeacherGradebookPage')
const TeacherAnswersPage = teacherPage('TeacherAnswersPage')
const TeacherPlanningPage = teacherPage('TeacherPlanningPage')
const TeacherAccountPage = teacherPage('TeacherAccountPage')
const learnerWorkspace = () => import('../pages/learner/workspace')
const LearnerLayout = lazy(() => learnerWorkspace().then((m) => ({ default: m.LearnerLayout })))
const LearnerHome = lazy(() => learnerWorkspace().then((m) => ({ default: m.LearnerHome })))
const ExamRoute = lazy(() => learnerWorkspace().then((m) => ({ default: m.ExamRoute })))
const LearnerAccountPage = lazy(() => learnerWorkspace().then((m) => ({ default: m.LearnerAccountPage })))
const LearnerReports = lazy(() => learnerWorkspace().then((m) => ({ default: m.LearnerReports })))
const PrepHome = lazy(() => learnerWorkspace().then((m) => ({ default: m.PrepHome })))
const PrepSessionRoute = lazy(() => learnerWorkspace().then((m) => ({ default: m.PrepSessionRoute })))
const ComputerSkills = lazy(() => learnerWorkspace().then((m) => ({ default: m.ComputerSkills })))
const OfflineSittingRoute = lazy(() => learnerWorkspace().then((m) => ({ default: m.OfflineSittingRoute })))

// RequireRole shows `render` to a signed-in user with the role, sends anyone
// else to sign in, and sends other roles to their own workspace.
function RequireRole({ role, render }: { role: string; render: (identity: Identity) => ReactNode }) {
  const { identity, loading } = useSession()
  if (loading) return <Loading />
  if (!identity) return <Navigate to="/login" replace />
  if (!identity.roles.includes(role)) return <Navigate to={homePath(identity)} replace />
  return <>{render(identity)}</>
}

function OperatorOnly({ children }: { children: ReactNode }) {
  const { identity } = useSession()
  return identity?.platform_operator ? <>{children}</> : <Navigate to="/admin/overview" replace />
}

function HomeRedirect() {
  const { identity, loading } = useSession()
  if (loading) return <Loading />
  return <Navigate to={identity ? homePath(identity) : '/login'} replace />
}

const hashValue = (name: string) => new URLSearchParams(window.location.hash.replace(/^#/, '')).get(name) ?? ''

export function App() {
  const { setSchoolLanguage } = useI18n()
  const { identity } = useSession(false)

  useEffect(() => { setSchoolLanguage(identity?.default_language ?? '') }, [identity?.default_language, setSchoolLanguage])

  // Invitation and reset links are read once at start; reload when one is pasted into an open tab.
  useEffect(() => {
    const onHashChange = () => { if (/(^|[#&])(invite|student-invite|reset)=/.test(window.location.hash)) window.location.reload() }
    window.addEventListener('hashchange', onHashChange)
    return () => window.removeEventListener('hashchange', onHashChange)
  }, [])

  const resetToken = hashValue('reset')
  const staffInvite = hashValue('invite')
  const studentInvite = hashValue('student-invite')
  if (resetToken) return <PasswordResetPage token={resetToken} />
  if (staffInvite) return <InvitePage kind="staff" token={staffInvite} role={hashValue('invite-role') || 'teacher'} />
  if (studentInvite) return <InvitePage kind="student" token={studentInvite} />

  return <Suspense fallback={<Loading />}>
    <Routes>
      <Route path="/" element={<MarketingLanding />} />
      <Route path="/login" element={<LoginPage />} />
      <Route path="/admin" element={<RequireRole role="school_admin" render={(user) => <AdminLayout identity={user} />} />}>
        <Route index element={<Navigate to="overview" replace />} />
        <Route path="overview" element={<OverviewPage />} />
        <Route path="students" element={<StudentsPage />} />
        <Route path="students/:studentID" element={<StudentProfile canEdit base="/admin" />} />
        <Route path="attendance" element={<AttendancePage />} />
        <Route path="classes" element={<ClassesPage />} />
        <Route path="academic-passport" element={<AcademicHistoryPage />} />
        <Route path="curriculum" element={<CurriculumPage />} />
        <Route path="assessments" element={<AssessmentsPage />} />
        <Route path="exam-prep" element={<ExamPrepPage />} />
        <Route path="exam-questions" element={<QuestionBankPage />} />
        <Route path="mock-days" element={<MockDaysPage />} />
        <Route path="exam-sessions" element={<ExamSessions />} />
        <Route path="results" element={<ResultsPage />} />
        <Route path="marks" element={<AdminMarksPage />} />
        <Route path="report-cards" element={<AdminReportCardsPage />} />
        <Route path="report-setup" element={<ReportSetupPage />} />
        <Route path="finance" element={<FinanceWorkspace />} />
        <Route path="lesson-plans" element={<LessonPlanReview />} />
        <Route path="staff" element={<StaffPage />} />
        <Route path="demo-requests" element={<OperatorOnly><SetupRequestsPage /></OperatorOnly>} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="*" element={<Navigate to="/admin/overview" replace />} />
      </Route>
      <Route path="/teach" element={<RequireRole role="teacher" render={(user) => <TeacherLayout identity={user} />} />}>
        <Route index element={<Navigate to="class" replace />} />
        <Route path="class" element={<ClassPage />} />
        <Route path="attendance" element={<TeacherAttendancePage />} />
        <Route path="marks" element={<TeacherMarksPage />} />
        <Route path="report-cards" element={<TeacherReportCardsPage />} />
        <Route path="students/:studentID" element={<StudentProfile canEdit={false} base="/teach" />} />
        <Route path="exam-sessions" element={<TeacherSessionsPage />} />
        <Route path="gradebook" element={<TeacherGradebookPage />} />
        <Route path="written-answers" element={<TeacherAnswersPage />} />
        <Route path="planning" element={<TeacherPlanningPage />} />
        <Route path="exam-prep" element={<TeacherReadinessPage />} />
        <Route path="exam-questions" element={<QuestionBankPage />} />
        <Route path="mock-days" element={<MockDaysPage />} />
        <Route path="account" element={<TeacherAccountPage />} />
        <Route path="*" element={<Navigate to="/teach/class" replace />} />
      </Route>
      <Route path="/learn" element={<RequireRole role="student" render={(user) => <LearnerLayout identity={user} />} />}>
        <Route index element={<LearnerHome />} />
        <Route path="assessments/:assignmentID" element={<ExamRoute />} />
        <Route path="account" element={<LearnerAccountPage />} />
        <Route path="report-cards" element={<LearnerReports />} />
        <Route path="prep" element={<PrepHome />} />
        <Route path="prep/computer-skills" element={<ComputerSkills />} />
        <Route path="prep/session/:sessionID" element={<PrepSessionRoute />} />
        <Route path="prep/offline/:packID" element={<OfflineSittingRoute />} />
        <Route path="*" element={<Navigate to="/learn" replace />} />
      </Route>
      <Route path="*" element={<HomeRedirect />} />
    </Routes>
  </Suspense>
}
