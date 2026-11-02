import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router'
import { useOnline, useInstall } from '../../app/pwa'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useRefresh } from '../../lib/queries'
import { deletePack, listPacks, listSittings, OfflinePack, OfflineSitting, PackItem, savePack, syncOfflineSittings } from './offlineStore'
import { usePaperName } from './readiness'

const PACK_QUESTIONS = 30

// OfflinePanel downloads practice to this device and lists what is ready. It
// works with no connection, apart from downloading.
export function OfflinePanel({ userID, papers, hubPath }: { userID: string; papers: { paper: string; available: number }[]; hubPath: string }) {
  const { t, errorText } = useI18n()
  const paperName = usePaperName()
  const online = useOnline()
  const refresh = useRefresh()
  const { canInstall, showIOSHelp, install } = useInstall()
  const [packs, setPacks] = useState<OfflinePack[]>([])
  const [sittings, setSittings] = useState<OfflineSitting[]>([])
  const [busyPaper, setBusyPaper] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const load = useCallback(async () => {
    setPacks(await listPacks(userID))
    setSittings(await listSittings(userID))
  }, [userID])

  const sync = useCallback(async () => {
    const result = await syncOfflineSittings(userID)
    await load()
    if (result.uploaded > 0) {
      setNotice(t('offline.uploaded', { n: result.uploaded }))
      await refresh(hubPath, '/student/learning-profile')
    }
    if (result.refused > 0) setError(t('offline.refused', { n: result.refused }))
  }, [userID, load, refresh, hubPath, t])

  useEffect(() => { void load().catch((e) => setError(errorText(e, 'error.load'))) }, [load, errorText])
  useEffect(() => { if (online) void sync().catch(() => undefined) }, [online, sync])

  async function download(paper: string) {
    setBusyPaper(paper); setError(''); setNotice('')
    try {
      const pack = await api<{ paper: string; items: PackItem[] }>('/student/prep/packs', { method: 'POST', body: JSON.stringify({ paper, question_count: PACK_QUESTIONS }) })
      await savePack({ id: crypto.randomUUID(), user_id: userID, paper, downloaded_at: Date.now(), items: pack.items })
      // Ask the browser not to clear this data when the device runs low on space.
      void navigator.storage?.persist?.().catch(() => undefined)
      await load()
      setNotice(t('offline.downloaded', { n: pack.items.length, paper: paperName(paper) }))
    } catch (e) { setError(errorText(e, 'offline.downloadFailed')) } finally { setBusyPaper('') }
  }

  async function remove(pack: OfflinePack) {
    await deletePack(pack.id)
    await load()
  }

  const waiting = sittings.filter((sitting) => sitting.finished_at).length
  const statusOf = (pack: OfflinePack) => {
    const sitting = sittings.find((item) => item.pack_id === pack.id)
    if (!sitting) return 'new'
    return sitting.finished_at ? 'finished' : 'started'
  }

  return <section className="panel offline-panel">
    <div className="panel-head">
      <div><h2>{t('offline.title')}</h2><p>{t('offline.help')}</p></div>
      {canInstall && <button className="primary compact" onClick={() => void install()}>{t('offline.install')}</button>}
    </div>
    {showIOSHelp && <p className="form-hint">{t('offline.iosHelp')}</p>}
    {error && <p className="form-error" role="alert">{error}</p>}
    {notice && <p className="invite-success" role="status">{notice}</p>}
    {waiting > 0 && <p className="form-hint" role="status">{online ? t('offline.uploading', { n: waiting }) : t('offline.waiting', { n: waiting })}</p>}

    <div className="prep-papers">
      {packs.map((pack) => {
        const status = statusOf(pack)
        return <article className="prep-paper" key={pack.id}>
          <div><strong>{paperName(pack.paper)}</strong><small>{t('prep.available', { n: pack.items.length })} · {t(`offline.pack.${status}`)}</small></div>
          <div className="prep-paper-actions">
            <Link className="primary compact prep-link" to={`/learn/prep/offline/${pack.id}`}>{status === 'new' ? t('offline.start') : status === 'started' ? t('prep.resume') : t('offline.seeResult')}</Link>
            {status === 'new' && <button className="secondary compact" onClick={() => void remove(pack)}>{t('offline.remove')}</button>}
          </div>
        </article>
      })}
      {!packs.length && <p className="empty">{online && papers.length > 0 ? t('offline.empty') : t('offline.emptyOffline')}</p>}
    </div>

    {papers.length > 0 && <div className="offline-downloads">
      <h3>{t('offline.downloadTitle')}</h3>
      <p className="form-hint">{t('offline.downloadHelp', { n: PACK_QUESTIONS })}</p>
      <div className="prep-paper-actions">
        {papers.map((paper) => <button key={paper.paper} className="secondary compact" disabled={!online || !paper.available || Boolean(busyPaper)} onClick={() => void download(paper.paper)}>
          {busyPaper === paper.paper ? t('offline.downloading') : `↓ ${paperName(paper.paper)}`}
        </button>)}
      </div>
    </div>}
  </section>
}
