import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Link } from 'react-router'
import { useAction, useFeedback } from '../../app/feedback'
import { useSession } from '../../app/session'
import { DateField } from '../../calendar/DateField'
import { useDates } from '../../calendar/useDates'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useClasses, useRefresh } from '../../lib/queries'
import { classLabel, MockGroup, MockResults, MockRound } from '../../lib/types'
import { usePrepBank } from './QuestionBank'
import { heatClass, usePaperName } from './readiness'

const ROUNDS_PATH = '/prep/mock-rounds'
const commonPapers = ['english', 'mathematics', 'aptitude']
const naturalPapers = ['physics', 'chemistry', 'biology']
const socialPapers = ['history', 'geography', 'economics']

function ScheduleForm() {
  const { t, label } = useI18n()
  const paperName = usePaperName()
  const refresh = useRefresh()
  const { setNotice } = useFeedback()
  const { busy, onSubmit } = useAction()
  const classes = useClasses().data ?? []
  const bank = usePrepBank()
  const [natural, setNatural] = useState(true)
  const [social, setSocial] = useState(true)
  const papers = [...commonPapers, ...(natural ? naturalPapers : []), ...(social ? socialPapers : [])]
  const approved = (paper: string) => {
    const row = bank.find((item) => item.paper === paper)
    return (row?.practice ?? 0) + (row?.exam_only ?? 0)
  }

  const save = onSubmit(async (f) => {
    const timetable = papers.map((paper) => {
      const opens = String(f.get(`opens_${paper}`) || '')
      if (!opens) throw new Error(t('mock.needTime', { paper: paperName(paper) }))
      return { paper, opens_at: new Date(opens).toISOString(), duration_minutes: Number(f.get(`minutes_${paper}`)) }
    })
    const created = await api<{ students_without_stream: number; students_without_papers: number }>(ROUNDS_PATH, { method: 'POST', body: JSON.stringify({
      name: f.get('name'), class_ids: f.getAll('class_ids').map(String), question_count: Number(f.get('question_count')), start_window_minutes: Number(f.get('start_window')), requires_start_code: f.get('requires_start_code') === 'on', papers: timetable,
    }) })
    await refresh(ROUNDS_PATH, '/assessments')
    setNotice(created.students_without_stream + created.students_without_papers > 0
      ? t('mock.createdWarning', { noStream: created.students_without_stream, noPapers: created.students_without_papers })
      : t('mock.created'))
  })

  const row = (paper: string) => <div className="mock-slot" key={paper}>
    <div><strong>{paperName(paper)}</strong><small>{t('mock.approved', { n: approved(paper) })}</small></div>
    <DateField name={`opens_${paper}`} label={t('mock.opens')} withTime />
    <label>{t('mock.minutes')}<input name={`minutes_${paper}`} type="number" min="5" max="240" defaultValue="120" required /></label>
  </div>

  return <section className="panel">
    <div className="panel-head"><div><p className="eyebrow">{t('mock.eyebrow')}</p><h2>{t('mock.scheduleTitle')}</h2><p>{t('mock.scheduleHelp')}</p></div></div>
    <form className="stack-form" onSubmit={save}>
      <div className="history-fields">
        <label>{t('mock.name')}<input name="name" required maxLength={120} placeholder={t('mock.nameHint')} /></label>
        <label>{t('prep.questionCount')}<input name="question_count" type="number" min="5" max="100" defaultValue="50" required /></label>
        <label>{t('mock.startWindow')}<input name="start_window" type="number" min="5" max="240" defaultValue="30" required /></label>
      </div>
      <fieldset className="mock-choices">
        <legend>{t('mock.classes')}</legend>
        {classes.map((c) => <label className="bank-check" key={c.id}><input type="checkbox" name="class_ids" value={c.id} /> {classLabel(c)}</label>)}
        {!classes.length && <p className="empty">{t('mock.noClasses')}</p>}
      </fieldset>
      <fieldset className="mock-choices">
        <legend>{t('mock.streams')}</legend>
        <label className="bank-check"><input type="checkbox" checked={natural} onChange={(e) => setNatural(e.target.checked)} /> {label('stream', 'natural')}</label>
        <label className="bank-check"><input type="checkbox" checked={social} onChange={(e) => setSocial(e.target.checked)} /> {label('stream', 'social')}</label>
      </fieldset>
      <label className="bank-check"><input type="checkbox" name="requires_start_code" /> {t('assessments.invigilated')}</label>
      <h3 className="mock-heading">{t('mock.timetable')}</h3>
      <p className="form-hint">{t('mock.timetableHelp')}</p>
      <div className="mock-slots">
        <h4>{t('mock.commonPapers')}</h4>
        {commonPapers.map(row)}
        {natural && <h4>{label('stream', 'natural')}</h4>}
        {natural && naturalPapers.map(row)}
        {social && <h4>{label('stream', 'social')}</h4>}
        {social && socialPapers.map(row)}
      </div>
      <button className="primary compact" disabled={busy || !classes.length}>{busy ? t('common.saving') : t('mock.create')} →</button>
    </form>
  </section>
}

