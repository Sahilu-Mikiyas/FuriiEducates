import { useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { Link, NavLink, Outlet, useLocation } from 'react-router'
import { useFeedback, Toast } from '../../app/feedback'
import { useOnline } from '../../app/pwa'
import { SESSION_KEY, useLogout } from '../../app/session'
import { Brand, LanguageSwitch } from '../../app/shared'
import { useI18n } from '../../i18n'
import { useLearnerAssignments } from '../../lib/queries'
import type { Identity } from '../../lib/types'

export default function LearnerLayout({ identity }: { identity: Identity }) {
  const { t, label } = useI18n()
  const { clear } = useFeedback()
  const logout = useLogout()
  const { pathname } = useLocation()
  const client = useQueryClient()
  const online = useOnline()
  const assignmentsQuery = useLearnerAssignments()
  const assignments = assignmentsQuery.data ?? []

  useEffect(() => { clear() }, [pathname, clear])
  // A student stays signed in offline from their last visit. Check with the server again once connected.
  useEffect(() => { if (online) void client.invalidateQueries({ queryKey: SESSION_KEY }) }, [online, client])

  return <div className="learner-shell">
    <aside className="learner-sidebar">
      <Brand />
      <nav className="workspace-nav" aria-label={t('learner.navLabel')}>
        <NavLink end to="/learn" className={({ isActive }) => (isActive ? 'active' : '')}>{t('learner.home')}</NavLink>
        <NavLink to="/learn/prep" className={({ isActive }) => (isActive ? 'active' : '')}>{t('prep.nav')}</NavLink>
        <NavLink to="/learn/report-cards" className={({ isActive }) => (isActive ? 'active' : '')}>{t('reports.studentNav')}</NavLink>
        <NavLink to="/learn/account" className={({ isActive }) => (isActive ? 'active' : '')}>{t('teacher.nav.account')}</NavLink>
      </nav>
      <p className="nav-heading">{t('learner.heading')}</p>
      <div className="learner-assignment-list">
        {assignments.map((a) => <Link key={a.assignment_id} className={`learner-assignment ${pathname === `/learn/assessments/${a.assignment_id}` ? 'selected' : ''}`} to={`/learn/assessments/${a.assignment_id}`}>
          <strong>{a.title}</strong>
          <small>{a.grade_level} · {a.class_name}</small>
          <span>{label('status', a.attempt_status)}</span>
        </Link>)}
        {assignmentsQuery.isSuccess && !assignments.length && <p className="empty">{t('learner.noAssignments')}</p>}
      </div>
      <div className="workspace-foot">
        <LanguageSwitch />
        <button className="teacher-logout" onClick={() => void logout()}>{t('common.signOut')}</button>
      </div>
    </aside>
    <main className="learner-main">
      <header className="teacher-topbar"><span>{t('learner.workspace')}</span><strong>{identity.display_name}</strong></header>
      <div className="learner-content">
        {!online && <p className="offline-banner" role="status">{t('offline.banner')} <Link to="/learn/prep">{t('offline.bannerLink')}</Link></p>}
        <Toast />
        <Outlet />
      </div>
    </main>
  </div>
}
