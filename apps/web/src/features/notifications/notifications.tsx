import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { NavLink, useNavigate } from 'react-router'
import { useAction } from '../../app/feedback'
import { useDates } from '../../calendar/useDates'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import type { NoticeChannel, NoticeTopic, NotificationInbox, NotificationItem, NotificationSettings } from '../../lib/types'

export const outsideTopics: NoticeTopic[] = ['attendance', 'reports', 'schoolwork', 'announcements']
export const channels: NoticeChannel[] = ['email', 'sms', 'telegram']

function useUnread() {
  return useQuery({ queryKey: ['/me/notifications/unread'], queryFn: () => api<{ unread: number }>('/me/notifications/unread'), refetchInterval: 60_000 }).data?.unread ?? 0
}

function useRefreshNotices() {
  const client = useQueryClient()
  return () => Promise.all([client.invalidateQueries({ queryKey: ['/me/notifications'] }), client.invalidateQueries({ queryKey: ['/me/notifications/unread'] })])
}

// NotificationBell links to the inbox and shows how many notices are unread.
export function NotificationBell({ base }: { base: string }) {
  const { t } = useI18n()
  const unread = useUnread()
  return <NavLink to={`${base}/notifications`} className={({ isActive }) => `notice-bell ${isActive ? 'active' : ''}`} aria-label={t('notify.bell', { count: unread })} title={t('notify.bell', { count: unread })}>
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true"><path d="M12 3a6 6 0 0 0-6 6v3.6L4.3 15.4A1 1 0 0 0 5.2 17h13.6a1 1 0 0 0 .9-1.6L18 12.6V9a6 6 0 0 0-6-6Zm-2.4 15.5a2.5 2.5 0 0 0 4.8 0Z" fill="currentColor" /></svg>
    {unread > 0 && <span className="notice-count">{unread > 99 ? '99+' : unread}</span>}
  </NavLink>
}

// NotificationsPage is a person's inbox. Opening a notice marks it read and
// goes to the page it is about.
export function NotificationsPage() {
  const { t, language, label } = useI18n()
  const { dateTime } = useDates()
  const navigate = useNavigate()
  const { busy, run } = useAction()
  const refresh = useRefreshNotices()
  const inbox = useQuery({ queryKey: ['/me/notifications', language], queryFn: () => api<NotificationInbox>(`/me/notifications?lang=${language}&limit=50`) })
  const items = inbox.data?.items ?? []

  const markRead = (body: object) => api('/me/notifications/read', { method: 'POST', body: JSON.stringify(body) }).then(refresh)
  const open = (item: NotificationItem) => {
    if (!item.read) void markRead({ ids: [item.id] })
    if (item.link) navigate(item.link)
  }

  return <section className="panel notice-inbox">
    <div className="panel-head">
      <div><h2>{t('notify.inbox')}</h2><p>{t('notify.inboxHelp')}</p></div>
      {(inbox.data?.unread ?? 0) > 0 && <button className="secondary" disabled={busy} onClick={() => void run(() => markRead({ all: true }))}>{t('notify.markAll')}</button>}
    </div>
    {inbox.isLoading && <p className="muted">{t('common.loading')}</p>}
    {!inbox.isLoading && items.length === 0 && <p className="empty">{t('notify.empty')}</p>}
    <ul className="notice-list">
      {items.map((item) => <li key={item.id} className={item.read ? 'read' : 'unread'}>
        <button type="button" onClick={() => open(item)} disabled={!item.link && item.read}>
          <span className={`notice-topic topic-${item.topic}`}>{label('notifyTopic', item.topic)}</span>
          <strong>{item.title}</strong>
          {item.body && item.body !== item.title && <span className="notice-body">{item.body}</span>}
          <small>{dateTime(item.created_at)}{!item.read && <> · {t('notify.new')}</>}</small>
        </button>
      </li>)}
    </ul>
  </section>
}

