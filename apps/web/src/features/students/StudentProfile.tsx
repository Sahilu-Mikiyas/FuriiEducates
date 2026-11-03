import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router'
import { useAction } from '../../app/feedback'
import { DateField } from '../../calendar/DateField'
import { useDates } from '../../calendar/useDates'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useClasses, useRefresh } from '../../lib/queries'
import { classLabel, Guardian, StudentProfileData } from '../../lib/types'
import { AttendanceSummary } from '../attendance/AttendanceSummary'

const RELATIONSHIPS = ['mother', 'father', 'guardian', 'other']

// StudentProfile is one student's record. Administrators can change it; a teacher
// of the student's class reads it, which is why medical notes are near the top.
export default function StudentProfile({ canEdit, base }: { canEdit: boolean; base: '/admin' | '/teach' }) {
  const { t, label, errorText } = useI18n()
  const { studentID = '' } = useParams()
  const path = `/students/${studentID}/profile`
  const query = useQuery({ queryKey: [path], queryFn: () => api<StudentProfileData>(path), meta: { silent: true } })
  const back = <Link className="profile-back" to={canEdit ? `${base}/students` : `${base}/class`}>← {t('profile.back')}</Link>
  if (query.error) return <section className="panel">{back}<p className="row-error">{errorText(query.error, 'error.load')}</p></section>
  if (!query.data) return <p className="empty">{t('common.loading')}</p>
  const { student, guardians, attendance } = query.data
  return <div className="operations-page">
    <section className="panel profile-card">
      {back}
      <div className="profile-head">
        <div className="student-avatar">{student.legal_name.slice(0, 1).toUpperCase()}</div>
        <div>
          <h2>{student.legal_name}</h2>
          <p>{student.student_number} · {student.class_id ? classLabel({ grade_level: student.grade_level, name: student.class_name }) : t('students.noClass')}</p>
        </div>
        <span className={`status-pill ${student.status === 'active' ? '' : 'draft'}`}>{label('status', student.status)}</span>
      </div>
      {student.medical_notes && <p className="profile-medical"><strong>{t('profile.medical')}:</strong> {student.medical_notes}</p>}
    </section>
    {canEdit ? <ProfileForm key={JSON.stringify(student)} path={path} student={student} /> : <ProfileDetails student={student} />}
    <Guardians studentID={studentID} guardians={guardians} canEdit={canEdit} path={path} />
    {canEdit && <MoveClass studentID={studentID} currentClass={student.class_id} path={path} />}
    <AttendanceSummary summary={attendance} />
  </div>
}

function ProfileDetails({ student }: { student: StudentProfileData['student'] }) {
  const { t, label } = useI18n()
  const { date } = useDates()
  return <section className="panel">
    <div className="panel-head"><div><h2>{t('profile.details')}</h2></div></div>
    <dl className="profile-facts">
      <div><dt>{t('profile.birth')}</dt><dd>{date(student.date_of_birth)}</dd></div>
      <div><dt>{t('profile.gender')}</dt><dd>{student.gender ? label('gender', student.gender) : '—'}</dd></div>
      <div><dt>{t('profile.address')}</dt><dd>{student.address || '—'}</dd></div>
    </dl>
  </section>
}

function ProfileForm({ path, student }: { path: string; student: StudentProfileData['student'] }) {
  const { t, label } = useI18n()
  const { busy, onSubmit } = useAction()
  const refresh = useRefresh()
  const save = onSubmit(async (f) => {
    await api(path, { method: 'PUT', body: JSON.stringify({
      legal_name: f.get('legal_name'), date_of_birth: f.get('date_of_birth'), gender: f.get('gender'),
      preferred_language: student.preferred_language, address: f.get('address'), medical_notes: f.get('medical_notes'), status: f.get('status'),
    }) })
    await refresh(path, '/students')
  }, t('profile.saved'), false)
  return <section className="panel">
    <div className="panel-head"><div><h2>{t('profile.details')}</h2></div></div>
    <form className="stack-form profile-form" onSubmit={save}>
      <label>{t('students.fullName')}<input name="legal_name" defaultValue={student.legal_name} maxLength={200} required /></label>
      <DateField name="date_of_birth" label={t('profile.birth')} defaultValue={student.date_of_birth} />
      <label>{t('profile.gender')}<select name="gender" defaultValue={student.gender}>
        <option value="">—</option>
        {['female', 'male'].map((value) => <option key={value} value={value}>{label('gender', value)}</option>)}
      </select></label>
      <label>{t('common.col.statusLabel')}<select name="status" defaultValue={student.status}>
        {['active', 'inactive'].map((value) => <option key={value} value={value}>{label('status', value)}</option>)}
      </select></label>
      <label className="profile-wide">{t('profile.address')}<input name="address" defaultValue={student.address} maxLength={500} /></label>
      <label className="profile-wide">{t('profile.medical')}<textarea name="medical_notes" defaultValue={student.medical_notes} maxLength={2000} rows={3} placeholder={t('profile.medicalHelp')} /></label>
      <button className="primary compact" disabled={busy}>{t('common.save')}</button>
    </form>
  </section>
}

