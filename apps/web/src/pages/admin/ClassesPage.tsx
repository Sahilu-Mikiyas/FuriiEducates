import { useState } from 'react'
import { useAction } from '../../app/feedback'
import { DateField } from '../../calendar/DateField'
import { useDates } from '../../calendar/useDates'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useClasses, useRefresh, useYears } from '../../lib/queries'
import { ClassRoom, classLabel, Year } from '../../lib/types'

export default function ClassesPage() {
  const { t } = useI18n()
  const { date } = useDates()
  const { busy, run, onSubmit } = useAction()
  const [editing, setEditing] = useState('')
  const refresh = useRefresh()
  const classes = useClasses().data ?? []
  const years = useYears().data ?? []

  const addClass = onSubmit(async (f) => {
    await api('/classes', { method: 'POST', body: JSON.stringify({ name: f.get('name'), grade_level: f.get('grade_level'), academic_year_id: f.get('academic_year_id') }) })
    await refresh('/classes')
  }, t('classes.added'))

  const addYear = onSubmit(async (f) => {
    await api('/academic-years', { method: 'POST', body: JSON.stringify({ name: f.get('name'), starts_on: f.get('starts_on'), ends_on: f.get('ends_on') }) })
    await refresh('/academic-years')
  }, t('classes.yearAdded'))

  const saveClass = (c: ClassRoom) => onSubmit(async (f) => {
    await api(`/classes/${c.id}`, { method: 'PATCH', body: JSON.stringify({ name: f.get('name'), grade_level: f.get('grade_level') }) })
    await refresh('/classes', '/teacher/classes')
    setEditing('')
  }, t('classes.saved'), false)
  const removeClass = (c: ClassRoom) => {
    if (!window.confirm(t('classes.deleteConfirm', { name: classLabel(c) }))) return
    void run(async () => {
      await api(`/classes/${c.id}`, { method: 'DELETE' })
      await refresh('/classes')
    }, t('classes.deleted'))
  }
  const saveYear = (y: Year) => onSubmit(async (f) => {
    await api(`/academic-years/${y.id}`, { method: 'PATCH', body: JSON.stringify({ name: f.get('name'), starts_on: f.get('starts_on'), ends_on: f.get('ends_on') }) })
    await refresh('/academic-years', '/classes')
    setEditing('')
  }, t('classes.yearSaved'), false)
  const removeYear = (y: Year) => {
    if (!window.confirm(t('classes.yearDeleteConfirm', { name: y.name }))) return
    void run(async () => {
      await api(`/academic-years/${y.id}`, { method: 'DELETE' })
      await refresh('/academic-years')
    }, t('classes.yearDeleted'))
  }

  return <section className="work-grid single-column"><div className="right-stack">
    <section className="panel" id="classes">
      <div className="panel-head"><div><h2>{t('classes.title')}</h2><p>{t('classes.help')}</p></div></div>
      <form className="stack-form" onSubmit={addClass}>
        <label>{t('classes.name')}<input name="name" placeholder={t('classes.nameHint')} required /></label>
        <div className="two-fields">
          <label>{t('classes.grade')}<input name="grade_level" placeholder={t('classes.gradeHint')} required /></label>
          <label>{t('classes.year')}<select name="academic_year_id" required>
            <option value="">{t('classes.chooseYear')}</option>
            {years.map((y) => <option key={y.id} value={y.id}>{y.name}</option>)}
          </select></label>
        </div>
        <button className="secondary" disabled={busy || !years.length}>{years.length ? t('classes.create') : t('classes.addYearFirst')} <span>→</span></button>
      </form>
      {classes.length > 0 && <div className="mini-list">
        {classes.map((c) => editing === c.id
          ? <form key={c.id} className="inline-form record-edit" onSubmit={saveClass(c)}>
              <input name="name" defaultValue={c.name} aria-label={t('classes.name')} maxLength={100} required />
              <input name="grade_level" defaultValue={c.grade_level} aria-label={t('classes.grade')} maxLength={40} required />
              <button className="primary compact" disabled={busy}>{t('common.save')}</button>
              <button type="button" className="secondary compact" onClick={() => setEditing('')}>{t('common.cancel')}</button>
            </form>
          : <div key={c.id}><span className="class-icon">▤</span><strong>{classLabel(c)}</strong><small>{c.academic_year_name ?? years.find((y) => y.id === c.academic_year_id)?.name}</small>
              <span className="record-actions">
                <button type="button" className="secondary compact" onClick={() => setEditing(c.id)}>{t('guardian.edit')}</button>
                <button type="button" className="secondary compact" disabled={busy} onClick={() => removeClass(c)}>{t('guardian.remove')}</button>
              </span>
            </div>)}
      </div>}
    </section>
    <section className="panel" id="school">
      <div className="panel-head"><div><h2>{t('classes.yearTitle')}</h2><p>{t('classes.yearHelp')}</p></div><span className="year-icon">◷</span></div>
      <form className="stack-form year-form" onSubmit={addYear}>
        <label>{t('classes.yearLabel')}<input name="name" placeholder={t('classes.yearLabelHint')} required /></label>
        <div className="two-fields">
          <DateField name="starts_on" label={t('classes.starts')} required />
          <DateField name="ends_on" label={t('classes.ends')} required />
        </div>
        <button className="secondary" disabled={busy}>{t('classes.addYear')} <span>→</span></button>
      </form>
      {years.length > 0 && <div className="mini-list">
        {years.map((y) => editing === y.id
          ? <form key={y.id} className="stack-form record-edit" onSubmit={saveYear(y)}>
              <label>{t('classes.yearLabel')}<input name="name" defaultValue={y.name} maxLength={100} required /></label>
              <div className="two-fields">
                <DateField name="starts_on" label={t('classes.starts')} defaultValue={y.starts_on} required />
                <DateField name="ends_on" label={t('classes.ends')} defaultValue={y.ends_on} required />
              </div>
              <div className="operation-actions">
                <button className="primary compact" disabled={busy}>{t('common.save')}</button>
                <button type="button" className="secondary compact" onClick={() => setEditing('')}>{t('common.cancel')}</button>
              </div>
            </form>
          : <div key={y.id}><span className="class-icon">◷</span><strong>{y.name}</strong><small>{date(y.starts_on)} – {date(y.ends_on)}</small>
              <span className="record-actions">
                <button type="button" className="secondary compact" onClick={() => setEditing(y.id)}>{t('guardian.edit')}</button>
                <button type="button" className="secondary compact" disabled={busy} onClick={() => removeYear(y)}>{t('guardian.remove')}</button>
              </span>
            </div>)}
      </div>}
    </section>
  </div></section>
}
