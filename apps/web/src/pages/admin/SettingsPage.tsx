import { useQueryClient } from '@tanstack/react-query'
import { useAction } from '../../app/feedback'
import { SESSION_KEY } from '../../app/session'
import { ChangePasswordPanel, PreferencesPanel } from '../../features/account/account'
import { NotificationSettingsPanel } from '../../features/notifications/notifications'
import { languages, useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useRefresh, useSchool, useSetupProfile } from '../../lib/queries'

export default function SettingsPage() {
  const { t } = useI18n()
  const { busy, onSubmit } = useAction()
  const refresh = useRefresh()
  const client = useQueryClient()
  const school = useSchool().data
  const profile = useSetupProfile().data

  const save = onSubmit(async (f) => {
    await api('/school', { method: 'PATCH', body: JSON.stringify({ name: f.get('name'), timezone: f.get('timezone'), default_language: f.get('default_language') }) })
    await Promise.all([refresh('/school'), client.invalidateQueries({ queryKey: SESSION_KEY })])
  }, t('settings.saved'), false)

  // A school may already store a language this app does not offer yet.
  const languageOptions = school && !languages.some((l) => l.code === school.default_language)
    ? [...languages, { code: school.default_language, label: school.default_language }]
    : languages

  return <>
    <section className="panel settings-panel">
      <div className="panel-head"><div><h2>{t('settings.profile')}</h2><p>{t('settings.help')}</p></div><span className="settings-mark">✳</span></div>
      {profile && <article className="setup-profile-card">
        <p className="eyebrow">{t('settings.onboarding')}</p>
        <h3>{t('settings.onboardingLine', { size: profile.learner_count_band, region: profile.region })}</h3>
        <p>{t('settings.priorities')}</p>
        <div>{profile.priorities.map((priority) => <span key={priority}>{priority.replaceAll('-', ' ')}</span>)}</div>
      </article>}
      {school && <form className="school-form" key={school.id} onSubmit={save}>
        <label>{t('settings.name')}<input name="name" defaultValue={school.name} required /></label>
        <label>{t('settings.timezone')}<input name="timezone" defaultValue={school.timezone} required /></label>
        <label>{t('settings.defaultLanguage')}<select name="default_language" defaultValue={school.default_language}>
          {languageOptions.map((option) => <option key={option.code} value={option.code}>{option.label}</option>)}
        </select></label>
        <button className="secondary" disabled={busy}>{t('settings.save')}</button>
      </form>}
    </section>
    <NotificationSettingsPanel />
    <PreferencesPanel />
    <ChangePasswordPanel />
  </>
}
