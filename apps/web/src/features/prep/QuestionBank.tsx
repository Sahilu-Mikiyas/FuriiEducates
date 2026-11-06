import { FormEvent, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useAction, useFeedback } from '../../app/feedback'
import { useSession } from '../../app/session'
import { useI18n } from '../../i18n'
import { apiURL, readAPIResponse, api } from '../../lib/api'
import { useRefresh } from '../../lib/queries'
import type { PrepBankRow, PrepQuestion } from '../../lib/types'
import { allPapers, usePaperName } from './readiness'

const BANK_PATH = '/prep/bank'
const TEMPLATE = 'paper,grade,topic,question,option_a,option_b,option_c,option_d,correct,explanation,difficulty,source,license,exam_only\nmathematics,12,Fractions,What is 1/2 + 1/4?,3/4,2/6,1/8,1,A,Use a common denominator: 2/4 + 1/4 = 3/4.,2,Written by our teachers,School-owned,no\n'

function downloadTemplate() {
  const link = document.createElement('a')
  link.href = URL.createObjectURL(new Blob([`﻿${TEMPLATE}`], { type: 'text/csv;charset=utf-8' }))
  link.download = 'exam-questions-template.csv'
  link.click()
  URL.revokeObjectURL(link.href)
}

export function usePrepBank() {
  return useQuery({ queryKey: [BANK_PATH], queryFn: () => api<PrepBankRow[]>(BANK_PATH) }).data ?? []
}

