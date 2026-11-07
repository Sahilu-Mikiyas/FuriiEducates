import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { useAction } from '../../app/feedback'
import { DateField } from '../../calendar/DateField'
import { useDates } from '../../calendar/useDates'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useRefresh, useYears } from '../../lib/queries'
import { classLabel } from '../../lib/types'
import type { PromotionAction, PromotionClass, PromotionPlan } from '../../lib/types'

const actions: PromotionAction[] = ['promote', 'repeat', 'graduate', 'leave']

// YearEndPage takes the school from one year to the next: copy the classes,
// move each student on, then close the old year.
export default function YearEndPage() {
  const { t } = useI18n()
  const { date } = useDates()
  const { busy, run, onSubmit } = useAction()
  const refresh = useRefresh()
  const years = useYears().data ?? []
  const open = years.filter((y) => !y.closed_at)
  const [fromID, setFromID] = useState('')
  const [toID, setToID] = useState('')
  const from = years.find((y) => y.id === fromID)
  const laterYears = years.filter((y) => from && y.id !== from.id && y.starts_on > from.starts_on && !y.closed_at)

  // Start from the oldest open year that still has students, and the year after it.
  useEffect(() => {
    if (fromID || !years.length) return
    const sorted = [...open].sort((a, b) => a.starts_on.localeCompare(b.starts_on))
    const start = sorted.find((y) => (y.active_students ?? 0) > 0) ?? sorted[0]
    if (start) setFromID(start.id)
  }, [years, fromID, open])
  useEffect(() => {
    if (!from) return
    if (!laterYears.some((y) => y.id === toID)) setToID([...laterYears].sort((a, b) => a.starts_on.localeCompare(b.starts_on))[0]?.id ?? '')
  }, [from, laterYears, toID])

  const planKey = `/academic-years/${fromID}/promotion?to=${toID}`
  const planQuery = useQuery({ queryKey: [planKey], queryFn: () => api<PromotionPlan>(planKey), enabled: Boolean(fromID && toID) })
  const plan = planQuery.data
  const [decisions, setDecisions] = useState<Record<string, PromotionAction>>({})
  const [targets, setTargets] = useState<Record<string, { promote: string; repeat: string }>>({})
  useEffect(() => {
    if (!plan) return
    setDecisions(Object.fromEntries(plan.classes.flatMap((c) => c.students.map((s) => [s.id, s.suggestion]))))
    setTargets(Object.fromEntries(plan.classes.map((c) => [c.id, { promote: c.promote_to, repeat: c.repeat_in }])))
  }, [plan])

  const refreshAll = () => Promise.all([refresh('/academic-years', '/classes', '/students', '/teacher/classes'), planQuery.refetch()])
  const createYear = onSubmit(async (f) => {
    const year = await api<{ id: string }>('/academic-years', { method: 'POST', body: JSON.stringify({ name: f.get('name'), starts_on: f.get('starts_on'), ends_on: f.get('ends_on') }) })
    await refresh('/academic-years')
    setToID(year.id)
  }, t('yearEnd.yearCreated'))

  const copied = { text: '' }
  const copy = onSubmit(async (f) => {
    const result = await api<Record<string, number>>(`/academic-years/${fromID}/copy`, { method: 'POST', body: JSON.stringify({ to_year_id: toID, teachers: f.get('teachers') === 'on', terms: f.get('terms') === 'on' }) })
    copied.text = t('yearEnd.copied', { classes: result.classes_created, kept: result.classes_kept, subjects: result.subjects_created, teachers: result.teachers_assigned, terms: result.terms_created })
    await refreshAll()
  }, () => copied.text, false)

  const createClass = (c: PromotionClass) => c.create_suggested && void run(async () => {
    await api('/classes', { method: 'POST', body: JSON.stringify({ ...c.create_suggested, academic_year_id: toID }) })
    await refreshAll()
  }, t('yearEnd.classCreated'))

  const chosen = useMemo(() => {
    if (!plan) return { list: [] as { student_id: string; action: PromotionAction; class_id: string }[], missing: 0, counts: {} as Record<PromotionAction, number> }
    const counts = { promote: 0, repeat: 0, graduate: 0, leave: 0 }
    let missing = 0
    const list = plan.classes.flatMap((c) => c.students.map((s) => {
      const action = decisions[s.id] ?? s.suggestion
      const classID = action === 'promote' ? targets[c.id]?.promote ?? '' : action === 'repeat' ? targets[c.id]?.repeat ?? '' : ''
      if ((action === 'promote' || action === 'repeat') && !classID) missing++
      counts[action]++
      return { student_id: s.id, action, class_id: classID }
    }))
    return { list, missing, counts }
  }, [plan, decisions, targets])

  const move = () => {
    if (!plan || !chosen.list.length || chosen.missing) return
    if (!window.confirm(t('yearEnd.confirmMove', { promote: chosen.counts.promote, repeat: chosen.counts.repeat, graduate: chosen.counts.graduate, leave: chosen.counts.leave, to: plan.to }))) return
    void run(async () => {
      const result = await api<Record<string, number>>(`/academic-years/${fromID}/promotion`, { method: 'POST', body: JSON.stringify({ to_year_id: toID, decisions: chosen.list }) })
      await refreshAll()
      return result
    }, t('yearEnd.moved'))
  }

  const close = (id: string, name: string) => {
    if (!window.confirm(t('yearEnd.confirmClose', { name }))) return
    void run(async () => { await api(`/academic-years/${id}/close`, { method: 'POST' }); await refreshAll() }, t('yearEnd.closed'))
  }
  const reopen = (id: string) => void run(async () => { await api(`/academic-years/${id}/close`, { method: 'DELETE' }); await refreshAll() }, t('yearEnd.reopened'))

  const result = (s: PromotionClass['students'][number]) => s.average === null
    ? t('yearEnd.noResult')
    : t('yearEnd.result', { average: s.average, pass: s.pass_percent ?? '—', term: s.result_from })
  const targetOptions = plan?.target_classes ?? []
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'Africa/Addis_Ababa' }).format(new Date())
  const nextStart = from ? `${Number(from.starts_on.slice(0, 4)) + 1}${from.starts_on.slice(4)}` : ''
  const nextEnd = from ? `${Number(from.ends_on.slice(0, 4)) + 1}${from.ends_on.slice(4)}` : ''

  return <>
    <section className="panel year-steps">
      <div className="panel-head"><div><h2>{t('yearEnd.years')}</h2><p>{t('yearEnd.yearsHelp')}</p></div></div>
      <div className="two-fields year-pick">
        <label>{t('yearEnd.from')}<select value={fromID} onChange={(e) => { setFromID(e.target.value); setToID('') }}>
          {open.map((y) => <option key={y.id} value={y.id}>{y.name} · {t('yearEnd.studentsIn', { count: y.active_students ?? 0 })}</option>)}
        </select></label>
        <label>{t('yearEnd.to')}<select value={toID} onChange={(e) => setToID(e.target.value)} disabled={!laterYears.length}>
          {!laterYears.length && <option value="">{t('yearEnd.noNextYear')}</option>}
          {laterYears.map((y) => <option key={y.id} value={y.id}>{y.name}</option>)}
        </select></label>
      </div>
      {from && !laterYears.length && <form className="stack-form next-year-form" onSubmit={createYear} key={from.id}>
        <p className="form-hint">{t('yearEnd.createNextHelp')}</p>
        <div className="three-fields">
          <label>{t('classes.yearLabel')}<input name="name" required maxLength={80} /></label>
          <DateField name="starts_on" label={t('classes.starts')} defaultValue={nextStart} required />
          <DateField name="ends_on" label={t('classes.ends')} defaultValue={nextEnd} required />
        </div>
        <button className="secondary" disabled={busy}>{t('yearEnd.createNext')}</button>
      </form>}
    </section>

    {fromID && toID && <section className="panel">
      <div className="panel-head"><div><p className="eyebrow">{t('yearEnd.step', { n: 1 })}</p><h2>{t('yearEnd.copy')}</h2><p>{t('yearEnd.copyHelp')}</p></div></div>
      <form className="year-copy" onSubmit={copy}>
        <label className="bank-check"><input type="checkbox" name="teachers" defaultChecked /> {t('yearEnd.copyTeachers')}</label>
        <label className="bank-check"><input type="checkbox" name="terms" defaultChecked /> {t('yearEnd.copyTerms')}</label>
        <button className="secondary" disabled={busy}>{t('yearEnd.copyButton')}</button>
      </form>
    </section>}

    {plan && <section className="panel">
      <div className="panel-head">
        <div><p className="eyebrow">{t('yearEnd.step', { n: 2 })}</p><h2>{t('yearEnd.move')}</h2><p>{t('yearEnd.moveHelp')}</p></div>
      </div>
      {!plan.classes.some((c) => c.students.length) && <p className="empty">{t('yearEnd.nobodyLeft')}</p>}
      {plan.classes.filter((c) => c.students.length).map((c) => <article key={c.id} className="promotion-class">
        <header>
          <div><h3>{classLabel(c)}</h3>{c.final_grade && <span className="status-pill">{t('yearEnd.topGrade')}</span>}</div>
          <div className="promotion-targets">
            {!c.final_grade && <label>{t('yearEnd.promoteTo')}<select value={targets[c.id]?.promote ?? ''} onChange={(e) => setTargets({ ...targets, [c.id]: { ...targets[c.id], promote: e.target.value } })}>
              <option value="">—</option>
              {targetOptions.map((o) => <option key={o.id} value={o.id}>{classLabel(o)}</option>)}
            </select></label>}
            <label>{t('yearEnd.repeatIn')}<select value={targets[c.id]?.repeat ?? ''} onChange={(e) => setTargets({ ...targets, [c.id]: { ...targets[c.id], repeat: e.target.value } })}>
              <option value="">—</option>
              {targetOptions.map((o) => <option key={o.id} value={o.id}>{classLabel(o)}</option>)}
            </select></label>
            {c.create_suggested && !targets[c.id]?.promote && <button type="button" className="secondary" disabled={busy} onClick={() => createClass(c)}>{t('yearEnd.createClass', { name: classLabel(c.create_suggested) })}</button>}
          </div>
        </header>
        <div className="table-wrap"><table>
          <thead><tr><th>{t('yearEnd.col.student')}</th><th>{t('yearEnd.col.result')}</th><th>{t('yearEnd.col.decision')}</th></tr></thead>
          <tbody>{c.students.map((s) => {
            const action = decisions[s.id] ?? s.suggestion
            return <tr key={s.id} className={action !== s.suggestion ? 'changed' : ''}>
              <td><strong>{s.name}</strong><small>{s.student_number}</small></td>
              <td className={s.average !== null && s.pass_percent !== null && s.average < s.pass_percent ? 'below-pass' : ''}>{result(s)}</td>
              <td><select aria-label={t('yearEnd.decisionFor', { name: s.name })} value={action} onChange={(e) => setDecisions({ ...decisions, [s.id]: e.target.value as PromotionAction })}>
                {actions.map((a) => <option key={a} value={a}>{t(`yearEnd.action.${a}`)}</option>)}
              </select>{action !== s.suggestion && <small>{t('yearEnd.suggested', { action: t(`yearEnd.action.${s.suggestion}`) })}</small>}</td>
            </tr>
          })}</tbody>
        </table></div>
      </article>)}
      {chosen.list.length > 0 && <div className="promotion-summary">
        <p>{t('yearEnd.summary', { promote: chosen.counts.promote, repeat: chosen.counts.repeat, graduate: chosen.counts.graduate, leave: chosen.counts.leave })}</p>
        {chosen.missing > 0 && <p className="form-error">{t('yearEnd.missingTarget', { count: chosen.missing })}</p>}
        <button className="primary" disabled={busy || chosen.missing > 0} onClick={move}>{t('yearEnd.moveButton', { count: chosen.list.length })}<span>→</span></button>
      </div>}
    </section>}

    <section className="panel">
      <div className="panel-head"><div><p className="eyebrow">{t('yearEnd.step', { n: 3 })}</p><h2>{t('yearEnd.close')}</h2><p>{t('yearEnd.closeHelp')}</p></div></div>
      <div className="mini-list year-close-list">
        {years.map((y) => <div key={y.id}>
          <span className="class-icon">{y.closed_at ? '■' : '□'}</span>
          <strong>{y.name}</strong>
          <small>{y.closed_at ? t('yearEnd.closedOn', { date: date(y.closed_at) }) : t('yearEnd.openLine', { classes: y.classes ?? 0, students: y.active_students ?? 0 })}</small>
          {y.closed_at
            ? <button className="link-button" disabled={busy} onClick={() => reopen(y.id)}>{t('yearEnd.reopen')}</button>
            : (y.id === fromID || y.ends_on <= today) && <button className="link-button" disabled={busy || (y.active_students ?? 0) > 0} title={(y.active_students ?? 0) > 0 ? t('yearEnd.moveFirst') : ''} onClick={() => close(y.id, y.name)}>{t('yearEnd.closeButton')}</button>}
        </div>)}
      </div>
    </section>
  </>
}
