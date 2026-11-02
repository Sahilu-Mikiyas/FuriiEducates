import { Suspense, useEffect } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router'
import { Toast, useFeedback } from '../../app/feedback'
import { useLogout } from '../../app/session'
import { Brand, LanguageSwitch, PageLoading, WorkspaceLinks } from '../../app/shared'
import { useDates } from '../../calendar/useDates'
import { useI18n } from '../../i18n'
import { useSchool, useStudents } from '../../lib/queries'
import type { Identity } from '../../lib/types'
import { adminPages, AdminPageID } from './pages'

export default function AdminLayout({ identity }: { identity: Identity }) {
  const { t } = useI18n()
  const { today } = useDates()
  const { clear } = useFeedback()
  const logout = useLogout()
  const { pathname } = useLocation()
  const school = useSchool().data
  const students = useStudents().data ?? []

  // Messages belong to the page that raised them.
  useEffect(() => { clear(); window.scrollTo({ top: 0, behavior: 'auto' }) }, [pathname, clear])

  const slug = pathname.replace(/^\/admin\/?/, '').split('/')[0]
  const page = (adminPages.some((item) => item.id === slug) ? slug : 'overview') as AdminPageID
  const visible = adminPages.filter((item) => item.id !== 'demo-requests' || identity.platform_operator)
  const initial = identity.display_name.slice(0, 1).toUpperCase()

  return <div className="app-shell">
    <aside className="sidebar">
      <Brand />
      <div className="school-switch">
        <div className="school-avatar">{school?.name.slice(0, 1) ?? 'F'}</div>
        <div><strong>{school?.name ?? t('admin.yourSchool')}</strong><span>{t('admin.schoolWorkspace')}</span></div>
      </div>
      <p className="nav-heading">{t('admin.workspace')}</p>
      <nav className="admin-nav" aria-label={t('admin.navLabel')}>
        {visible.map((item) => <NavLink key={item.id} to={`/admin/${item.id}`} className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}>
          <span>{item.icon}</span>{t(`admin.nav.${item.id}`)}{item.id === 'students' && <small>{students.length}</small>}
        </NavLink>)}
      </nav>
      <div className="sidebar-bottom">
        <WorkspaceLinks identity={identity} current="admin" />
        <div className="user-chip">
          <div className="user-avatar">{initial}</div>
          <div><strong>{identity.display_name}</strong><span>{t('admin.role')}</span></div>
          <button aria-label={t('common.signOut')} title={t('common.signOut')} onClick={() => void logout()}>↗</button>
        </div>
      </div>
    </aside>
    <main className="main-area">
      <header className="topbar">
        <span>{t('admin.workspaceCrumb')} <b>/</b> {t(`admin.nav.${page}`)}</span>
        <div className="top-actions">
          <LanguageSwitch />
          <div className="user-avatar small">{initial}</div>
          <button className="topbar-signout" onClick={() => void logout()}>{t('common.signOut')}</button>
        </div>
      </header>
      <div className="content">
        <div className="page-title">
          <div>
            <p className="eyebrow">{page === 'overview' ? t('admin.overviewEyebrow') : t('admin.workspace')}</p>
            <h1>{page === 'overview' ? <>{t('admin.greeting', { name: identity.display_name.split(' ')[0] })} <span className="wave">✳</span></> : t(`admin.title.${page}`)}</h1>
            <p className="muted">{t(`admin.desc.${page}`)}</p>
          </div>
          {page === 'overview' && <div className="date-pill">{today()}</div>}
        </div>
        <Toast />
        <Suspense fallback={<PageLoading />}><Outlet /></Suspense>
        <footer>FURII SCHOOL OS <span>·</span> ACADEMIC FOUNDATION</footer>
      </div>
    </main>
  </div>
}
