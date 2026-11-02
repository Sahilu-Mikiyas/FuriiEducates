import { FormEvent, useState } from 'react'
import { Brand } from '../../app/shared'
import { CalendarMode, languages, Language, useI18n } from '../../i18n'
import { api } from '../../lib/api'

const MIN_PASSWORD_LENGTH = 12

export function passwordResetLink(token: string) {
  return `${window.location.origin}/#reset=${encodeURIComponent(token)}`
}

// ChangePasswordPanel lets any signed-in user replace their password. The API
// signs out the account's other sessions.
export function ChangePasswordPanel() {
  const { t, errorText } = useI18n()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    const next = String(values.get('new_password') || '')
    setError(''); setNotice('')
    if (next !== String(values.get('confirm_password') || '')) { setError(t('account.passwordsDiffer')); return }
    setBusy(true)
    try {
      await api('/auth/password', { method: 'POST', body: JSON.stringify({ current_password: values.get('current_password'), new_password: next }) })
      form.reset()
      setNotice(t('account.passwordChanged'))
    } catch (e) {
      setError(errorText(e, 'error.generic'))
    } finally { setBusy(false) }
  }
  return <section className="panel account-panel">
    <div className="panel-head"><div><h2>{t('account.changePassword')}</h2><p>{t('account.changePasswordHelp', { n: MIN_PASSWORD_LENGTH })}</p></div></div>
    <form className="stack-form" onSubmit={(e) => void submit(e)}>
      <div className="history-fields">
        <label>{t('account.currentPassword')}<input name="current_password" type="password" autoComplete="current-password" required /></label>
        <label>{t('account.newPassword')}<input name="new_password" type="password" autoComplete="new-password" minLength={MIN_PASSWORD_LENGTH} required /></label>
        <label>{t('account.confirmNewPassword')}<input name="confirm_password" type="password" autoComplete="new-password" minLength={MIN_PASSWORD_LENGTH} required /></label>
      </div>
      {error && <p className="form-error" role="alert">{error}</p>}
      {notice && <p className="invite-success" role="status">{notice}</p>}
      <button className="secondary" disabled={busy}>{busy ? t('common.saving') : t('account.changePassword')}</button>
    </form>
  </section>
}

// PreferencesPanel sets the language and calendar for this device.
export function PreferencesPanel() {
  const { t, language, setLanguage, calendar, setCalendar } = useI18n()
  return <section className="panel account-panel">
    <div className="panel-head"><div><h2>{t('preferences.title')}</h2><p>{t('preferences.help')}</p></div></div>
    <div className="preferences-fields">
      <label>{t('preferences.language')}
        <select value={language} onChange={(e) => setLanguage(e.target.value as Language)}>
          {languages.map((option) => <option key={option.code} value={option.code}>{option.label}</option>)}
        </select>
      </label>
      <label>{t('preferences.calendar')}
        <select value={calendar} onChange={(e) => setCalendar(e.target.value as CalendarMode)}>
          <option value="both">{t('preferences.calendarBoth')}</option>
          <option value="ethiopian">{t('preferences.calendarEthiopian')}</option>
          <option value="gregorian">{t('preferences.calendarGregorian')}</option>
        </select>
      </label>
    </div>
  </section>
}

// CopyLinkResult shows a one-time link with a copy button.
export function CopyLinkResult({ title, detail, link, onCopied, onCopyFailed }: { title: string; detail: string; link: string; onCopied: () => void; onCopyFailed: () => void }) {
  const { t } = useI18n()
  return <div className="invite-result">
    <div><strong>{title}</strong><small>{detail}</small><code>{link}</code></div>
    <button className="secondary compact" onClick={() => void navigator.clipboard.writeText(link).then(onCopied).catch(onCopyFailed)}>{t('common.copyLink')}</button>
  </div>
}

// PasswordResetPage redeems a one-time reset link opened from #reset=<token>.
export function PasswordResetPage({ token }: { token: string }) {
  const { t, errorText } = useI18n()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [email, setEmail] = useState('')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const values = new FormData(event.currentTarget)
    const next = String(values.get('new_password') || '')
    setError('')
    if (next !== String(values.get('confirm_password') || '')) { setError(t('account.passwordsDiffer')); return }
    setBusy(true)
    try {
      const result = await api<{ email: string }>('/auth/password-reset/complete', { method: 'POST', body: JSON.stringify({ token, new_password: next }) })
      setEmail(result.email)
      window.history.replaceState({}, '', '/login')
    } catch (e) {
      setError(errorText(e, 'error.generic'))
    } finally { setBusy(false) }
  }
  return <main className="invite-shell"><div className="invite-card"><Brand /><p className="eyebrow">{t('reset.eyebrow')}</p><h1>{t('reset.title')}</h1>
    {email
      ? <><p className="invite-success">{t('reset.done', { email })}</p><a className="primary invite-login-link" href="/login">{t('common.continueToSignIn')} <span>→</span></a></>
      : <form className="stack-form invite-form" onSubmit={(e) => void submit(e)}>
          <p className="muted">{t('reset.help', { n: MIN_PASSWORD_LENGTH })}</p>
          <label>{t('account.newPassword')}<input name="new_password" type="password" autoComplete="new-password" minLength={MIN_PASSWORD_LENGTH} required /></label>
          <label>{t('account.confirmNewPassword')}<input name="confirm_password" type="password" autoComplete="new-password" minLength={MIN_PASSWORD_LENGTH} required /></label>
          {error && <p className="form-error" role="alert">{error}</p>}
          <button className="primary" disabled={busy}>{busy ? t('common.saving') : t('reset.submit')} <span>→</span></button>
        </form>}
  </div></main>
}
