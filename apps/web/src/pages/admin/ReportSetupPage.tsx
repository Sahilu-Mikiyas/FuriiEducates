import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useAction } from '../../app/feedback'
import { DateField } from '../../calendar/DateField'
import { useDates } from '../../calendar/useDates'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useClasses, useRefresh, useStaff, useTerms, useYears } from '../../lib/queries'
import { ClassSubject, classLabel, GradeBand, ReportSettings, Term } from '../../lib/types'

// Setup for marks and reports: terms, then each class's subjects, then the grade bands.
export default function ReportSetupPage() {
  return <div className="operations-page">
    <TermsPanel />
    <AddSubjectsPanel />
    <ClassSubjectsPanel />
    <GradeBandsPanel />
  </div>
}

function TermsPanel() {
  const { t } = useI18n()
  const { date } = useDates()
  const { busy, run, onSubmit } = useAction()
  const refresh = useRefresh()
  const years = useYears().data ?? []
  const terms = useTerms().data ?? []
  const [editing, setEditing] = useState('')
  const add = onSubmit(async (f) => {
    await api('/terms', { method: 'POST', body: JSON.stringify({ academic_year_id: f.get('academic_year_id'), name: f.get('name'), starts_on: f.get('starts_on'), ends_on: f.get('ends_on') }) })
    await refresh('/terms')
  }, t('setup.termAdded'))
  const update = (term: Term) => onSubmit(async (f) => {
    await api(`/terms/${term.id}`, { method: 'PUT', body: JSON.stringify({ name: f.get('name'), starts_on: f.get('starts_on'), ends_on: f.get('ends_on') }) })
    await refresh('/terms')
    setEditing('')
  }, t('setup.termSaved'), false)
  const remove = (term: Term) => {
    if (!window.confirm(t('setup.termDeleteConfirm', { name: term.name }))) return
    void run(async () => {
      await api(`/terms/${term.id}`, { method: 'DELETE' })
      await refresh('/terms')
    }, t('setup.termDeleted'))
  }
  return <section className="panel">
    <div className="panel-head"><div><h2>{t('setup.termsTitle')}</h2><p>{t('setup.termsHelp')}</p></div></div>
    <ul className="guardian-list">
      {terms.map((term) => <li key={term.id}>
        {editing === term.id
          ? <form className="stack-form profile-form" onSubmit={update(term)}>
              <label className="profile-wide">{t('setup.termName')}<input name="name" defaultValue={term.name} maxLength={80} required /></label>
              <DateField name="starts_on" label={t('setup.termStarts')} defaultValue={term.starts_on} required />
              <DateField name="ends_on" label={t('setup.termEnds')} defaultValue={term.ends_on} required />
              <div className="operation-actions profile-wide">
                <button className="primary compact" disabled={busy}>{t('common.save')}</button>
                <button type="button" className="secondary compact" onClick={() => setEditing('')}>{t('common.cancel')}</button>
              </div>
            </form>
          : <>
              <div className="guardian-who"><strong>{term.name} · {term.academic_year_name}</strong><small>{date(term.starts_on)} – {date(term.ends_on)}</small></div>
              <div className="operation-actions">
                <button className="secondary compact" onClick={() => setEditing(term.id)}>{t('guardian.edit')}</button>
                {!term.in_use && <button className="secondary compact" disabled={busy} onClick={() => remove(term)}>{t('guardian.remove')}</button>}
              </div>
            </>}
      </li>)}
      {!terms.length && <li className="empty">{t('setup.termsEmpty')}</li>}
    </ul>
    <form className="stack-form profile-form guardian-add" onSubmit={add}>
      <h3 className="attendance-subhead profile-wide">{t('setup.addTerm')}</h3>
      <label>{t('setup.year')}<select name="academic_year_id" required>{years.map((year) => <option key={year.id} value={year.id}>{year.name}</option>)}</select></label>
      <label>{t('setup.termName')}<input name="name" maxLength={80} placeholder={t('setup.termNameHint')} required /></label>
      <DateField name="starts_on" label={t('setup.termStarts')} required />
      <DateField name="ends_on" label={t('setup.termEnds')} required />
      <button className="primary compact" disabled={busy}>{t('setup.addTerm')}</button>
    </form>
  </section>
}

function AddSubjectsPanel() {
  const { t } = useI18n()
  const { busy, onSubmit } = useAction()
  const refresh = useRefresh()
  const classes = useClasses().data ?? []
  const add = onSubmit(async (f) => {
    const names = String(f.get('names')).split(/[\n,]/).map((name) => name.trim()).filter(Boolean)
    const classIDs = f.getAll('class_ids')
    if (!classIDs.length) throw new Error(t('setup.chooseClasses'))
    await api('/class-subjects', { method: 'POST', body: JSON.stringify({ class_ids: classIDs, names }) })
    await refresh('/class-subjects')
  }, t('setup.subjectsAdded'))
  return <section className="panel">
    <div className="panel-head"><div><h2>{t('setup.addSubjectsTitle')}</h2><p>{t('setup.addSubjectsHelp')}</p></div></div>
    <form className="stack-form profile-form" onSubmit={add}>
      <fieldset className="mock-choices profile-wide">
        <legend>{t('setup.classes')}</legend>
        {classes.map((c) => <label key={c.id} className="check-row"><input type="checkbox" name="class_ids" value={c.id} /> {classLabel(c)}</label>)}
      </fieldset>
      <label className="profile-wide">{t('setup.subjectNames')}<textarea name="names" rows={4} placeholder={t('setup.subjectNamesHint')} required /></label>
      <button className="primary compact" disabled={busy}>{t('setup.addSubjects')}</button>
    </form>
  </section>
}

