import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useAction } from '../../app/feedback'
import { useDates } from '../../calendar/useDates'
import { channels, outsideTopics } from '../../features/notifications/notifications'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useClasses, useRefresh } from '../../lib/queries'
import type { Announcement, AnnouncementResult, Delivery, NoticeChannel, NoticeTopic, NotificationOverview } from '../../lib/types'

const audiences = ['everyone', 'parents', 'students', 'staff', 'class'] as const

// MessagesPage is where the school sends announcements, decides which
// automatic messages may go by email, SMS or Telegram, and checks what was sent.
export default function MessagesPage() {
  const { t, label } = useI18n()
  const { dateTime } = useDates()
  const { busy, run, onSubmit } = useAction()
  const refresh = useRefresh()
  const classes = useClasses().data ?? []
  const overview = useQuery({ queryKey: ['/notifications/overview'], queryFn: () => api<NotificationOverview>('/notifications/overview') }).data
  const announcementQuery = useQuery({ queryKey: ['/announcements'], queryFn: () => api<Announcement[]>('/announcements') })
  const announcements = announcementQuery.data ?? []
  const [filter, setFilter] = useState({ status: '', channel: '' })
  const deliveryPath = `/notifications/deliveries?status=${filter.status}&channel=${filter.channel}`
  const deliveryQuery = useQuery({ queryKey: ['/notifications/deliveries', filter], queryFn: () => api<Delivery[]>(deliveryPath) })
  const deliveries = deliveryQuery.data ?? []
  const [audience, setAudience] = useState<string>('parents')
  const [rules, setRules] = useState<Record<NoticeChannel, NoticeTopic[]> | null>(null)

  const ready = (channel: NoticeChannel) => overview?.channels.find((c) => c.channel === channel)?.ready ?? false
  const currentRules = rules ?? overview?.rules
  const refreshAll = () => refresh('/notifications/overview', '/notifications/deliveries', '/announcements')

  const summary = (result: AnnouncementResult) => {
    const parts = channels.map((channel) => {
      const counts = result.channels[channel]
      if (!counts) return ''
      const waiting = (counts.pending ?? 0)
      const skipped = counts.skipped ?? 0
      return waiting ? t('messages.willSend', { count: waiting, channel: label('notifyChannel', channel) })
        : skipped ? t('messages.wouldSkip', { count: skipped, channel: label('notifyChannel', channel) }) : ''
    }).filter(Boolean)
    return [t('messages.reaches', { count: result.recipients }), ...parts].join(' · ')
  }

  const send = onSubmit(async (f) => {
    const body = {
      title: f.get('title'), body: f.get('body'), audience, class_id: audience === 'class' ? f.get('class_id') : '',
      channels: f.getAll('channels'),
    }
    const preview = await api<AnnouncementResult>('/announcements', { method: 'POST', body: JSON.stringify({ ...body, dry_run: true }) })
    if (!window.confirm(`${summary(preview)}\n\n${t('messages.confirmSend')}`)) throw new Error(t('messages.notSent'))
    await api('/announcements', { method: 'POST', body: JSON.stringify(body) })
    await refreshAll()
  }, t('messages.sent'))

  const saveRules = () => currentRules && void run(async () => {
    await api('/notifications/rules', { method: 'PUT', body: JSON.stringify(currentRules) })
    setRules(null)
    await refresh('/notifications/overview')
  }, t('messages.rulesSaved'))

  const test = onSubmit(async (f) => {
    await api('/notifications/test', { method: 'POST', body: JSON.stringify({ channel: f.get('channel'), address: f.get('address') }) })
    await refresh('/notifications/deliveries')
  }, t('messages.testSent'), false)

  const retry = (delivery: Delivery) => void run(async () => {
    await api(`/notifications/deliveries/${delivery.id}/retry`, { method: 'POST' })
    await refreshAll()
  }, t('messages.retryQueued'))

  const counted = (channel: NoticeChannel, status: string) => overview?.last_30_days.find((c) => c.channel === channel && c.status === status)?.count ?? 0

  return <>
    <section className="panel">
      <div className="panel-head"><div><h2>{t('messages.channels')}</h2><p>{t('messages.channelsHelp')}</p></div></div>
      <div className="channel-cards">
        {channels.map((channel) => <article key={channel} className={ready(channel) ? 'ready' : 'off'}>
          <strong>{label('notifyChannel', channel)}</strong>
          <span className="status-pill">{ready(channel) ? t('messages.ready', { provider: overview?.channels.find((c) => c.channel === channel)?.provider ?? '' }) : t('notify.notSetUp')}</span>
          <small>{t('messages.last30', { sent: counted(channel, 'sent'), failed: counted(channel, 'failed'), skipped: counted(channel, 'skipped') })}</small>
        </article>)}
        <article className="ready"><strong>{t('messages.inApp')}</strong><span className="status-pill">{t('messages.alwaysOn')}</span><small>{t('messages.inAppHelp')}</small></article>
      </div>
      {overview && !channels.every(ready) && <p className="form-hint">{t('messages.setupHint')}</p>}
      {overview && !overview.links_ready && <p className="form-hint">{t('messages.linksHint')}</p>}
      {overview && <p className="form-hint">{t('messages.smsThisMonth', { count: overview.sms_this_month })}</p>}
      {channels.some(ready) && <form className="inline-form message-test" onSubmit={test}>
        <select name="channel" aria-label={t('messages.testChannel')}>{channels.filter(ready).map((channel) => <option key={channel} value={channel}>{label('notifyChannel', channel)}</option>)}</select>
        <input name="address" placeholder={t('messages.testAddress')} aria-label={t('messages.testAddress')} />
        <button className="secondary" disabled={busy}>{t('messages.sendTest')}</button>
      </form>}
    </section>

    <div className="work-grid">
      <section className="panel">
        <div className="panel-head"><div><h2>{t('messages.announce')}</h2><p>{t('messages.announceHelp')}</p></div></div>
        <form className="stack-form announce-form" onSubmit={send}>
          <div className="two-fields">
            <label>{t('messages.audience')}<select value={audience} onChange={(e) => setAudience(e.target.value)}>
              {audiences.map((value) => <option key={value} value={value}>{label('audience', value)}</option>)}
            </select></label>
            {audience === 'class' && <label>{t('common.class')}<select name="class_id" required>
              {classes.map((c) => <option key={c.id} value={c.id}>{c.grade_level} · {c.name}</option>)}
            </select></label>}
          </div>
          <label>{t('messages.title')}<input name="title" maxLength={120} required /></label>
          <label>{t('messages.body')}<textarea name="body" maxLength={2000} rows={5} required /></label>
          <fieldset className="notice-topics">
            <legend>{t('messages.alsoSendBy')}</legend>
            {channels.map((channel) => <label key={channel} className="bank-check">
              <input type="checkbox" name="channels" value={channel} disabled={!ready(channel)} /> {label('notifyChannel', channel)}{!ready(channel) && <small> · {t('notify.notSetUp')}</small>}
            </label>)}
          </fieldset>
          <button className="primary" disabled={busy}>{t('messages.send')}<span>→</span></button>
        </form>
      </section>

      <section className="panel">
        <div className="panel-head"><div><h2>{t('messages.rules')}</h2><p>{t('messages.rulesHelp')}</p></div></div>
        {currentRules && <div className="table-wrap"><table className="rules-table">
          <thead><tr><th>{t('messages.topic')}</th>{channels.map((channel) => <th key={channel}>{label('notifyChannel', channel)}</th>)}</tr></thead>
          <tbody>{outsideTopics.map((topic) => <tr key={topic}>
            <td>{label('notifyTopic', topic)}</td>
            {channels.map((channel) => <td key={channel}><input type="checkbox" aria-label={`${label('notifyTopic', topic)} · ${label('notifyChannel', channel)}`}
              checked={currentRules[channel].includes(topic)}
              onChange={(e) => setRules({ ...currentRules, [channel]: e.target.checked ? [...currentRules[channel], topic] : currentRules[channel].filter((x) => x !== topic) })} /></td>)}
          </tr>)}</tbody>
        </table></div>}
        <p className="form-hint">{t('messages.rulesNote')}</p>
        <button className="secondary" disabled={busy || !rules} onClick={saveRules}>{t('messages.saveRules')}</button>
      </section>
    </div>

    <section className="panel">
      <div className="panel-head"><div><h2>{t('messages.past')}</h2><p>{t('messages.pastHelp')}</p></div></div>
      {announcementQuery.isSuccess && !announcements.length && <p className="empty">{t('messages.noAnnouncements')}</p>}
      <div className="announcement-list">
        {announcements.map((a) => <article key={a.id}>
          <div><strong>{a.title}</strong><small>{dateTime(a.created_at)} · {a.sent_by} · {a.audience === 'class' ? a.class_name : label('audience', a.audience)}</small></div>
          <p>{a.body}</p>
          <small>{t('messages.reaches', { count: a.recipients })}{channels.filter((c) => a.channels.includes(c)).map((c) => ` · ${label('notifyChannel', c)}: ${t('messages.sentOf', { sent: a.deliveries[`${c}.sent`] ?? 0, total: Object.entries(a.deliveries).filter(([k]) => k.startsWith(`${c}.`)).reduce((sum, [, v]) => sum + v, 0) })}`).join('')}</small>
        </article>)}
      </div>
    </section>

    <section className="panel">
      <div className="panel-head">
        <div><h2>{t('messages.log')}</h2><p>{t('messages.logHelp')}</p></div>
        <div className="inline-form log-filters">
          <select aria-label={t('messages.filterStatus')} value={filter.status} onChange={(e) => setFilter({ ...filter, status: e.target.value })}>
            <option value="">{t('messages.allStatuses')}</option>
            {['pending', 'sent', 'failed', 'skipped', 'cancelled'].map((s) => <option key={s} value={s}>{label('deliveryStatus', s)}</option>)}
          </select>
          <select aria-label={t('messages.filterChannel')} value={filter.channel} onChange={(e) => setFilter({ ...filter, channel: e.target.value })}>
            <option value="">{t('messages.allChannels')}</option>
            {channels.map((c) => <option key={c} value={c}>{label('notifyChannel', c)}</option>)}
          </select>
        </div>
      </div>
      <div className="table-wrap"><table className="delivery-table">
        <thead><tr><th>{t('messages.col.when')}</th><th>{t('messages.col.to')}</th><th>{t('messages.col.what')}</th><th>{t('messages.col.status')}</th><th /></tr></thead>
        <tbody>
          {deliveryQuery.isSuccess && !deliveries.length && <tr><td colSpan={5} className="empty">{t('messages.noDeliveries')}</td></tr>}
          {deliveries.map((d) => <tr key={d.id}>
            <td>{dateTime(d.created_at)}</td>
            <td><strong>{d.name || '—'}</strong><small>{label('notifyChannel', d.channel)} · {d.address}</small></td>
            <td>{label('notifyKind', d.kind)}{d.subject && <small>{d.subject}</small>}</td>
            <td><span className={`status-pill delivery-${d.status}`}>{label('deliveryStatus', d.status)}</span>{d.error && <small>{d.error}</small>}</td>
            <td>{(d.status === 'failed' || d.status === 'skipped') && d.body && ready(d.channel) && <button className="link-button" disabled={busy} onClick={() => retry(d)}>{t('messages.retry')}</button>}</td>
          </tr>)}
        </tbody>
      </table></div>
    </section>
  </>
}
