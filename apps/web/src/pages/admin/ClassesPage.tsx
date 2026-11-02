import { useAction } from '../../app/feedback'
import { DateField } from '../../calendar/DateField'
import { useDates } from '../../calendar/useDates'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useClasses, useRefresh, useYears } from '../../lib/queries'
import { classLabel } from '../../lib/types'

export default function ClassesPage() {
  const { t } = useI18n()
  const { date } = useDates()
  const { busy, onSubmit } = useAction()
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
        {classes.map((c) => <div key={c.id}><span className="class-icon">▤</span><strong>{classLabel(c)}</strong><small>{c.academic_year_name ?? years.find((y) => y.id === c.academic_year_id)?.name}</small></div>)}
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
        {years.map((y) => <div key={y.id}><span className="class-icon">◷</span><strong>{y.name}</strong><small>{date(y.starts_on)} – {date(y.ends_on)}</small></div>)}
      </div>}
    </section>
  </div></section>
}
