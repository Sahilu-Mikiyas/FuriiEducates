import { useState } from 'react'
import { useAction, useFeedback } from '../../app/feedback'
import { CopyLinkResult, passwordResetLink } from '../../features/account/account'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useClasses, useRefresh, useStudents } from '../../lib/queries'
import { classLabel, Student } from '../../lib/types'

export default function StudentsPage() {
  const { t, label } = useI18n()
  const { setNotice, setError } = useFeedback()
  const { busy, run, onSubmit } = useAction()
  const refresh = useRefresh()
  const students = useStudents().data ?? []
  const classes = useClasses().data ?? []
  const [inviteLinks, setInviteLinks] = useState<Record<string, string>>({})
  const [resetLink, setResetLink] = useState<{ name: string; link: string } | null>(null)
  const copied = (message: string) => () => setNotice(message)
  const copyFailed = () => setError(t('common.copyFailed'))

  const addStudent = onSubmit(async (f) => {
    await api('/students', { method: 'POST', body: JSON.stringify({ student_number: f.get('student_number'), legal_name: f.get('legal_name'), preferred_language: f.get('preferred_language'), class_id: f.get('class_id') }) })
    await refresh('/students')
  }, t('students.added'))

  const invite = (student: Student) => onSubmit(async (f) => {
    const result = await api<{ token: string }>(`/students/${student.id}/invitation`, { method: 'POST', body: JSON.stringify({ email: String(f.get('email') || '') }) })
    setInviteLinks((current) => ({ ...current, [student.id]: `${window.location.origin}/#student-invite=${encodeURIComponent(result.token)}` }))
  }, t('students.inviteCreated'))

  const resetPassword = (student: Student) => run(async () => {
    const result = await api<{ token: string }>(`/students/${student.id}/password-reset`, { method: 'POST', body: '{}' })
    setResetLink({ name: student.legal_name, link: passwordResetLink(result.token) })
  }, t('students.resetCreated'))

  return <section className="work-grid single-column"><div className="panel" id="students">
    <div className="panel-head"><div><h2>{t('admin.title.students')}</h2><p>{t('students.help')}</p></div><span className="count-pill">{t('students.total', { n: students.length })}</span></div>
    <form className="inline-form student-form" onSubmit={addStudent}>
      <input name="student_number" placeholder={t('students.id')} aria-label={t('students.id')} required />
      <input name="legal_name" placeholder={t('students.fullName')} aria-label={t('students.fullName')} required />
      <select name="class_id" aria-label={t('students.assignClass')}>
        <option value="">{t('students.noClass')}</option>
        {classes.map((c) => <option key={c.id} value={c.id}>{classLabel(c)}</option>)}
      </select>
      <button className="primary compact" disabled={busy}>{t('students.add')}</button>
    </form>
    {resetLink && <CopyLinkResult title={t('account.resetLinkFor', { name: resetLink.name })} detail={t('account.resetLinkDetail')} link={resetLink.link} onCopied={copied(t('account.resetLinkCopied'))} onCopyFailed={copyFailed} />}
    <div className="table-wrap"><table>
      <thead><tr><th>{t('common.col.student')}</th><th>{t('students.col.id')}</th><th>{t('common.col.status')}</th><th>{t('students.col.login')}</th></tr></thead>
      <tbody>
        {students.map((s) => <tr key={s.id}>
          <td><div className="student-cell"><div className="student-avatar">{s.legal_name.slice(0, 1).toUpperCase()}</div><strong>{s.legal_name}</strong></div></td>
          <td>{s.student_number}</td>
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
        {!students.length && <tr><td colSpan={4} className="empty">{t('students.empty')}</td></tr>}
      </tbody>
    </table></div>
  </div></section>
}