// QuestionBank is where teachers write exam questions and administrators approve
// them. A question reaches students only after approval.
export function QuestionBank() {
  const { t, label } = useI18n()
  const { identity } = useSession()
  const paperName = usePaperName()
  const refresh = useRefresh()
  const { setError, setNotice } = useFeedback()
  const { busy, run, onSubmit } = useAction()
  const isAdmin = identity?.roles.includes('school_admin') ?? false
  const [status, setStatus] = useState<'draft' | 'published'>('draft')
  const [paper, setPaper] = useState('')
  const [rowErrors, setRowErrors] = useState<{ row: number; error: string }[]>([])
  const listPath = `/prep/questions?status=${status}&paper=${paper}`
  const questions = useQuery({ queryKey: ['/prep/questions', status, paper], queryFn: () => api<PrepQuestion[]>(listPath) }).data ?? []
  const bank = usePrepBank()
  const refreshAll = async () => {
    await refresh(BANK_PATH)
    await refresh('/prep/questions')
  }

  const save = onSubmit(async (f) => {
    await api('/prep/questions', { method: 'POST', body: JSON.stringify({
      paper: f.get('paper'), grade: Number(f.get('grade')), topic: f.get('topic'), prompt: f.get('prompt'),
      options: [f.get('option_a'), f.get('option_b'), f.get('option_c'), f.get('option_d')],
      correct: f.get('correct'), explanation: f.get('explanation'), difficulty: Number(f.get('difficulty')),
      source_name: f.get('source_name'), license_notes: f.get('license_notes'), exam_only: f.get('exam_only') === 'on',
    }) })
    await refreshAll()
  }, t('bank.saved'))

  function importCSV(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const file = new FormData(form).get('file') as File | null
    setRowErrors([])
    void run(async () => {
      if (!file || file.size === 0) throw new Error(t('history.chooseCSV'))
      const response = await fetch(apiURL('/prep/questions/import'), { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'text/csv' }, body: await file.text() })
      const body = await readAPIResponse(response)
      if (!response.ok) {
        setRowErrors(body?.error?.row_errors ?? [])
        throw new Error(body?.error?.message ?? t('bank.importFailed'))
      }
      form.reset()
      await refreshAll()
      setNotice(t('bank.imported', { n: body.data.imported }))
    })
  }

  const approve = (ids: string[]) => run(async () => {
    const result = await api<{ approved: number }>('/prep/questions/approve', { method: 'POST', body: JSON.stringify({ question_ids: ids }) })
    await refreshAll()
    setNotice(t('bank.approved', { n: result.approved }))
  })
  const archive = (id: string) => {
    if (!window.confirm(t('questions.archiveConfirm'))) return Promise.resolve()
    return run(async () => {
      await api(`/questions/${id}/archive`, { method: 'POST', body: '{}' })
      await refreshAll()
    }, t('questions.archived'))
  }
  const reject = (id: string) => run(async () => {
    await api(`/prep/questions/${id}/reject`, { method: 'POST', body: '{}' })
    await refreshAll()
  }, t('bank.rejected'))

  return <div className="operations-page">
    <section className="panel">
      <div className="panel-head"><div><p className="eyebrow">{t('bank.eyebrow')}</p><h2>{t('bank.statusTitle')}</h2><p>{t('bank.statusHelp')}</p></div></div>
      <div className="table-wrap"><table>
        <thead><tr><th>{t('bank.col.paper')}</th><th>{t('bank.col.practice')}</th><th>{t('bank.col.examOnly')}</th><th>{t('bank.col.drafts')}</th></tr></thead>
        <tbody>{bank.map((row) => <tr key={row.paper}><td><strong>{paperName(row.paper)}</strong></td><td>{row.practice}</td><td>{row.exam_only}</td><td>{row.drafts}</td></tr>)}</tbody>
      </table></div>
    </section>

    <section className="panel">
      <div className="panel-head"><div><h2>{t('bank.writeTitle')}</h2><p>{t('bank.writeHelp')}</p></div></div>
      <form className="stack-form" onSubmit={save}>
        <div className="history-fields bank-fields">
          <label>{t('bank.paper')}<select name="paper" required defaultValue="">
            <option value="" disabled>{t('bank.choosePaper')}</option>
            {allPapers.map((value) => <option key={value} value={value}>{paperName(value)}</option>)}
          </select></label>
          <label>{t('classes.grade')}<select name="grade" defaultValue="12">{[9, 10, 11, 12].map((grade) => <option key={grade} value={grade}>{grade}</option>)}</select></label>
          <label>{t('plans.topic')}<input name="topic" required maxLength={200} placeholder={t('bank.topicHint')} /></label>
          <label className="span-all">{t('bank.question')}<textarea name="prompt" required rows={3} maxLength={5000} /></label>
          {['a', 'b', 'c', 'd'].map((letter) => <label key={letter}>{t('bank.option', { letter: letter.toUpperCase() })}<input name={`option_${letter}`} required /></label>)}
          <label>{t('bank.correct')}<select name="correct" required defaultValue="">
            <option value="" disabled>{t('bank.chooseCorrect')}</option>
            {['A', 'B', 'C', 'D'].map((letter) => <option key={letter}>{letter}</option>)}
          </select></label>
          <label>{t('questions.difficulty')}<select name="difficulty" defaultValue="3">{[1, 2, 3, 4, 5].map((level) => <option key={level} value={level}>{level}</option>)}</select></label>
          <label className="span-all">{t('questions.explanationLabel')}<textarea name="explanation" rows={2} maxLength={5000} placeholder={t('bank.explanationHint')} /></label>
          <label>{t('questions.source')}<input name="source_name" required placeholder={t('bank.sourceHint')} /></label>
          <label>{t('curriculum.license')}<input name="license_notes" required placeholder={t('bank.licenseHint')} /></label>
        </div>
        <label className="bank-check"><input type="checkbox" name="exam_only" /> {t('bank.examOnly')}</label>
        <p className="form-hint">{t('bank.rightsNote')}</p>
        <button className="primary compact" disabled={busy}>{t('bank.save')} →</button>
      </form>
    </section>

    <section className="panel">
      <div className="panel-head"><div><h2>{t('bank.importTitle')}</h2><p>{t('bank.importHelp')}</p></div><button className="secondary compact" onClick={downloadTemplate}>{t('bank.template')}</button></div>
      <form className="csv-import" onSubmit={importCSV}>
        <input type="file" name="file" accept=".csv,text/csv" aria-label={t('bank.importTitle')} required />
        <button className="secondary" disabled={busy}>{t('bank.import')}</button>
      </form>
      {rowErrors.map((row) => <p className="row-error" key={row.row}>{t('history.rowError', { row: row.row })}: {row.error}</p>)}
    </section>

    <section className="panel">
      <div className="panel-head">
        <div><h2>{isAdmin ? t('bank.reviewTitle') : t('bank.listTitle')}</h2><p>{isAdmin ? t('bank.reviewHelp') : t('bank.listHelp')}</p></div>
        {isAdmin && status === 'draft' && questions.length > 0 && <button className="primary compact" disabled={busy} onClick={() => void approve(questions.map((q) => q.id))}>{t('bank.approveAll', { n: questions.length })}</button>}
      </div>
      <div className="bank-filters">
        <label className="student-picker">{t('common.col.status')}<select value={status} onChange={(e) => setStatus(e.target.value as 'draft' | 'published')}>
          <option value="draft">{t('status.draft')}</option>
          <option value="published">{t('status.published')}</option>
        </select></label>
        <label className="student-picker">{t('bank.paper')}<select value={paper} onChange={(e) => setPaper(e.target.value)}>
          <option value="">{t('bank.allPapers')}</option>
          {allPapers.map((value) => <option key={value} value={value}>{paperName(value)}</option>)}
        </select></label>
      </div>
      <div className="question-list">
        {questions.map((q) => <article key={q.id}>
          <div className="question-card-head">
            <span className={`status-pill ${q.status}`}>{label('status', q.status)}</span>
            <small>{paperName(q.paper)} · {q.grade} · {q.topic}{q.exam_only ? ` · ${t('bank.examOnlyTag')}` : ''}</small>
          </div>
          <strong>{q.prompt}</strong>
          {q.options.map((option) => <span className={`question-option ${q.correct === option.id ? 'correct-option' : ''}`} key={option.id}>{option.id}. {option.text}{q.correct === option.id ? ' ✓' : ''}</span>)}
          {q.explanation && <small>{q.explanation}</small>}
          <small>{t('questions.sourceLine', { source: q.source_name })} · {q.license_notes}{q.author ? ` · ${q.author}` : ''}</small>
          {isAdmin && q.status === 'published' && <div className="operation-actions"><button className="secondary compact" disabled={busy} onClick={() => void archive(q.id)}>{t('questions.archive')}</button></div>}
          {isAdmin && q.status === 'draft' && <div className="operation-actions">
            <button className="secondary compact" disabled={busy} onClick={() => void reject(q.id)}>{t('bank.reject')}</button>
            <button className="primary compact" disabled={busy} onClick={() => void approve([q.id])}>{t('bank.approve')}</button>
          </div>}
        </article>)}
        {!questions.length && <p className="empty">{t('bank.empty')}</p>}
      </div>
    </section>
  </div>
}
