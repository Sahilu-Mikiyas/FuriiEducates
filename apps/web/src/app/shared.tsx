import { Component, ReactNode } from 'react'
import { Link } from 'react-router'
import { languages, Language, useI18n } from '../i18n'
import type { Identity } from '../lib/types'

export function Brand() {
  return <div className="brand"><div className="brand-mark">F</div><div><strong>FURII</strong><span>SCHOOL OS</span></div></div>
}

export function Metric({ icon, label, value, detail }: { icon: string; label: string; value: string; detail: string }) {
  return <article className="metric"><div className="metric-top"><span>{label}</span><i>{icon}</i></div><strong>{value}</strong><small>{detail}</small></article>
}

export function LanguageSwitch() {
  const { t, language, setLanguage } = useI18n()
  return <label className="language-switch">
    <span>{t('preferences.language')}</span>
    <select value={language} onChange={(e) => setLanguage(e.target.value as Language)}>
      {languages.map((option) => <option key={option.code} value={option.code}>{option.label}</option>)}
    </select>
  </label>
}

export function Loading() {
  const { t } = useI18n()
  return <main className="loading">{t('common.opening')}</main>
}

export function PageLoading() {
  const { t } = useI18n()
  return <p className="empty" role="status">{t('common.loading')}</p>
}

// WorkspaceLinks lets someone with several roles move between their workspaces.
export function WorkspaceLinks({ identity, current }: { identity: Identity; current: 'admin' | 'teacher' }) {
  const { t } = useI18n()
  return <>
    {current !== 'admin' && identity.roles.includes('school_admin') && <Link className="workspace-link" to="/admin/overview">{t('workspace.openAdmin')}</Link>}
    {current !== 'teacher' && identity.roles.includes('teacher') && <Link className="workspace-link" to="/teach/class">{t('workspace.openTeacher')}</Link>}
  </>
}

function CrashScreen() {
  const { t } = useI18n()
  return <main className="invite-shell"><div className="invite-card"><Brand />
    <h1>{t('crash.title')}</h1>
    <p className="muted">{t('crash.body')}</p>
    <button className="primary" onClick={() => window.location.reload()}>{t('crash.reload')} <span>→</span></button>
  </div></main>
}

// ErrorBoundary catches rendering failures and pages that cannot load, such as a
// file replaced by a new release while this tab was open.
export class ErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false }
  static getDerivedStateFromError() { return { failed: true } }
  render() { return this.state.failed ? <CrashScreen /> : this.props.children }
}
