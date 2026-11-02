import { useQueryClient } from '@tanstack/react-query'
import { FormEvent, useState } from 'react'
import { Navigate } from 'react-router'
import { homePath, SESSION_KEY, useSession } from '../../app/session'
import { Brand, LanguageSwitch, Loading } from '../../app/shared'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import type { Identity } from '../../lib/types'

export default function LoginPage() {
  const { t, errorText } = useI18n()
  const client = useQueryClient()
  const { identity, loading } = useSession()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function login(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    setBusy(true); setError('')
    try {
      const user = await api<Identity>('/auth/login', { method: 'POST', body: JSON.stringify({ email: form.get('email'), password: form.get('password') }) })
      if (!user.roles.some((role) => role === 'school_admin' || role === 'teacher' || role === 'student')) {
        await api('/auth/logout', { method: 'POST' })
        throw new Error(t('login.roleNotEnabled'))
      }
      client.setQueryData(SESSION_KEY, user)
    } catch (e) { setError(errorText(e, 'error.generic')) } finally { setBusy(false) }
  }

  if (loading) return <Loading />
  if (identity) return <Navigate to={homePath(identity)} replace />

  return <main className="login-shell">
    <div className="login-art">
      <Brand />
      <div className="login-copy">
        <p className="eyebrow">{t('login.artEyebrow')}</p>
        <h1>{t('login.artTitle')}</h1>
        <p>{t('login.artBody')}</p>
        <div className="art-orbit orbit-one" /><div className="art-orbit orbit-two" /><div className="art-spark">✳</div>
      </div>
      <span className="login-footnote">FURII SCHOOL OS <span>·</span> MADE FOR LEARNING</span>
    </div>
    <div className="login-pane">
      <form className="login-form" onSubmit={(e) => void login(e)}>
        <LanguageSwitch />
        <p className="eyebrow">{t('login.eyebrow')}</p>
        <h2>{t('login.title')}</h2>
        <p className="muted">{t('login.help')}</p>
        <label>{t('login.email')}<input name="email" type="email" autoComplete="username" placeholder={t('login.emailHint')} required /></label>
        <label>{t('login.password')}<input name="password" type="password" autoComplete="current-password" placeholder={t('login.passwordHint')} required /></label>
        {error && <p className="form-error" role="alert">{error}</p>}
        <button className="primary" disabled={busy}>{busy ? t('login.signingIn') : t('login.signIn')} <span>→</span></button>
        <p className="login-help">{t('login.needAccess')}</p>
      </form>
    </div>
  </main>
}
