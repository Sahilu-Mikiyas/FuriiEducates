import { FormEvent, ReactNode, useState } from 'react'
import { api } from './api'

const MIN_PASSWORD_LENGTH = 12

export function passwordResetLink(token: string) {
  return `${window.location.origin}/#reset=${encodeURIComponent(token)}`
}

// ChangePasswordPanel lets any signed-in user replace their password. The API
// signs out the account's other sessions.
export function ChangePasswordPanel() {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    const next = String(values.get('new_password') || '')
    setError(''); setNotice('')
    if (next !== String(values.get('confirm_password') || '')) { setError('The new passwords do not match.'); return }
    setBusy(true)
    try {
      await api('/auth/password', { method: 'POST', body: JSON.stringify({ current_password: values.get('current_password'), new_password: next }) })
      form.reset()
      setNotice('Password changed. Other devices signed in to this account have been signed out.')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Unable to change your password.')
    } finally { setBusy(false) }
  }
  return <section className="panel account-panel">
    <div className="panel-head"><div><h2>Change password</h2><p>Use at least {MIN_PASSWORD_LENGTH} characters. Changing it signs out your other devices.</p></div></div>
    <form className="stack-form" onSubmit={(e) => void submit(e)}>
      <div className="history-fields">
        <label>Current password<input name="current_password" type="password" autoComplete="current-password" required /></label>
        <label>New password<input name="new_password" type="password" autoComplete="new-password" minLength={MIN_PASSWORD_LENGTH} required /></label>
        <label>Confirm new password<input name="confirm_password" type="password" autoComplete="new-password" minLength={MIN_PASSWORD_LENGTH} required /></label>
      </div>
      {error && <p className="form-error" role="alert">{error}</p>}
      {notice && <p className="invite-success" role="status">{notice}</p>}
      <button className="secondary" disabled={busy}>{busy ? 'Saving…' : 'Change password'}</button>
    </form>
  </section>
}

// ResetLinkResult shows a one-time password reset link with a copy button.
export function ResetLinkResult({ name, link, onCopied, onCopyFailed }: { name: string; link: string; onCopied: () => void; onCopyFailed: () => void }) {
  return <div className="invite-result">
    <div><strong>Password reset link for {name}</strong><small>Works once and expires in 24 hours. Share it privately. Anyone with this link can set the password.</small><code>{link}</code></div>
    <button className="secondary compact" onClick={() => void navigator.clipboard.writeText(link).then(onCopied).catch(onCopyFailed)}>Copy link</button>
  </div>
}

// PasswordResetPage redeems a one-time reset link opened from #reset=<token>.
export function PasswordResetPage({ token, brand }: { token: string; brand: ReactNode }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [email, setEmail] = useState('')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const values = new FormData(event.currentTarget)
    const next = String(values.get('new_password') || '')
    setError('')
    if (next !== String(values.get('confirm_password') || '')) { setError('The passwords do not match.'); return }
    setBusy(true)
    try {
      const result = await api<{ email: string }>('/auth/password-reset/complete', { method: 'POST', body: JSON.stringify({ token, new_password: next }) })
      setEmail(result.email)
      window.history.replaceState({}, '', '/login')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Unable to reset your password.')
    } finally { setBusy(false) }
  }
  return <main className="invite-shell"><div className="invite-card">{brand}<p className="eyebrow">PASSWORD RESET</p><h1>Choose a new password</h1>
    {email
      ? <><p className="invite-success">The password for {email} has been changed. Sign in with your new password.</p><a className="primary invite-login-link" href="/login">Continue to sign in <span>→</span></a></>
      : <form className="stack-form invite-form" onSubmit={(e) => void submit(e)}>
          <p className="muted">This link works once. Use at least {MIN_PASSWORD_LENGTH} characters.</p>
          <label>New password<input name="new_password" type="password" autoComplete="new-password" minLength={MIN_PASSWORD_LENGTH} required /></label>
          <label>Confirm new password<input name="confirm_password" type="password" autoComplete="new-password" minLength={MIN_PASSWORD_LENGTH} required /></label>
          {error && <p className="form-error" role="alert">{error}</p>}
          <button className="primary" disabled={busy}>{busy ? 'Saving…' : 'Set new password'} <span>→</span></button>
        </form>}
  </div></main>
}