// NotificationSettingsPanel lets a person choose how the school reaches them
// outside the app. Notices always appear in the app.
export function NotificationSettingsPanel() {
  const { t, label } = useI18n()
  const { busy, run } = useAction()
  const client = useQueryClient()
  const settings = useQuery({ queryKey: ['/me/notification-settings'], queryFn: () => api<NotificationSettings>('/me/notification-settings') }).data
  const [draft, setDraft] = useState<NotificationSettings | null>(null)
  const [telegramURL, setTelegramURL] = useState('')
  useEffect(() => { if (settings) setDraft(settings) }, [settings])

  // While the person is in Telegram, check every few seconds whether they pressed Start.
  useEffect(() => {
    if (!telegramURL) return
    const started = Date.now()
    const timer = window.setInterval(async () => {
      if (Date.now() - started > 5 * 60_000) { setTelegramURL(''); return }
      try {
        const result = await api<{ connected: boolean }>('/me/telegram-link/check', { method: 'POST' })
        if (result.connected) { setTelegramURL(''); await client.invalidateQueries({ queryKey: ['/me/notification-settings'] }) }
      } catch { /* the next check will try again */ }
    }, 4000)
    return () => window.clearInterval(timer)
  }, [telegramURL, client])

  if (!draft) return null
  const ready = (channel: NoticeChannel) => draft.channels.find((c) => c.channel === channel)?.ready ?? false
  const set = (change: Partial<NotificationSettings>) => setDraft({ ...draft, ...change })
  const enabledKey = { email: 'email_enabled', sms: 'sms_enabled', telegram: 'telegram_enabled' } as const

  const save = () => void run(async () => {
    const saved = await api<NotificationSettings>('/me/notification-settings', { method: 'PUT', body: JSON.stringify({
      language: draft.language, email_enabled: draft.email_enabled, sms_enabled: draft.sms_enabled, telegram_enabled: draft.telegram_enabled,
      muted_topics: draft.muted_topics, phone: draft.phone,
    }) })
    client.setQueryData(['/me/notification-settings'], saved)
  }, t('notify.settingsSaved'))
  const connect = () => void run(async () => {
    const result = await api<{ url: string }>('/me/telegram-link', { method: 'POST' })
    setTelegramURL(result.url)
  })
  const disconnect = () => void run(async () => {
    await api('/me/telegram-link', { method: 'DELETE' })
    await client.invalidateQueries({ queryKey: ['/me/notification-settings'] })
  }, t('notify.telegramRemoved'))

  return <section className="panel account-panel notice-settings">
    <div className="panel-head"><div><h2>{t('notify.settings')}</h2><p>{t('notify.settingsHelp')}</p></div></div>
    <label className="notice-language">{t('notify.messageLanguage')}
      <select value={draft.language} onChange={(e) => set({ language: e.target.value as 'en' | 'am' })}>
        <option value="en">English</option><option value="am">አማርኛ</option>
      </select>
    </label>
    <div className="notice-channels">
      {channels.map((channel) => <div key={channel} className={`notice-channel ${ready(channel) ? '' : 'off'}`}>
        <label className="bank-check">
          <input type="checkbox" disabled={!ready(channel)} checked={ready(channel) && draft[enabledKey[channel]]} onChange={(e) => set({ [enabledKey[channel]]: e.target.checked })} />
          <strong>{label('notifyChannel', channel)}</strong>
        </label>
        {!ready(channel) && <small>{t('notify.notSetUp')}</small>}
        {ready(channel) && channel === 'email' && <small>{draft.email || t('notify.noEmail')}</small>}
        {channel === 'sms' && <input aria-label={t('notify.phone')} placeholder="0911 234 567" value={draft.phone} onChange={(e) => set({ phone: e.target.value })} />}
        {ready(channel) && channel === 'telegram' && (draft.telegram_connected
          ? <span className="notice-telegram"><small>{t('notify.telegramConnected')}</small><button type="button" className="link-button" disabled={busy} onClick={disconnect}>{t('notify.telegramDisconnect')}</button></span>
          : telegramURL
            ? <span className="notice-telegram"><a className="secondary" href={telegramURL} target="_blank" rel="noreferrer">{t('notify.telegramOpen')}</a><small>{t('notify.telegramWaiting')}</small></span>
            : <button type="button" className="secondary" disabled={busy} onClick={connect}>{t('notify.telegramConnect')}</button>)}
      </div>)}
    </div>
    <fieldset className="notice-topics">
      <legend>{t('notify.topicsOutside')}</legend>
      {outsideTopics.map((topic) => <label key={topic} className="bank-check">
        <input type="checkbox" checked={!draft.muted_topics.includes(topic)} onChange={(e) => set({ muted_topics: e.target.checked ? draft.muted_topics.filter((x) => x !== topic) : [...draft.muted_topics, topic] })} />
        {label('notifyTopic', topic)}
      </label>)}
    </fieldset>
    <p className="muted">{t('notify.alwaysInApp')}</p>
    <button className="primary" disabled={busy} onClick={save}>{t('notify.saveSettings')}</button>
  </section>
}
