import { FormEvent, useState } from 'react'
import { Brand, LanguageSwitch } from '../../app/shared'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'

// InvitePage redeems a one-time invitation, for staff (#invite=), a learner
// (#student-invite=) or a parent (#parent-invite=), by choosing a name and a password.
export default function InvitePage({ token, kind, role = 'teacher' }: { token: string; kind: 'staff' | 'student' | 'parent'; role?: string }) {
  const { t, errorText } = useI18n()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [acceptedRole, setAcceptedRole] = useState(role)
  const [email, setEmail] = useState('')

  async function accept(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const password = String(form.get('password') || '')
    setError('')
    if (password !== String(form.get('confirm_password') || '')) { setError(t('account.passwordsDiffer')); return }
    setBusy(true)
    try {
      const path = kind === 'staff' ? '/staff/invitations/accept' : kind === 'parent' ? '/guardian-invitations/accept' : '/student-invitations/accept'
      const result = await api<{ email: string; role?: string }>(path, { method: 'POST', body: JSON.stringify({ token, display_name: form.get('display_name'), password }) })
      setEmail(result.email)
      if (result.role) setAcceptedRole(result.role)
    } catch (e) { setError(errorText(e, 'error.generic')) } finally { setBusy(false) }
  }

  const who = kind !== 'staff' ? kind : acceptedRole === 'school_admin' ? 'admin' : 'teacher'
  return <main className="invite-shell"><div className="invite-card">
    <Brand />
    <LanguageSwitch />
    <p className="eyebrow">{t(`invite.${who}.eyebrow`)}</p>
    <h1>{t('invite.title')}</h1>
    <p className="muted">{t(`invite.${who}.help`)}</p>
    {email
      ? <>
          <p className="invite-success">{t(`invite.${who}.done`, { email })}</p>
          <a className="primary invite-login-link" href="/login">{t('common.continueToSignIn')} <span>→</span></a>
        </>
      : <form className="stack-form invite-form" onSubmit={(e) => void accept(e)}>
          <label>{t('invite.name')}<input name="display_name" autoComplete="name" required maxLength={200} /></label>
          <label>{t('invite.password')}<input name="password" type="password" autoComplete="new-password" minLength={12} required /><small>{t('invite.passwordHelp')}</small></label>
          <label>{t('account.confirmNewPassword')}<input name="confirm_password" type="password" autoComplete="new-password" minLength={12} required /></label>
          {error && <p className="form-error" role="alert">{error}</p>}
          <button className="primary" disabled={busy}>{busy ? t('invite.creating') : t('invite.accept')} <span>→</span></button>
        </form>}
  </div></main>
}