function GuardianFields({ guardian }: { guardian?: Guardian }) {
  const { t, label } = useI18n()
  return <>
    <label>{t('guardian.name')}<input name="full_name" defaultValue={guardian?.full_name} maxLength={200} required /></label>
    <label>{t('guardian.relationship')}<select name="relationship" defaultValue={guardian?.relationship ?? 'mother'}>
      {RELATIONSHIPS.map((value) => <option key={value} value={value}>{label('guardian.relation', value)}</option>)}
    </select></label>
    <label>{t('guardian.phone')}<input name="phone" type="tel" defaultValue={guardian?.phone} placeholder="0911 234 567" required /></label>
    <label>{t('guardian.email')}<input name="email" type="email" defaultValue={guardian?.email} /></label>
  </>
}

const guardianBody = (f: FormData, isPrimary: boolean) => JSON.stringify({ full_name: f.get('full_name'), relationship: f.get('relationship'), phone: f.get('phone'), email: f.get('email'), is_primary: isPrimary })

function Guardians({ studentID, guardians, canEdit, path }: { studentID: string; guardians: Guardian[]; canEdit: boolean; path: string }) {
  const { t, label } = useI18n()
  const { busy, run, onSubmit } = useAction()
  const refresh = useRefresh()
  const [editing, setEditing] = useState('')
  const base = `/students/${studentID}/guardians`
  const refreshAll = () => refresh(path, '/attendance/overview', '/attendance/alerts')

  const add = onSubmit(async (f) => {
    await api(base, { method: 'POST', body: guardianBody(f, false) })
    await refreshAll()
  }, t('guardian.added'))
  const update = (guardian: Guardian) => onSubmit(async (f) => {
    await api(`${base}/${guardian.id}`, { method: 'PUT', body: guardianBody(f, guardian.is_primary) })
    await refreshAll()
    setEditing('')
  }, t('guardian.saved'), false)
  const makePrimary = (guardian: Guardian) => run(async () => {
    const { id, ...details } = guardian
    await api(`${base}/${id}`, { method: 'PUT', body: JSON.stringify({ ...details, is_primary: true }) })
    await refreshAll()
  }, t('guardian.saved'))
  const remove = (guardian: Guardian) => {
    if (!window.confirm(t('guardian.removeConfirm', { name: guardian.full_name }))) return
    void run(async () => {
      await api(`${base}/${guardian.id}`, { method: 'DELETE' })
      await refreshAll()
    }, t('guardian.removed'))
  }

  return <section className="panel">
    <div className="panel-head"><div><h2>{t('guardian.title')}</h2><p>{t('guardian.help')}</p></div></div>
    <ul className="guardian-list">
      {guardians.map((guardian) => <li key={guardian.id}>
        {editing === guardian.id
          ? <form className="stack-form profile-form" onSubmit={update(guardian)}>
              <GuardianFields guardian={guardian} />
              <div className="operation-actions">
                <button className="primary compact" disabled={busy}>{t('common.save')}</button>
                <button type="button" className="secondary compact" onClick={() => setEditing('')}>{t('common.cancel')}</button>
              </div>
            </form>
          : <>
              <div className="guardian-who">
                <strong>{guardian.full_name}</strong>
                <small>{label('guardian.relation', guardian.relationship)}{guardian.email && ` · ${guardian.email}`}</small>
                <a href={`tel:${guardian.phone.replace(/[^\d+]/g, '')}`}>{guardian.phone}</a>
              </div>
              {guardian.is_primary && <span className="status-pill">{t('guardian.primary')}</span>}
              {canEdit && <div className="operation-actions">
                {!guardian.is_primary && <button className="secondary compact" disabled={busy} onClick={() => void makePrimary(guardian)}>{t('guardian.makePrimary')}</button>}
                <button className="secondary compact" onClick={() => setEditing(guardian.id)}>{t('guardian.edit')}</button>
                <button className="secondary compact" disabled={busy} onClick={() => remove(guardian)}>{t('guardian.remove')}</button>
              </div>}
            </>}
      </li>)}
      {!guardians.length && <li className="empty">{t('guardian.empty')}</li>}
    </ul>
    {canEdit && <form className="stack-form profile-form guardian-add" onSubmit={add}>
      <h3 className="attendance-subhead profile-wide">{t('guardian.addTitle')}</h3>
      <GuardianFields />
      <button className="primary compact" disabled={busy}>{t('guardian.add')}</button>
    </form>}
  </section>
}

function MoveClass({ studentID, currentClass, path }: { studentID: string; currentClass: string; path: string }) {
  const { t } = useI18n()
  const { busy, onSubmit } = useAction()
  const refresh = useRefresh()
  const classes = useClasses().data ?? []
  const move = onSubmit(async (f) => {
    await api(`/students/${studentID}/move`, { method: 'POST', body: JSON.stringify({ class_id: f.get('class_id') }) })
    await refresh(path, '/students', '/attendance/overview', '/attendance/alerts')
  }, t('profile.moved'))
  return <section className="panel">
    <div className="panel-head"><div><h2>{t('profile.moveTitle')}</h2><p>{t('profile.moveHelp')}</p></div></div>
    <form className="inline-form" onSubmit={move}>
      <select name="class_id" aria-label={t('profile.moveTitle')} required defaultValue="">
        <option value="" disabled>{t('attendance.chooseClass')}</option>
        {classes.filter((c) => c.id !== currentClass).map((c) => <option key={c.id} value={c.id}>{classLabel(c)}</option>)}
      </select>
      <button className="secondary compact" disabled={busy}>{t('profile.move')}</button>
    </form>
  </section>
}