function ClassSubjectsPanel() {
  const { t } = useI18n()
  const { busy, run } = useAction()
  const refresh = useRefresh()
  const classes = useClasses().data ?? []
  const staff = useStaff().data ?? []
  const [picked, setPicked] = useState('')
  const classID = picked || classes[0]?.id || ''
  const subjects = useQuery({
    queryKey: ['/class-subjects', classID, ''],
    queryFn: () => api<ClassSubject[]>(`/classes/${classID}/subjects`),
    enabled: Boolean(classID),
  }).data ?? []
  const change = (subject: ClassSubject, next: Partial<{ name: string; teacher_id: string; counts_in_average: boolean }>) => run(async () => {
    await api(`/class-subjects/${subject.id}`, { method: 'PUT', body: JSON.stringify({ name: subject.name, teacher_id: subject.teacher_id, counts_in_average: subject.counts_in_average, ...next }) })
    await refresh('/class-subjects')
  }, t('setup.subjectSaved'))
  const remove = (subject: ClassSubject) => {
    if (!window.confirm(t('setup.subjectDeleteConfirm', { name: subject.name }))) return
    void run(async () => {
      await api(`/class-subjects/${subject.id}`, { method: 'DELETE' })
      await refresh('/class-subjects')
    }, t('setup.subjectDeleted'))
  }
  return <section className="panel">
    <div className="panel-head"><div><h2>{t('setup.classSubjectsTitle')}</h2><p>{t('setup.classSubjectsHelp')}</p></div></div>
    <select className="register-class" aria-label={t('attendance.chooseClass')} value={classID} onChange={(event) => setPicked(event.target.value)}>
      {classes.map((c) => <option key={c.id} value={c.id}>{classLabel(c)}</option>)}
    </select>
    <div className="table-wrap"><table>
      <thead><tr><th>{t('reports.subject').toUpperCase()}</th><th>{t('marks.col.teacher')}</th><th>{t('setup.col.inAverage')}</th><th /></tr></thead>
      <tbody>
        {subjects.map((subject) => <tr key={subject.id}>
          <td><strong>{subject.name}</strong></td>
          <td><select aria-label={t('setup.teacherFor', { name: subject.name })} value={subject.teacher_id} disabled={busy} onChange={(event) => void change(subject, { teacher_id: event.target.value })}>
            <option value="">{t('setup.anyTeacher')}</option>
            {staff.map((member) => <option key={member.id} value={member.id}>{member.display_name}</option>)}
          </select></td>
          <td><input type="checkbox" aria-label={t('setup.inAverageFor', { name: subject.name })} checked={subject.counts_in_average} disabled={busy} onChange={(event) => void change(subject, { counts_in_average: event.target.checked })} /></td>
          <td><button className="secondary compact" disabled={busy} onClick={() => remove(subject)}>{t('guardian.remove')}</button></td>
        </tr>)}
        {classID && !subjects.length && <tr><td colSpan={4} className="empty">{t('setup.subjectsEmpty')}</td></tr>}
      </tbody>
    </table></div>
  </section>
}

function GradeBandsPanel() {
  const { t } = useI18n()
  const { busy, run } = useAction()
  const refresh = useRefresh()
  const settings = useQuery({ queryKey: ['/report-settings'], queryFn: () => api<ReportSettings>('/report-settings') }).data
  const [bands, setBands] = useState<GradeBand[]>([])
  const [pass, setPass] = useState('')
  useEffect(() => {
    if (!settings) return
    setBands(settings.grade_bands)
    setPass(String(settings.pass_percent))
  }, [settings])
  const setBand = (index: number, next: Partial<GradeBand>) => setBands(bands.map((band, i) => (i === index ? { ...band, ...next } : band)))
  const save = () => run(async () => {
    await api('/report-settings', { method: 'PUT', body: JSON.stringify({ pass_percent: Number(pass), grade_bands: bands.map((band) => ({ label: band.label, min_percent: Number(band.min_percent) })) }) })
    await refresh('/report-settings', '/report-cards')
  }, t('setup.bandsSaved'))
  return <section className="panel">
    <div className="panel-head">
      <div><h2>{t('setup.bandsTitle')}</h2><p>{t('setup.bandsHelp')}</p></div>
      {settings && !settings.custom && <span className="status-pill draft">{t('setup.bandsDefault')}</span>}
    </div>
    <div className="grade-bands">
      <label>{t('setup.passMark')}<input type="number" min="0" max="100" value={pass} onChange={(event) => setPass(event.target.value)} /></label>
      {bands.map((band, index) => <div key={index} className="grade-band">
        <input aria-label={t('setup.bandName')} value={band.label} maxLength={12} placeholder={t('setup.bandName')} onChange={(event) => setBand(index, { label: event.target.value })} />
        <input aria-label={t('setup.bandMin')} type="number" min="0" max="100" value={band.min_percent} onChange={(event) => setBand(index, { min_percent: Number(event.target.value) })} />
        <button type="button" className="secondary compact" onClick={() => setBands(bands.filter((_, i) => i !== index))}>{t('guardian.remove')}</button>
      </div>)}
    </div>
    <div className="operation-actions">
      <button type="button" className="secondary compact" onClick={() => setBands([...bands, { label: '', min_percent: 0 }])}>{t('setup.addBand')}</button>
      <button type="button" className="primary compact" disabled={busy} onClick={() => void save()}>{t('setup.saveBands')}</button>
    </div>
  </section>
}
