import { FormEvent, useEffect, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Link } from 'react-router'
import { useAction, useFeedback } from '../../app/feedback'
import { CopyLinkResult, passwordResetLink } from '../../features/account/account'
import { useI18n } from '../../i18n'
import { api, apiPage, apiURL, readAPIResponse } from '../../lib/api'
import { useClasses, useRefresh } from '../../lib/queries'
import { classLabel, Student } from '../../lib/types'

const PAGE_SIZE = 25
const TEMPLATE = 'student_number,full_name,grade,section,date_of_birth_ec,gender,stream,guardian_name,guardian_phone,guardian_relationship\nS-1001,Hanna Bekele,12,A,2001-03-15,female,natural,Almaz Tesfaye,0911234567,mother\n'

function downloadTemplate() {
  const link = document.createElement('a')
  link.href = URL.createObjectURL(new Blob([`﻿${TEMPLATE}`], { type: 'text/csv;charset=utf-8' }))
  link.download = 'students-template.csv'
  link.click()
  URL.revokeObjectURL(link.href)
}

export default function StudentsPage() {
  const { t, label } = useI18n()
  const { setNotice, setError } = useFeedback()
  const { busy, run, onSubmit } = useAction()
  const refresh = useRefresh()
  const classes = useClasses().data ?? []
  const [search, setSearch] = useState('')
  const [term, setTerm] = useState('')
  const [classID, setClassID] = useState('')
  const [page, setPage] = useState(1)
  const [inviteLinks, setInviteLinks] = useState<Record<string, string>>({})
  const [resetLink, setResetLink] = useState<{ name: string; link: string } | null>(null)
  const [rowErrors, setRowErrors] = useState<{ row: number; error: string }[]>([])
  const copied = (message: string) => () => setNotice(message)
  const copyFailed = () => setError(t('common.copyFailed'))

  // Search once typing pauses, so a slow connection is not sent every letter.
  useEffect(() => {
    const timer = window.setTimeout(() => { setTerm(search.trim()); setPage(1) }, 300)
    return () => window.clearTimeout(timer)
  }, [search])

  const query = useQuery({
    queryKey: ['/students', term, classID, page],
    queryFn: () => apiPage<Student[]>(`/students?page=${page}&page_size=${PAGE_SIZE}&q=${encodeURIComponent(term)}&class_id=${classID}`),
    placeholderData: keepPreviousData,
  })
  const students = query.data?.data ?? []
  const total = query.data?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const filtered = Boolean(term || classID)

  const addStudent = onSubmit(async (f) => {
    await api('/students', { method: 'POST', body: JSON.stringify({ student_number: f.get('student_number'), legal_name: f.get('legal_name'), preferred_language: f.get('preferred_language'), class_id: f.get('class_id') }) })
    await refresh('/students')
  }, t('students.added'))

  function importCSV(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    const file = values.get('file') as File | null
    setRowErrors([])
    void run(async () => {
      if (!file || file.size === 0) throw new Error(t('history.chooseCSV'))
      const response = await fetch(apiURL(`/students/import${values.get('allow_duplicates') === 'on' ? '?allow_duplicates=true' : ''}`), { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'text/csv' }, body: await file.text() })
      const body = await readAPIResponse(response)
      if (!response.ok) {
        setRowErrors(body?.error?.row_errors ?? [])
        throw new Error(body?.error?.row_errors ? t('students.importHasErrors') : (body?.error?.message ?? t('bank.importFailed')))
      }
      form.reset()
      await refresh('/students')
      setNotice(t('students.imported', { n: body.data.imported }))
    })
  }

  const emailed = { to: '' }
  const invite = (student: Student) => onSubmit(async (f) => {
    const result = await api<{ token: string; email: string; emailed?: boolean }>(`/students/${student.id}/invitation`, { method: 'POST', body: JSON.stringify({ email: String(f.get('email') || '') }) })
    emailed.to = result.emailed ? result.email : ''
    setInviteLinks((current) => ({ ...current, [student.id]: `${window.location.origin}/#student-invite=${encodeURIComponent(result.token)}` }))
  }, () => emailed.to ? t('notify.linkEmailed', { email: emailed.to }) : t('students.inviteCreated'))

  const resetPassword = (student: Student) => run(async () => {
    const result = await api<{ token: string; email: string; emailed?: boolean }>(`/students/${student.id}/password-reset`, { method: 'POST', body: '{}' })
    emailed.to = result.emailed ? result.email : ''
    setResetLink({ name: student.legal_name, link: passwordResetLink(result.token) })
  }, () => emailed.to ? t('notify.linkEmailed', { email: emailed.to }) : t('students.resetCreated'))

  return <section className="work-grid single-column"><div className="panel" id="students">
    <div className="panel-head"><div><h2>{t('admin.title.students')}</h2><p>{t('students.help')}</p></div><span className="count-pill">{t('students.total', { n: total })}</span></div>
    <form className="inline-form student-form" onSubmit={addStudent}>
      <input name="student_number" placeholder={t('students.id')} aria-label={t('students.id')} required />
      <input name="legal_name" placeholder={t('students.fullName')} aria-label={t('students.fullName')} required />
      <select name="class_id" aria-label={t('students.assignClass')}>
        <option value="">{t('students.noClass')}</option>
        {classes.map((c) => <option key={c.id} value={c.id}>{classLabel(c)}</option>)}
      </select>
      <button className="primary compact" disabled={busy}>{t('students.add')}</button>
    </form>
    <details className="student-import">
      <summary>{t('students.importTitle')}</summary>
      <p>{t('students.importHelp')}</p>
      <form className="csv-import" onSubmit={importCSV}>
        <input type="file" name="file" accept=".csv,text/csv" aria-label={t('students.importTitle')} required />
        <label className="check-row"><input type="checkbox" name="allow_duplicates" /> {t('students.allowDuplicates')}</label>
        <button className="secondary" disabled={busy}>{t('bank.import')}</button>
        <button type="button" className="secondary" onClick={downloadTemplate}>{t('bank.template')}</button>
      </form>
      {rowErrors.map((row) => <p className="row-error" key={row.row}>{t('history.rowError', { row: row.row })}: {row.error}</p>)}
    </details>
    {resetLink && <CopyLinkResult title={t('account.resetLinkFor', { name: resetLink.name })} detail={t('account.resetLinkDetail')} link={resetLink.link} onCopied={copied(t('account.resetLinkCopied'))} onCopyFailed={copyFailed} />}
    <div className="student-filters">
      <input type="search" value={search} onChange={(event) => setSearch(event.target.value)} placeholder={t('students.search')} aria-label={t('students.search')} />
      <select value={classID} aria-label={t('students.filterClass')} onChange={(event) => { setClassID(event.target.value); setPage(1) }}>
        <option value="">{t('students.allClasses')}</option>
        {classes.map((c) => <option key={c.id} value={c.id}>{classLabel(c)}</option>)}
      </select>
    </div>
    <div className="table-wrap"><table>
      <thead><tr><th>{t('common.col.student')}</th><th>{t('students.col.id')}</th><th>{t('attendance.col.class')}</th><th>{t('common.col.status')}</th><th>{t('students.col.login')}</th></tr></thead>
      <tbody>
        {students.map((s) => <tr key={s.id}>
          <td><div className="student-cell"><div className="student-avatar">{s.legal_name.slice(0, 1).toUpperCase()}</div><Link to={`/admin/students/${s.id}`}><strong>{s.legal_name}</strong></Link></div></td>
          <td>{s.student_number}</td>
          <td>{s.class_id ? classLabel({ grade_level: s.grade_level ?? '', name: s.class_name ?? '' }) : '—'}</td>
          <td><span className="status-pill">{label('status', s.status)}</span></td>
          <td>{s.has_account
            ? <div className="student-invite-cell">
                <span className="status-pill">{t('students.accountActive')}</span>
                <button className="secondary compact" disabled={busy} onClick={() => void resetPassword(s)}>{t('students.resetPassword')}</button>
              </div>
            : <div className="student-invite-cell">
                <form onSubmit={invite(s)}>
                  <input name="email" type="email" placeholder={t('students.learnerEmail')} aria-label={t('students.emailFor', { name: s.legal_name })} required />
                  <button className="secondary compact" disabled={busy || s.status !== 'active'}>{t('students.createInvite')}</button>
                </form>
                {inviteLinks[s.id] && <div className="student-invite-link">
                  <code>{inviteLinks[s.id]}</code>
                  <button onClick={() => void navigator.clipboard.writeText(inviteLinks[s.id]).then(copied(t('students.inviteCopied'))).catch(copyFailed)}>{t('common.copy')}</button>
                </div>}
              </div>}</td>
        </tr>)}
        {query.isSuccess && !students.length && <tr><td colSpan={5} className="empty">{filtered ? t('students.noMatches') : t('students.empty')}</td></tr>}
      </tbody>
    </table></div>
    {pages > 1 && <div className="pager">
      <button className="secondary compact" disabled={page <= 1} onClick={() => setPage(page - 1)}>← {t('students.previous')}</button>
      <span>{t('students.pageOf', { page, pages })}</span>
      <button className="secondary compact" disabled={page >= pages} onClick={() => setPage(page + 1)}>{t('students.next')} →</button>
    </div>}
  </div></section>
}