// RoundCard shows one mock day's timetable. Administrators can move a paper or
// cancel until students start.
function RoundCard({ round, canManage, selected, onSelect }: { round: MockRound; canManage: boolean; selected: boolean; onSelect: () => void }) {
  const { t, label } = useI18n()
  const { dateTime } = useDates()
  const paperName = usePaperName()
  const refresh = useRefresh()
  const { busy, run, onSubmit } = useAction()
  const [moving, setMoving] = useState('')
  const started = round.papers.some((paper) => paper.in_progress + paper.submitted > 0)

  const cancel = () => {
    if (!window.confirm(t('mock.cancelConfirm', { name: round.name }))) return
    void run(async () => {
      await api(`${ROUNDS_PATH}/${round.id}/cancel`, { method: 'POST', body: '{}' })
      await refresh(ROUNDS_PATH, '/assessments')
    }, t('mock.cancelled'))
  }

  const move = (paper: string) => onSubmit(async (f) => {
    const opens = String(f.get('opens_at') || '')
    if (!opens) throw new Error(t('mock.needTime', { paper: paperName(paper) }))
    await api(`${ROUNDS_PATH}/${round.id}/reschedule`, { method: 'POST', body: JSON.stringify({ paper, opens_at: new Date(opens).toISOString() }) })
    setMoving('')
    await refresh(ROUNDS_PATH)
  }, t('mock.moved'))

  return <article className="mock-round">
    <div className="mock-round-head">
      <div><strong>{round.name}</strong><small>{t('mock.roundLine', { papers: round.papers.length, students: round.summary.all.students, complete: round.summary.all.complete })}</small></div>
      <div className="operation-actions">
        <button className="secondary compact" aria-pressed={selected} onClick={onSelect}>{selected ? t('mock.hideResults') : t('mock.showResults')}</button>
        {canManage && !started && <button className="secondary compact" disabled={busy} onClick={cancel}>{t('mock.cancel')}</button>}
      </div>
    </div>
    <div className="table-wrap"><table>
      <thead><tr><th>{t('bank.col.paper')}</th><th>{t('readiness.col.stream')}</th><th>{t('mock.col.opens')}</th><th>{t('mock.col.length')}</th><th>{t('mock.col.progress')}</th><th></th></tr></thead>
      <tbody>{round.papers.map((paper) => <tr key={paper.paper}>
        <td><strong>{paperName(paper.paper)}</strong></td>
        <td>{paper.stream ? label('stream', paper.stream) : t('mock.everyone')}</td>
        <td>{moving === paper.paper
          ? <form className="mock-move" onSubmit={move(paper.paper)}>
              <DateField name="opens_at" label={t('mock.newTime')} withTime />
              <button className="primary compact" disabled={busy}>{t('common.save')}</button>
              <button type="button" className="secondary compact" onClick={() => setMoving('')}>{t('common.cancel')}</button>
            </form>
          : dateTime(paper.opens_at)}</td>
        <td>{t('common.minutesShort', { n: paper.duration_minutes })}</td>
        <td>{t('mock.progress', { active: paper.in_progress, done: paper.submitted })}</td>
        <td>{canManage && paper.in_progress + paper.submitted === 0 && moving !== paper.paper && <button className="staff-toggle" onClick={() => setMoving(paper.paper)}>{t('mock.move')}</button>}</td>
      </tr>)}</tbody>
    </table></div>
  </article>
}

