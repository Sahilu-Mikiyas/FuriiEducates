import { useQuery } from '@tanstack/react-query'
import { useAction } from '../../app/feedback'
import { useI18n } from '../../i18n'
import { api, apiURL } from '../../lib/api'
import type { Resource } from '../../lib/types'

export function useTeacherResources(classID: string) {
  const path = `/teacher/classes/${classID}/resources`
  return useQuery({ queryKey: [path], queryFn: () => api<Resource[]>(path), enabled: Boolean(classID) }).data ?? []
}

export function StudentResources() {
  const { t } = useI18n()
  const resources = useQuery({ queryKey: ['/student/resources'], queryFn: () => api<Resource[]>('/student/resources') }).data ?? []
  return <section className="panel student-resources">
    <div className="panel-head"><div><p className="eyebrow">{t('resources.eyebrow')}</p><h2>{t('resources.title')}</h2><p>{t('resources.help')}</p></div><span className="count-pill">{t('resources.files', { n: resources.length })}</span></div>
    <ResourceList resources={resources} />
  </section>
}

export function ResourceList({ resources, onRemoved }: { resources: Resource[]; onRemoved?: () => Promise<unknown> }) {
  const { t } = useI18n()
  const { busy, run } = useAction()
  const remove = (resource: Resource) => {
    if (!window.confirm(t('resources.removeConfirm', { title: resource.title }))) return
    void run(async () => {
      await api(`/resources/${resource.id}`, { method: 'DELETE' })
      await onRemoved?.()
    }, t('resources.removed'))
  }
  return <div className="resource-list">
    {resources.map((r) => <article className="operation-card" key={r.id}>
      <div>
        <strong>{r.title}</strong>
        <p>{r.subject || t('resources.classResource')} · {r.file_name} · {(r.file_size / 1024 / 1024).toFixed(1)} MB</p>
        {r.description && <p>{r.description}</p>}
      </div>
      <div className="operation-actions">
        <a className="secondary compact" href={apiURL(`/resources/${r.id}/download`)}>{t('resources.download')}</a>
        {onRemoved && r.can_delete && <button className="secondary compact" disabled={busy} onClick={() => remove(r)}>{t('guardian.remove')}</button>}
      </div>
    </article>)}
    {!resources.length && <p className="empty">{t('resources.empty')}</p>}
  </div>
}
