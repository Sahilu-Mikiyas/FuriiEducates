import { useState } from 'react'
import { useFeedback } from '../../app/feedback'
import { useDates } from '../../calendar/useDates'
import { CopyLinkResult } from '../../features/account/account'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useDemoRequests, useRefresh } from '../../lib/queries'
import type { DemoRequest } from '../../lib/types'

// Platform operators prepare new school workspaces from landing page requests.
export default function SetupRequestsPage() {
  const { t, errorText } = useI18n()
  const { dateTime } = useDates()
  const { setNotice, setError, clear } = useFeedback()
  const refresh = useRefresh()
  const query = useDemoRequests()
  const requests = query.data ?? []
  const [saving, setSaving] = useState('')
  const [preparing, setPreparing] = useState('')
  const [inviteLink, setInviteLink] = useState('')

  async function prepare(item: DemoRequest) {
    setPreparing(item.id); clear(); setInviteLink('')
    try {
      const result = await api<{ request: DemoRequest; invitation_token: string; expires_at: string }>(`/platform/demo-requests/${item.id}/prepare`, { method: 'POST', body: '{}' })
      await refresh('/platform/demo-requests')
      setInviteLink(`${window.location.origin}/#invite=${encodeURIComponent(result.invitation_token)}&invite-role=school_admin`)
      setNotice(t('setup.prepared', { email: item.email }))
    } catch (e) { setError(errorText(e, 'error.generic')) } finally { setPreparing('') }
  }

  async function updateStatus(item: DemoRequest, status: DemoRequest['status']) {
    setSaving(item.id); clear()
    try {
      await api(`/platform/demo-requests/${item.id}`, { method: 'PATCH', body: JSON.stringify({ status }) })
      await refresh('/platform/demo-requests')
      setNotice(t('setup.updated'))
    } catch (e) { setError(errorText(e, 'error.generic')) } finally { setSaving('') }
  }

  return <section className="panel demo-requests-panel">
    <div className="panel-head">
      <div><p className="eyebrow">{t('setup.eyebrow')}</p><h2>{t('admin.title.demo-requests')}</h2><p>{t('setup.help')}</p></div>
      <span className="count-pill">{t('setup.newCount', { n: requests.filter((r) => r.status === 'new').length })}</span>
    </div>
    {inviteLink && <CopyLinkResult title={t('setup.linkTitle')} detail={t('setup.linkDetail')} link={inviteLink} onCopied={() => setNotice(t('setup.linkCopied'))} onCopyFailed={() => setError(t('common.copyFailed'))} />}
    {query.isPending
      ? <p className="empty">{t('common.loading')}</p>
      : requests.length
        ? <div className="demo-request-list">{requests.map((request) => <article className="demo-request-card" key={request.id}>
            <div className="demo-request-head">
              <div><h3>{request.school_name}</h3><p>{t('setup.contactLine', { name: request.name, role: request.role, size: request.school_size })}</p></div>
              <time dateTime={request.created_at}>{dateTime(request.created_at)}</time>
            </div>
            <a href={`mailto:${encodeURIComponent(request.email)}?subject=${encodeURIComponent(`FURII school setup for ${request.school_name}`)}`}>{request.email}</a>
            <p className="demo-request-message">{t('setup.regionLine', { region: request.region, timezone: request.timezone, language: request.default_language })}</p>
            <p className="demo-request-message">{t('setup.priorities', { list: request.priorities?.join(', ') || t('common.notProvided') })}</p>
            {request.message && <p className="demo-request-message">{request.message}</p>}
            <label className="demo-status-label">{t('setup.status')}
              <select value={request.status} disabled={saving === request.id} onChange={(e) => void updateStatus(request, e.currentTarget.value as DemoRequest['status'])}>
                <option value="new">{t('setup.status.new')}</option>
                <option value="contacted">{t('setup.status.contacted')}</option>
                <option value="closed">{t('setup.status.closed')}</option>
              </select>
            </label>
            <div className="setup-request-action">
              {request.prepared_school_id && <span className="status-pill">{t('setup.workspacePrepared')}</span>}
              <button className="primary compact" disabled={Boolean(preparing) || request.status === 'closed'} onClick={() => void prepare(request)}>
                {preparing === request.id ? t('setup.preparing') : request.prepared_school_id ? t('setup.renew') : t('setup.prepare')}
              </button>
            </div>
          </article>)}</div>
        : <p className="empty">{t('setup.empty')}</p>}
  </section>
}