function downloadResults(name: string, data: MockResults, paperName: (paper: string) => string, headers: string[]) {
  const quote = (value: string) => `"${value.replace(/"/g, '""')}"`
  const papers = [...commonPapers, ...naturalPapers, ...socialPapers]
  const lines = [[...headers, ...papers.map(paperName), 'Total'].map(quote).join(',')]
  for (const student of data.students) {
    lines.push([student.student_number, student.student_name, `${student.grade_level} ${student.class_name}`, student.stream,
      ...papers.map((paper) => (student.scores[paper] === undefined ? '' : String(Math.round(student.scores[paper])))),
      student.total === null ? '' : String(Math.round(student.total))].map(quote).join(','))
  }
  const link = document.createElement('a')
  link.href = URL.createObjectURL(new Blob([`﻿${lines.join('\r\n')}`], { type: 'text/csv;charset=utf-8' }))
  link.download = `${name.replace(/[^\w-]+/g, '-')}-results.csv`
  link.click()
  URL.revokeObjectURL(link.href)
}

function RoundResults({ round }: { round: MockRound }) {
  const { t, label } = useI18n()
  const paperName = usePaperName()
  const path = `${ROUNDS_PATH}/${round.id}/results`
  const data = useQuery({ queryKey: [path], queryFn: () => api<MockResults>(path) }).data
  if (!data) return <p className="empty" role="status">{t('common.loading')}</p>

  const groupRow = (group: MockGroup, name: string) => <tr key={group.key}>
    <td><strong>{name}</strong></td>
    <td>{group.students}</td>
    <td>{group.complete}</td>
    <td>{group.average_total === null ? '—' : Math.round(group.average_total)}</td>
    <td>{group.below_line}</td>
  </tr>
  const groupHead = (first: string) => <thead><tr><th>{first}</th><th>{t('readiness.col.students')}</th><th>{t('mock.col.complete')}</th><th>{t('readiness.col.average')}</th><th>{t('mock.col.below', { line: data.pass_line })}</th></tr></thead>
  const papersSat = data.summary.papers.filter((paper) => paper.sat > 0)

  return <section className="panel">
    <div className="panel-head">
      <div><p className="eyebrow">{t('mock.resultsEyebrow')}</p><h2>{round.name}</h2><p>{t('mock.resultsHelp', { line: data.pass_line })}</p></div>
      {data.students.length > 0 && <button className="secondary compact" onClick={() => downloadResults(round.name, data, paperName, [t('gradebook.csvStudentID'), t('gradebook.csvStudent'), t('common.class'), t('stream.heading')])}>{t('gradebook.download')}</button>}
    </div>
    <div className="table-wrap"><table>{groupHead(t('readiness.col.stream'))}<tbody>{data.summary.streams.map((group) => groupRow(group, label('stream', group.key)))}</tbody></table></div>
    <div className="table-wrap readiness-classes"><table>{groupHead(t('common.class'))}<tbody>
      {data.summary.classes.map((group) => groupRow(group, `${group.grade_level} · ${group.name}`))}
    </tbody></table></div>
    {papersSat.length > 0 && <div className="heatmap readiness-classes">
      {papersSat.map((paper) => <article className={`heat-cell ${heatClass(paper.average ?? 0)}`} key={paper.paper}>
        <strong>{paperName(paper.paper)}</strong>
        <b>{Math.round(paper.average ?? 0)}</b>
        <small>{t('mock.sat', { n: paper.sat })}</small>
      </article>)}
    </div>}
    <div className="table-wrap readiness-classes"><table>
      <thead><tr><th>{t('common.col.student')}</th><th>{t('common.class')}</th><th>{t('readiness.col.stream')}</th><th>{t('mock.col.sat')}</th><th>{t('readiness.col.papers')}</th><th>{t('mock.col.total')}</th></tr></thead>
      <tbody>
        {data.students.map((student) => <tr key={student.student_id}>
          <td><strong>{student.student_name}</strong><small>{student.student_number}</small></td>
          <td>{student.grade_level} · {student.class_name}</td>
          <td>{student.stream ? label('stream', student.stream) : t('stream.none')}</td>
          <td>{student.papers_sat} / {student.papers_expected}</td>
          <td><div className="paper-chips">{Object.entries(student.scores).map(([paper, score]) => <span key={paper} className={`paper-chip ${heatClass(score)}`}>{paperName(paper)} {Math.round(score)}</span>)}</div></td>
          <td>{student.total === null ? '—' : <span className={`readiness-badge ${student.total < data.pass_line ? 'at_risk' : 'above_line'}`}>{Math.round(student.total)}</span>}</td>
        </tr>)}
        {!data.students.length && <tr><td colSpan={6} className="empty">{t('common.noActiveStudents')}</td></tr>}
      </tbody>
    </table></div>
  </section>
}

