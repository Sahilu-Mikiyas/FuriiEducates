import { Suspense, useEffect, useState } from 'react'
import { NavLink, Outlet, useLocation, useOutletContext } from 'react-router'
import { Toast, useFeedback } from '../../app/feedback'
import { useLogout } from '../../app/session'
import { Brand, LanguageSwitch, PageLoading, WorkspaceLinks } from '../../app/shared'
import { MessageKey, useI18n } from '../../i18n'
import { useTeacherClasses } from '../../lib/queries'
import { ClassRoom, classLabel, Identity } from '../../lib/types'

export type TeacherContext = { classes: ClassRoom[]; classID: string; classroom: ClassRoom | undefined }
export const useTeacherContext = () => useOutletContext<TeacherContext>()

const teacherPages: { path: string; label: MessageKey }[] = [
  { path: 'class', label: 'teacher.nav.class' },
  { path: 'attendance', label: 'teacher.nav.attendance' },
  { path: 'quizzes', label: 'teacher.nav.quizzes' },
  { path: 'marks', label: 'teacher.nav.marks' },
  { path: 'report-cards', label: 'teacher.nav.reportCards' },
  { path: 'exam-sessions', label: 'teacher.nav.sessions' },
  { path: 'gradebook', label: 'teacher.nav.gradebook' },
  { path: 'written-answers', label: 'teacher.nav.answers' },
  { path: 'planning', label: 'teacher.nav.planning' },
  { path: 'exam-prep', label: 'teacher.nav.examPrep' },
  { path: 'exam-questions', label: 'teacher.nav.examQuestions' },
  { path: 'mock-days', label: 'teacher.nav.mockDays' },
  { path: 'account', label: 'teacher.nav.account' },
]

export default function TeacherLayout({ identity }: { identity: Identity }) {
  const { t } = useI18n()
  const { clear } = useFeedback()
  const logout = useLogout()
  const { pathname } = useLocation()
  const classesQuery = useTeacherClasses()
  const classes = classesQuery.data ?? []
  const [selected, setSelected] = useState('')
  const classroom = classes.find((c) => c.id === selected) ?? classes[0]

  useEffect(() => { clear(); window.scrollTo({ top: 0, behavior: 'auto' }) }, [pathname, clear])

  return <div className="teacher-shell">
    <aside className="teacher-sidebar">
      <Brand />
      <p className="nav-heading">{t('teacher.classesHeading')}</p>
      <div className="teacher-class-list">
        {classes.map((c) => <button key={c.id} className={classroom?.id === c.id ? 'selected' : ''} onClick={() => setSelected(c.id)}>
          <span>{classLabel(c)}</span><small>{c.academic_year_name}</small>
        </button>)}
      </div>
      <p className="nav-heading">{t('teacher.pagesHeading')}</p>
      <nav className="workspace-nav" aria-label={t('teacher.navLabel')}>
        {teacherPages.map((page) => <NavLink key={page.path} to={`/teach/${page.path}`} className={({ isActive }) => (isActive ? 'active' : '')}>{t(page.label)}</NavLink>)}
      </nav>
      <div className="workspace-foot">
        <WorkspaceLinks identity={identity} current="teacher" />
        <LanguageSwitch />
        <button className="teacher-logout" onClick={() => void logout()}>{t('common.signOut')}</button>
      </div>
    </aside>
    <main className="teacher-main">
      <header className="teacher-topbar"><span>{t('teacher.workspace')}</span><strong>{identity.display_name}</strong></header>
      <div className="teacher-content">
        <Toast />
        <p className="eyebrow">{t('teacher.classesHeading')}</p>
        <h1>{classroom ? classLabel(classroom) : t('teacher.noClasses')}</h1>
        <p className="muted">{classroom ? classroom.academic_year_name : t('teacher.noClassesHelp')}</p>
        <Suspense fallback={<PageLoading />}><Outlet context={{ classes, classID: classroom?.id ?? '', classroom } satisfies TeacherContext} /></Suspense>
      </div>
    </main>
  </div>
}
