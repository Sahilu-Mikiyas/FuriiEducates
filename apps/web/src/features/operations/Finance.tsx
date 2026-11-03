import { useQuery } from '@tanstack/react-query'
import { useAction } from '../../app/feedback'
import { DateField } from '../../calendar/DateField'
import { useDates } from '../../calendar/useDates'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useRefresh, useStaff, useStudents } from '../../lib/queries'
import type { FinanceEntry, FinanceSummary } from '../../lib/types'
import { money } from '../../lib/money'


export function FinanceWorkspace() {
  const { t, label } = useI18n()
  const { date } = useDates()
  const { busy, onSubmit } = useAction()
  const refresh = useRefresh()
  const students = useStudents().data ?? []
  const staff = useStaff().data ?? []
  const summary = useQuery({ queryKey: ['/finance/summary'], queryFn: () => api<FinanceSummary>('/finance/summary') }).data
  const entries = useQuery({ queryKey: ['/finance/entries'], queryFn: () => api<FinanceEntry[]>('/finance/entries') }).data ?? []

  const save = onSubmit(async (f) => {
    const amount = Math.round(Number(f.get('amount')) * 100)
    if (!Number.isSafeInteger(amount) || amount <= 0) throw new Error(t('finance.invalidAmount'))
    await api('/finance/entries', { method: 'POST', body: JSON.stringify({
      entry_type: f.get('entry_type'), category: f.get('category'), description: f.get('description'), amount_minor: amount,
      currency_code: f.get('currency_code'), student_id: f.get('student_id') || null, staff_user_id: f.get('staff_user_id') || null,
      due_on: f.get('due_on') || null, paid_on: f.get('paid_on') || null, status: f.get('status'), reference: f.get('reference'),
    }) })
    await refresh('/finance/summary', '/finance/entries')
  }, t('finance.saved'))

  const totals: [string, number | undefined][] = [
    [t('finance.tuitionBilled'), summary?.tuition_billed_minor],
    [t('finance.paymentsRecorded'), summary?.payments_recorded_minor],
    [t('finance.outstanding'), summary?.outstanding_charges_minor],
    [t('finance.expenses'), summary?.expenses_recorded_minor],
    [t('finance.salaries'), summary?.salaries_recorded_minor],
  ]

  return <div className="operations-page">
    <section className="metrics">
      {totals.map(([name, amount]) => <article className="metric" key={name}>
        <div className="metric-top"><span>{name}</span><i>ETB</i></div>
        <strong>{typeof amount === 'number' ? money(amount) : '—'}</strong>
        <small>{t('finance.ledgerTotals')}</small>
      </article>)}
    </section>
    <section className="panel">
      <div className="panel-head"><div><h2>{t('finance.recordTitle')}</h2><p>{t('finance.recordHelp')}</p></div></div>
      <form className="stack-form" onSubmit={save}>
        <div className="history-fields">
          <label>{t('finance.type')}<select name="entry_type">
            <option value="tuition_charge">{t('finance.type.tuition_charge')}</option>
            <option value="payment">{t('finance.type.payment')}</option>
            <option value="expense">{t('finance.type.expense')}</option>
            <option value="salary">{t('finance.type.salary')}</option>
          </select></label>
          <label>{t('finance.category')}<input name="category" placeholder={t('finance.categoryHint')} required maxLength={80} /></label>
          <label>{t('finance.description')}<input name="description" required maxLength={500} /></label>
          <label>{t('finance.amount')}<input name="amount" type="number" min="0.01" step="0.01" required /></label>
          <label>{t('finance.currency')}<select name="currency_code"><option>ETB</option><option>USD</option><option>EUR</option></select></label>
          <label>{t('finance.student')}<select name="student_id"><option value="">{t('common.notApplicable')}</option>{students.map((s) => <option key={s.id} value={s.id}>{s.legal_name} · {s.student_number}</option>)}</select></label>
          <label>{t('finance.staffMember')}<select name="staff_user_id"><option value="">{t('common.notApplicable')}</option>{staff.map((s) => <option key={s.id} value={s.id}>{s.display_name}</option>)}</select></label>
          <DateField name="due_on" label={t('finance.dueDate')} />
          <DateField name="paid_on" label={t('finance.paidDate')} />
          <label>{t('finance.status')}<select name="status"><option value="recorded">{t('finance.status.recorded')}</option><option value="paid">{t('finance.status.paid')}</option></select></label>
          <label>{t('finance.reference')}<input name="reference" maxLength={200} /></label>
        </div>
        <button className="primary compact" disabled={busy}>{busy ? t('common.saving') : t('finance.save')} →</button>
      </form>
    </section>
    <section className="panel">
      <div className="panel-head"><div><h2>{t('finance.ledger')}</h2><p>{t('finance.ledgerHelp')}</p></div><span className="count-pill">{t('finance.records', { n: entries.length })}</span></div>
      <div className="table-wrap"><table>
        <thead><tr><th>{t('finance.col.date')}</th><th>{t('finance.col.type')}</th><th>{t('finance.col.detail')}</th><th>{t('finance.col.person')}</th><th>{t('finance.col.amount')}</th><th>{t('finance.col.status')}</th></tr></thead>
        <tbody>
          {entries.map((e) => <tr key={e.id}>
            <td>{date(e.created_at)}</td>
            <td>{label('finance.type', e.entry_type)}</td>
            <td><strong>{e.category}</strong><small>{e.description}{e.reference ? ` · ${e.reference}` : ''}</small></td>
            <td>{e.student_name || e.staff_name || '—'}</td>
            <td>{money(e.amount_minor, e.currency_code)}</td>
            <td>{label('finance.status', e.status)}</td>
          </tr>)}
          {!entries.length && <tr><td colSpan={6} className="empty">{t('finance.empty')}</td></tr>}
        </tbody>
      </table></div>
    </section>
  </div>
}