// MockDays is where administrators schedule mock days and both administrators
// and teachers follow them. Teachers see only their own classes.
export function MockDays() {
  const { t, label } = useI18n()
  const { identity } = useSession()
  const isAdmin = identity?.roles.includes('school_admin') ?? false
  const query = useQuery({ queryKey: [ROUNDS_PATH], queryFn: () => api<MockRound[]>(ROUNDS_PATH) })
  const rounds = query.data ?? []
  const [selected, setSelected] = useState('')
  const chosen = rounds.find((round) => round.id === selected)
  // The API lists newest first. A trend reads oldest to newest.
  const trend = [...rounds].reverse().filter((round) => round.summary.all.complete > 0)

  return <div className="operations-page">
    {isAdmin && query.isSuccess && !rounds.length && <ScheduleForm />}
    {trend.length > 0 && <section className="panel">
      <div className="panel-head"><div><h2>{t('mock.trendTitle')}</h2><p>{t('mock.trendHelp')}</p></div></div>
      <div className="table-wrap"><table>
        <thead><tr><th>{t('mock.col.round')}</th><th>{label('stream', 'natural')}</th><th>{label('stream', 'social')}</th><th>{t('mock.col.complete')}</th><th>{t('mock.col.belowShort')}</th></tr></thead>
        <tbody>{trend.map((round) => {
          const [natural, social] = round.summary.streams
          const average = (value: number | null) => (value === null ? '—' : Math.round(value))
          return <tr key={round.id}><td><strong>{round.name}</strong></td><td>{average(natural.average_total)}</td><td>{average(social.average_total)}</td><td>{round.summary.all.complete} / {round.summary.all.students}</td><td>{round.summary.all.below_line}</td></tr>
        })}</tbody>
      </table></div>
    </section>}
    <section className="panel">
      <div className="panel-head"><div><h2>{t('mock.listTitle')}</h2><p>{t('mock.listHelp')}</p></div></div>
      <div className="mock-rounds">
        {rounds.map((round) => <RoundCard key={round.id} round={round} canManage={isAdmin} selected={selected === round.id} onSelect={() => setSelected(selected === round.id ? '' : round.id)} />)}
        {query.isSuccess && !rounds.length && <p className="empty">{isAdmin ? t('mock.empty') : t('mock.emptyTeacher')}</p>}
      </div>
      {!isAdmin && rounds.length > 0 && <p className="form-hint">{t('mock.invigilateHint')} <Link to="/teach/exam-sessions">{t('teacher.nav.sessions')}</Link></p>}
    </section>
    {chosen && <RoundResults key={chosen.id} round={chosen} />}
    {isAdmin && rounds.length > 0 && <ScheduleForm />}
  </div>
}
