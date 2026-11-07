import { useState } from 'react'
import { useAction, useFeedback } from '../../app/feedback'
import { useDates } from '../../calendar/useDates'
import { CopyLinkResult, passwordResetLink } from '../../features/account/account'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useAllStaff, useClasses, useRefresh, useStaffInvitations } from '../../lib/queries'
import { staffRoleOptions } from '../../lib/roles'
import { classLabel, StaffMember } from '../../lib/types'

export default function StaffPage() {
  const { t, label } = useI18n()
  const { date } = useDates()
  const { setNotice, setError } = useFeedback()
  const { busy, run, onSubmit } = useAction()
  const refresh = useRefresh()
  const staff = useAllStaff().data ?? []
  const invitations = useStaffInvitations().data ?? []
  const classes = useClasses().data ?? []
  const [inviteLink, setInviteLink] = useState('')
  const [resetLink, setResetLink] = useState<{ name: string; link: string } | null>(null)
  const copyFailed = () => setError(t('common.copyFailed'))

  const emailed = { to: '' }
  const invite = onSubmit(async (f) => {
    const result = await api<{ id: string; token: string; email: string; emailed?: boolean }>('/staff/invitations', { method: 'POST', body: JSON.stringify({ email: f.get('email'), role: f.get('role') }) })
    emailed.to = result.emailed ? result.email : ''
    setInviteLink(`${window.location.origin}/#invite=${encodeURIComponent(result.token)}`)
    await refresh('/staff/invitations')
  }, () => emailed.to ? t('notify.linkEmailed', { email: emailed.to }) : t('staff.inviteCreated'))

  const assign = (teacher: StaffMember) => onSubmit(async (f) => {
    const classID = String(f.get('class_id') || '')
    if (!classID) throw new Error(t('common.chooseClass'))
    await api(`/classes/${classID}/teachers`, { method: 'POST', body: JSON.stringify({ teacher_id: teacher.id }) })
    await refresh('/staff')
  }, t('staff.assigned'))

  const unassign = (teacher: StaffMember, classID: string, name: string) => {
    if (!window.confirm(t('staff.unassignConfirm', { name: teacher.display_name, className: name }))) return
    void run(async () => {
      await api(`/classes/${classID}/teachers/${teacher.id}`, { method: 'DELETE' })
      await refresh('/staff', '/class-subjects')
    }, t('staff.unassigned'))
  }

  const reset = (teacher: StaffMember) => run(async () => {
    const result = await api<{ token: string; email: string; emailed?: boolean }>(`/staff/${teacher.id}/password-reset`, { method: 'POST', body: '{}' })
    emailed.to = result.emailed ? result.email : ''
    setResetLink({ name: teacher.display_name, link: passwordResetLink(result.token) })
  }, () => emailed.to ? t('notify.linkEmailed', { email: emailed.to }) : t('staff.resetCreated'))

  const toggle = (teacher: StaffMember) => run(async () => {
    await api(`/staff/${teacher.id}`, { method: 'PATCH', body: JSON.stringify({ status: teacher.status === 'active' ? 'disabled' : 'active' }) })
    await refresh('/staff')
  }, teacher.status === 'active' ? t('staff.disabled') : t('staff.enabled'))

  const saveRoles = (member: StaffMember) => onSubmit(async (f) => {
    const roles = f.getAll('roles')
    if (!roles.length) throw new Error(t('staff.chooseRole'))
    await api(`/staff/${member.id}/roles`, { method: 'PUT', body: JSON.stringify({ roles }) })
    await refresh('/staff', '/classes')
  }, t('staff.rolesSaved'), false)

  const setHomeroom = (member: StaffMember, classID: string) => run(async () => {
    const isHomeroom = member.homeroom_class_ids.includes(classID)
    await api(`/classes/${classID}/homeroom`, { method: 'PUT', body: JSON.stringify({ teacher_id: isHomeroom ? '' : member.id }) })
    await refresh('/staff', '/classes')
  }, t('staff.homeroomSaved'))

  const revoke = (id: string) => run(async () => {
    await api(`/staff/invitations/${id}/revoke`, { method: 'POST', body: '{}' })
    await refresh('/staff/invitations')
  }, t('staff.revoked'))

  return <section className="panel staff-panel" id="staff">
    <div className="panel-head"><div><p className="eyebrow">{t('staff.eyebrow')}</p><h2>{t('staff.heading')}</h2><p>{t('admin.desc.staff')}</p></div><span className="settings-mark">♙</span></div>
    <form className="inline-form staff-invite-form" onSubmit={invite}>
      <input name="email" type="email" placeholder={t('staff.emailHint')} aria-label={t('staff.email')} required />
      <select name="role" aria-label={t('staff.inviteAs')} defaultValue="teacher">
        {['teacher', 'registrar', 'finance_officer'].map((role) => <option key={role} value={role}>{t(`role.${role}` as 'role.teacher')}</option>)}
      </select>
      <button className="primary compact" disabled={busy}>{t('staff.createInvite')}</button>
    </form>
    {resetLink && <CopyLinkResult title={t('account.resetLinkFor', { name: resetLink.name })} detail={t('account.resetLinkDetail')} link={resetLink.link} onCopied={() => setNotice(t('account.resetLinkCopied'))} onCopyFailed={copyFailed} />}
    {inviteLink && <CopyLinkResult title={t('staff.inviteLinkTitle')} detail={t('staff.inviteLinkDetail')} link={inviteLink} onCopied={() => setNotice(t('staff.inviteCopied'))} onCopyFailed={copyFailed} />}
    <div className="staff-grid">
      <div>
        <h3>{t('staff.people')} <span>{staff.length}</span></h3>
        <div className="staff-list">
          {staff.map((member) => {
            const available = classes.filter((c) => !member.class_ids.includes(c.id))
            return <article key={member.id}>
              <div className="staff-person">
                <strong>{member.display_name}</strong>
                <small>{member.email}</small>
                <span className={`status-pill ${member.status === 'active' ? '' : 'disabled-status'}`}>{label('status', member.status)}</span>
                <span className="role-chips">{member.roles.map((role) => <span key={role}>{t(`role.${role}` as 'role.teacher')}</span>)}</span>
              </div>
              {member.roles.includes('teacher') ? <form className="staff-assign" onSubmit={assign(member)}>
                <select name="class_id" aria-label={t('staff.assignTo', { name: member.display_name })} defaultValue="">
                  <option value="">{t('staff.assignClass')}</option>
                  {available.map((c) => <option key={c.id} value={c.id}>{classLabel(c)}</option>)}
                </select>
                <button className="secondary compact" disabled={busy || !available.length}>{t('common.assign')}</button>
              </form> : <span />}
              {member.roles.includes('teacher') && <div className="assigned-classes">
                {member.class_ids.map((id) => {
                  const classroom = classes.find((c) => c.id === id)
                  const homeroom = member.homeroom_class_ids.includes(id)
                  return classroom ? <span key={id} className={homeroom ? 'homeroom' : ''}>
                    <button type="button" className="chip-star" disabled={busy} aria-pressed={homeroom} title={homeroom ? t('staff.homeroomOf') : t('staff.makeHomeroom')}
                      aria-label={t(homeroom ? 'staff.removeHomeroom' : 'staff.setHomeroom', { name: member.display_name, className: classLabel(classroom) })} onClick={() => void setHomeroom(member, id)}>{homeroom ? '★' : '☆'}</button>
                    {classLabel(classroom)} <button type="button" className="chip-remove" disabled={busy} aria-label={t('staff.unassignFrom', { name: member.display_name, className: classLabel(classroom) })} onClick={() => unassign(member, id, classLabel(classroom))}>×</button></span> : null
                })}
                {!member.class_ids.length && <small>{t('staff.noClasses')}</small>}
              </div>}
              <details className="role-editor">
                <summary>{t('staff.changeRoles')}</summary>
                <form onSubmit={saveRoles(member)}>
                  {staffRoleOptions.map((role) => <label key={role} className="bank-check"><input type="checkbox" name="roles" value={role} defaultChecked={member.roles.includes(role)} /> {t(`role.${role}` as 'role.teacher')}</label>)}
                  <button className="secondary compact" disabled={busy}>{t('staff.saveRoles')}</button>
                </form>
              </details>
              {member.status === 'active' && <button className="staff-toggle" disabled={busy} onClick={() => void reset(member)}>{t('staff.resetLink')}</button>}
              <button className="staff-toggle" disabled={busy} onClick={() => void toggle(member)}>{member.status === 'active' ? t('staff.disable') : t('staff.enable')}</button>
            </article>
          })}
          {!staff.length && <p className="empty">{t('staff.empty')}</p>}
        </div>
      </div>
      <div>
        <h3>{t('staff.pending')} <span>{invitations.length}</span></h3>
        <div className="staff-list">
          {invitations.map((item) => <article key={item.id} className="pending-invite">
            <div className="staff-person"><strong>{item.email}</strong><small>{t(`role.${item.role}` as 'role.teacher')} · {t('staff.expires', { date: date(item.expires_at) })}</small></div>
            <button className="staff-toggle" disabled={busy} onClick={() => void revoke(item.id)}>{t('staff.revoke')}</button>
          </article>)}
          {!invitations.length && <p className="empty">{t('staff.pendingEmpty')}</p>}
        </div>
      </div>
    </div>
  </section>
}
