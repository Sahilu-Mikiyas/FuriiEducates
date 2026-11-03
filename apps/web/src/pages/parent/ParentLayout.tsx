import { Suspense, useEffect } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router'
import { Toast, useFeedback } from '../../app/feedback'
import { useLogout } from '../../app/session'
import { Brand, LanguageSwitch, PageLoading, WorkspaceLinks } from '../../app/shared'
import { useI18n } from '../../i18n'
import type { Identity } from '../../lib/types'

export default function ParentLayout({ identity }: { identity: Identity }) {
  const { t } = useI18n()
  const { clear } = useFeedback()
  const logout = useLogout()
  const { pathname } = useLocation()
  useEffect(() => { clear(); window.scrollTo({ top: 0, behavior: 'auto' }) }, [pathname, clear])
  return <div className="learner-shell">
    <aside className="learner-sidebar">
      <Brand />
      <nav className="workspace-nav" aria-label={t('parent.navLabel')}>
        <NavLink to="/family/children" className={({ isActive }) => (isActive ? 'active' : '')}>{t('parent.nav.children')}</NavLink>
        <NavLink to="/family/account" className={({ isActive }) => (isActive ? 'active' : '')}>{t('teacher.nav.account')}</NavLink>
      </nav>
      <div className="workspace-foot">
        <WorkspaceLinks identity={identity} current="parent" />
        <LanguageSwitch />
        <button className="teacher-logout" onClick={() => void logout()}>{t('common.signOut')}</button>
      </div>
    </aside>
    <main className="learner-main">
      <header className="teacher-topbar"><span>{t('parent.workspace')}</span><strong>{identity.display_name}</strong></header>
      <div className="learner-content">
        <Toast />
        <Suspense fallback={<PageLoading />}><Outlet /></Suspense>
      </div>
    </main>
  </div>
}
