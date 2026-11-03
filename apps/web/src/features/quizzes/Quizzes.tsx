import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useAction, useFeedback } from '../../app/feedback'
import { useSession } from '../../app/session'
import { DateField } from '../../calendar/DateField'
import { useDates } from '../../calendar/useDates'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useRefresh } from '../../lib/queries'
import { ClassRoom, ClassSubject, classLabel, QuizData, QuizQuestion, QuizResults, QuizRow, Resource } from '../../lib/types'
import { TermSelect, useClassTerm } from '../marks/TermSelect'

const LIST_KEY = '/quizzes'
const blankQuestion = (topic = ''): QuizQuestion => ({ question_type: 'single_choice', prompt: '', options: ['', ''], correct: 0, value: null, tolerance: 0, guide: '', topic, points: 1, explanation: '' })
const toISO = (value: FormDataEntryValue | null) => (value ? new Date(String(value)).toISOString() : null)

type View = { mode: 'list' } | { mode: 'new'; kind: 'quiz' | 'assignment' } | { mode: 'edit'; id: string } | { mode: 'results'; id: string }

// Quizzes is where a teacher sets work for a class and sees how it went. Each is
// an ordinary assessment, so marking, saved answers and skill tracking are shared.
export function Quizzes({ classroom, classes }: { classroom: ClassRoom; classes: ClassRoom[] }) {
  const { t, label } = useI18n()
  const { date } = useDates()
  const [view, setView] = useState<View>({ mode: 'list' })
  const subjects = useQuery({ queryKey: ['/class-subjects', classroom.id, ''], queryFn: () => api<ClassSubject[]>(`/classes/${classroom.id}/subjects`) }).data ?? []
  const query = useQuery({ queryKey: [LIST_KEY, classroom.id], queryFn: () => api<QuizRow[]>(`/quizzes?class_id=${classroom.id}`) })
  const rows = query.data ?? []
  const back = () => setView({ mode: 'list' })

  if (view.mode === 'new') return <QuizEditor classroom={classroom} subjects={subjects} kind={view.kind} onSaved={(id) => setView({ mode: 'edit', id })} onCancel={back} />
  if (view.mode === 'edit') return <LoadedEditor key={view.id} id={view.id} classroom={classroom} subjects={subjects} onDone={(id) => setView(id ? { mode: 'results', id } : { mode: 'list' })} onCancel={back} />
  if (view.mode === 'results') return <Results key={view.id} id={view.id} classroom={classroom} classes={classes} onBack={back} onEdit={(id) => setView({ mode: 'edit', id })} />

  return <div className="operations-page">
    <section className="panel">
      <div className="panel-head">
        <div><h2>{t('quiz.listTitle')}</h2><p>{t('quiz.listHelp')}</p></div>
        <div className="operation-actions">
          <button className="primary compact" disabled={!subjects.length} onClick={() => setView({ mode: 'new', kind: 'quiz' })}>{t('quiz.newQuiz')}</button>
          <button className="secondary compact" disabled={!subjects.length} onClick={() => setView({ mode: 'new', kind: 'assignment' })}>{t('quiz.newAssignment')}</button>
        </div>
      </div>
      {!subjects.length && <p className="empty">{t('marks.noSubjects')}</p>}
      <ul className="guardian-list">
        {rows.map((row) => <li key={row.id}>
          <div className="guardian-who">
            <strong>{row.title}</strong>
            <small>{label('quiz.kind', row.kind)} · {row.subject} · {t('quiz.questionsLine', { n: row.question_count, points: row.total_points })}</small>
            {row.status === 'published' && <small>{t('quiz.submittedLine', { done: row.submitted, total: row.students })}{row.due_at && ` · ${t('quiz.dueLine', { date: date(row.due_at) })}`}{row.average_percent !== null && ` · ${t('quiz.averageLine', { n: Math.round(row.average_percent) })}`}</small>}
            {row.waiting_review > 0 && <small className="insight-down">{t('quiz.waitingLine', { n: row.waiting_review })}</small>}
          </div>
          <span className={`status-pill ${row.status === 'published' ? '' : 'draft'}`}>{label('quiz.status', row.status)}</span>
          {row.in_marks && <span className="status-pill">{t('quiz.inMarks')}</span>}
          {row.status === 'published'
            ? <button className="secondary compact" onClick={() => setView({ mode: 'results', id: row.id })}>{t('quiz.results')}</button>
            : <button className="secondary compact" onClick={() => setView({ mode: 'edit', id: row.id })}>{t('guardian.edit')}</button>}
        </li>)}
        {query.isSuccess && !rows.length && <li className="empty">{t('quiz.empty')}</li>}
      </ul>
    </section>
  </div>
}

function LoadedEditor({ id, classroom, subjects, onDone, onCancel }: { id: string; classroom: ClassRoom; subjects: ClassSubject[]; onDone: (resultsFor?: string) => void; onCancel: () => void }) {
  const { errorText } = useI18n()
  const query = useQuery({ queryKey: [`/quizzes/${id}`], queryFn: () => api<QuizData>(`/quizzes/${id}`), staleTime: 0, meta: { silent: true } })
  if (query.error) return <section className="panel"><p className="row-error">{errorText(query.error, 'error.load')}</p></section>
  if (!query.data) return null
  return <QuizEditor key={query.dataUpdatedAt} classroom={classroom} subjects={subjects} saved={query.data} kind={query.data.kind} onSaved={() => undefined} onPublished={() => onDone(id)} onDeleted={() => onDone()} onCancel={onCancel} />
}

function QuizEditor({ classroom, subjects, saved, kind, onSaved, onPublished, onDeleted, onCancel }: {
  classroom: ClassRoom
  subjects: ClassSubject[]
  saved?: QuizData
  kind: 'quiz' | 'assignment'
  onSaved: (id: string) => void
  onPublished?: () => void
  onDeleted?: () => void
  onCancel: () => void
}) {
  const { t, label } = useI18n()
  const { identity } = useSession()
  const { setNotice } = useFeedback()
  const { busy, run, onSubmit } = useAction()
  const refresh = useRefresh()
  const editable = subjects.filter((subject) => subject.can_edit)
  const [title, setTitle] = useState(saved?.title ?? '')
  const [subjectID, setSubjectID] = useState(saved?.class_subject_id ?? editable[0]?.id ?? '')
  const [minutes, setMinutes] = useState(String(saved && saved.kind === 'quiz' ? saved.duration_minutes : 20))
  const [instructions, setInstructions] = useState(saved?.instructions ?? '')
  const [resourceID, setResourceID] = useState(saved?.resource_id ?? '')
  const [showAnswers, setShowAnswers] = useState(saved?.show_answers ?? true)
  const [shuffle, setShuffle] = useState(saved?.shuffle ?? false)
  const [questions, setQuestions] = useState<QuizQuestion[]>(saved?.questions.length ? saved.questions : [blankQuestion()])
  const [dirty, setDirty] = useState(!saved)
  const canEdit = saved ? saved.can_edit : true
  const resourcesPath = `/teacher/classes/${classroom.id}/resources`
  const resources = useQuery({ queryKey: [resourcesPath], queryFn: () => api<Resource[]>(resourcesPath) }).data ?? []
  const topics = useQuery({ queryKey: ['/quizzes/topics', subjectID], queryFn: () => api<string[]>(`/quizzes/topics?class_subject_id=${subjectID}`), enabled: Boolean(subjectID) }).data ?? []
  const listID = 'quiz-topics'
  const change = <T,>(setter: (value: T) => void) => (value: T) => { setter(value); setDirty(true) }
  const setQuestion = (index: number, next: Partial<QuizQuestion>) => { setQuestions(questions.map((q, i) => (i === index ? { ...q, ...next } : q))); setDirty(true) }
  const moveQuestion = (index: number, step: number) => {
    const list = [...questions]
    const [item] = list.splice(index, 1)
    list.splice(index + step, 0, item)
    setQuestions(list)
    setDirty(true)
  }
  const body = () => JSON.stringify({ class_subject_id: subjectID, title, instructions, kind, duration_minutes: kind === 'quiz' ? Number(minutes) : 0, resource_id: resourceID, show_answers: showAnswers, shuffle, questions })
  const save = (message: string) => run(async () => {
    if (saved) await api(`/quizzes/${saved.id}`, { method: 'PUT', body: body() })
    else onSaved((await api<{ id: string }>('/quizzes', { method: 'POST', body: body() })).id)
    setDirty(false)
    await refresh(LIST_KEY, '/quizzes/topics')
    if (saved) await refresh(`/quizzes/${saved.id}`)
  }, message)
  const publish = onSubmit(async (f) => {
    if (!saved) return
    if (dirty) await api(`/quizzes/${saved.id}`, { method: 'PUT', body: body() })
    await api(`/quizzes/${saved.id}/publish`, { method: 'POST', body: JSON.stringify({ opens_at: toISO(f.get('opens_at')), due_at: toISO(f.get('due_at')) }) })
    await refresh(LIST_KEY)
    onPublished?.()
  }, t('quiz.published'), false)
  const remove = () => {
    if (!saved || !window.confirm(t('quiz.deleteConfirm', { title: saved.title }))) return
    void run(async () => {
      await api(`/quizzes/${saved.id}`, { method: 'DELETE' })
      await refresh(LIST_KEY)
      onDeleted?.()
    }, t('quiz.deleted'))
  }
  const cancel = () => { if (!dirty || window.confirm(t('quiz.leaveConfirm'))) onCancel() }
  const uploadWorksheet = onSubmit(async (f, form) => {
    const file = f.get('file') as File | null
    if (!file || file.size === 0) throw new Error(t('resources.chooseFile'))
    const values = new FormData()
    values.set('title', file.name)
    values.set('subject', subjects.find((subject) => subject.id === subjectID)?.name ?? '')
    values.set('description', '')
    values.set('file', file)
    const created = await api<{ id: string }>(resourcesPath, { method: 'POST', body: values })
    await refresh(resourcesPath)
    setResourceID(created.id)
    setDirty(true)
    form.reset()
    setNotice(t('quiz.worksheetAdded'))
  }, undefined, false)

  return <div className="operations-page">
    <section className="panel">
      <div className="panel-head">
        <div><h2>{saved ? saved.title || label('quiz.kind', kind) : t(kind === 'quiz' ? 'quiz.newQuiz' : 'quiz.newAssignment')}</h2><p>{t(kind === 'quiz' ? 'quiz.quizHelp' : 'quiz.assignmentHelp')}</p></div>
        <button className="secondary compact" onClick={cancel}>← {t('profile.back')}</button>
      </div>
      {!canEdit && <p className="report-incomplete">{t('marks.readOnly')}</p>}
      <div className="stack-form profile-form">
        <label className="profile-wide">{t('quiz.title')}<input value={title} maxLength={200} onChange={(e) => change(setTitle)(e.target.value)} /></label>
        <label>{t('reports.subject')}<select value={subjectID} disabled={Boolean(saved)} onChange={(e) => change(setSubjectID)(e.target.value)}>
          {(saved ? subjects : editable).map((subject) => <option key={subject.id} value={subject.id}>{subject.name}</option>)}
        </select></label>
        {kind === 'quiz' && <label>{t('quiz.minutes')}<input type="number" min="5" max="240" value={minutes} onChange={(e) => change(setMinutes)(e.target.value)} /></label>}
        <label className="profile-wide">{t('quiz.instructions')}<textarea rows={2} maxLength={5000} value={instructions} onChange={(e) => change(setInstructions)(e.target.value)} /></label>
        <label>{t('quiz.worksheet')}<select value={resourceID} onChange={(e) => change(setResourceID)(e.target.value)}>
          <option value="">{t('quiz.noWorksheet')}</option>
          {resources.map((resource) => <option key={resource.id} value={resource.id}>{resource.title}</option>)}
        </select></label>
        <div className="quiz-checks">
          <label className="check-row"><input type="checkbox" checked={showAnswers} onChange={(e) => change(setShowAnswers)(e.target.checked)} /> {t('quiz.showAnswers')}</label>
          <label className="check-row"><input type="checkbox" checked={shuffle} onChange={(e) => change(setShuffle)(e.target.checked)} /> {t('quiz.shuffle')}</label>
        </div>
      </div>
      {identity?.roles.includes('teacher') && <form className="csv-import quiz-upload" onSubmit={uploadWorksheet}>
        <input type="file" name="file" aria-label={t('quiz.uploadWorksheet')} required />
        <button className="secondary compact" disabled={busy}>{t('quiz.uploadWorksheet')}</button>
      </form>}
    </section>

    <datalist id={listID}>{topics.map((topic) => <option key={topic} value={topic} />)}</datalist>
    {questions.map((question, index) => <section className="panel quiz-question" key={index}>
      <div className="panel-head">
        <div><h3>{t('exam.question', { n: index + 1 })}</h3></div>
        <div className="operation-actions">
          <button type="button" className="secondary compact" disabled={index === 0} aria-label={t('quiz.moveUp', { n: index + 1 })} onClick={() => moveQuestion(index, -1)}>↑</button>
          <button type="button" className="secondary compact" disabled={index === questions.length - 1} aria-label={t('quiz.moveDown', { n: index + 1 })} onClick={() => moveQuestion(index, 1)}>↓</button>
          <button type="button" className="secondary compact" disabled={questions.length === 1} onClick={() => { setQuestions(questions.filter((_, i) => i !== index)); setDirty(true) }}>{t('guardian.remove')}</button>
        </div>
      </div>
      <div className="stack-form profile-form">
        <label className="profile-wide">{t('quiz.prompt')}<textarea rows={2} maxLength={4000} value={question.prompt} onChange={(e) => setQuestion(index, { prompt: e.target.value })} /></label>
        <label>{t('quiz.type')}<select value={question.question_type} onChange={(e) => setQuestion(index, { question_type: e.target.value as QuizQuestion['question_type'] })}>
          {(['single_choice', 'numeric', 'text'] as const).map((type) => <option key={type} value={type}>{label('quiz.type', type)}</option>)}
        </select></label>
        <label>{t('quiz.topic')}<input list={listID} maxLength={120} value={question.topic} placeholder={t('quiz.topicHint')} onChange={(e) => setQuestion(index, { topic: e.target.value })} /></label>
        <label>{t('quiz.points')}<input type="number" min="0.25" max="100" step="0.25" value={question.points} onChange={(e) => setQuestion(index, { points: Number(e.target.value) })} /></label>
        {question.question_type === 'numeric' && <>
          <label>{t('quiz.correctNumber')}<input type="number" step="any" value={question.value ?? ''} onChange={(e) => setQuestion(index, { value: e.target.value === '' ? null : Number(e.target.value) })} /></label>
          <label>{t('quiz.tolerance')}<input type="number" min="0" step="any" value={question.tolerance} onChange={(e) => setQuestion(index, { tolerance: Number(e.target.value) })} /></label>
        </>}
        {question.question_type === 'text' && <label className="profile-wide">{t('quiz.guide')}<textarea rows={2} maxLength={4000} value={question.guide} placeholder={t('quiz.guideHint')} onChange={(e) => setQuestion(index, { guide: e.target.value })} /></label>}
        {question.question_type === 'single_choice' && <fieldset className="quiz-options profile-wide">
          <legend>{t('quiz.choices')}</legend>
          {question.options.map((option, choice) => <div className="quiz-option" key={choice}>
            <input type="radio" name={`correct-${index}`} aria-label={t('quiz.correctChoice', { n: choice + 1 })} checked={question.correct === choice} onChange={() => setQuestion(index, { correct: choice })} />
            <input aria-label={t('quiz.choice', { n: choice + 1 })} maxLength={1000} value={option} onChange={(e) => setQuestion(index, { options: question.options.map((text, i) => (i === choice ? e.target.value : text)) })} />
            <button type="button" className="secondary compact" disabled={question.options.length <= 2} onClick={() => setQuestion(index, { options: question.options.filter((_, i) => i !== choice), correct: question.correct === choice ? 0 : question.correct > choice ? question.correct - 1 : question.correct })}>×</button>
          </div>)}
          {question.options.length < 6 && <button type="button" className="secondary compact" onClick={() => setQuestion(index, { options: [...question.options, ''] })}>{t('quiz.addChoice')}</button>}
        </fieldset>}
        <label className="profile-wide">{t('quiz.explanation')}<textarea rows={2} maxLength={4000} value={question.explanation} placeholder={t('quiz.explanationHint')} onChange={(e) => setQuestion(index, { explanation: e.target.value })} /></label>
      </div>
    </section>)}

    <section className="panel">
      <div className="register-foot quiz-foot">
        <button type="button" className="secondary compact" disabled={questions.length >= 100} onClick={() => { setQuestions([...questions, blankQuestion(questions[questions.length - 1]?.topic)]); setDirty(true) }}>{t('quiz.addQuestion')}</button>
        <div className="operation-actions">
          {saved && <button type="button" className="secondary compact" disabled={busy || !canEdit} onClick={remove}>{t('guardian.remove')}</button>}
          <button type="button" className="primary compact" disabled={busy || !canEdit || !dirty} onClick={() => void save(t('quiz.saved'))}>{t('quiz.saveDraft')}</button>
        </div>
      </div>
      {!saved && <p className="form-hint">{t('quiz.saveFirst')}</p>}
    </section>

    {saved && canEdit && <section className="panel">
      <div className="panel-head"><div><h2>{t('quiz.publishTitle')}</h2><p>{t('quiz.publishHelp')}</p></div></div>
      <form className="attendance-range" onSubmit={publish}>
        <DateField name="opens_at" label={t('quiz.opens')} withTime />
        <DateField name="due_at" label={t(kind === 'quiz' ? 'quiz.dueOptional' : 'quiz.due')} withTime required={kind === 'assignment'} />
        <div className="operation-actions"><button className="primary compact" disabled={busy}>{t('quiz.publish')}</button></div>
      </form>
    </section>}
  </div>
}

function Results({ id, classroom, classes, onBack, onEdit }: { id: string; classroom: ClassRoom; classes: ClassRoom[]; onBack: () => void; onEdit: (id: string) => void }) {
  const { t, label, errorText } = useI18n()
  const { dateTime } = useDates()
  const { setNotice } = useFeedback()
  const { busy, run, onSubmit } = useAction()
  const refresh = useRefresh()
  const { terms, term, setPicked } = useClassTerm(classroom.academic_year_id)
  const path = `/quizzes/${id}/results`
  const query = useQuery({ queryKey: [path], queryFn: () => api<QuizResults>(path), staleTime: 0, meta: { silent: true } })
  const results = query.data
  const refreshAll = () => refresh(path, LIST_KEY, '/marks', '/class-subjects', '/report-cards')
  if (query.error) return <section className="panel"><p className="row-error">{errorText(query.error, 'error.load')}</p></section>
  if (!results) return <p className="empty">{t('common.loading')}</p>
  const done = results.students.filter((student) => student.status === 'submitted').length
  const started = results.students.some((student) => student.status !== 'not_started')

  const send = onSubmit(async (f) => {
    const result = await api<{ sent: number; waiting: number }>(`/quizzes/${id}/send-to-marks`, { method: 'POST', body: JSON.stringify({ term_id: term?.id, weight: Number(f.get('weight')) }) })
    await refreshAll()
    setNotice(t('quiz.sentToMarks', { sent: result.sent, waiting: result.waiting }))
  }, undefined, false)
  const reschedule = onSubmit(async (f) => {
    await api(`/quizzes/${id}/schedule`, { method: 'PUT', body: JSON.stringify({ due_at: toISO(f.get('due_at')) }) })
    await refreshAll()
  }, t('quiz.rescheduled'), false)
  const takeBack = () => {
    if (!window.confirm(t('quiz.takeBackConfirm', { title: results.title }))) return
    void run(async () => {
      await api(`/quizzes/${id}`, { method: 'DELETE' })
      await refresh(LIST_KEY)
      onBack()
    }, t('quiz.deleted'))
  }

  return <div className="operations-page">
    <section className="panel">
      <div className="panel-head">
        <div><h2>{results.title}</h2><p>{t('quiz.submittedLine', { done, total: results.students.length })}</p></div>
        <button className="secondary compact" onClick={onBack}>← {t('profile.back')}</button>
      </div>
      <div className="table-wrap"><table>
        <thead><tr><th>{t('common.col.student')}</th><th>{t('common.col.status')}</th><th>{t('quiz.col.score')}</th><th>{t('quiz.col.submitted')}</th></tr></thead>
        <tbody>
          {results.students.map((student) => <tr key={student.student_id}>
            <td><strong>{student.student_name}</strong><small>{student.student_number}</small></td>
            <td><span className={`status-pill ${student.status === 'submitted' ? '' : 'draft'}`}>{label('status', student.status)}</span></td>
            <td>{student.status !== 'submitted' ? '—' : student.results_released ? `${student.final_score ?? 0} / ${student.final_max_score ?? 0}` : t('quiz.waitingMarking', { score: student.auto_score ?? 0, max: student.auto_max_score ?? 0 })}</td>
            <td>{student.submitted_at ? dateTime(student.submitted_at) : '—'}</td>
          </tr>)}
        </tbody>
      </table></div>
    </section>

    {results.topics.length > 0 && <section className="panel">
      <div className="panel-head"><div><h2>{t('quiz.topicsTitle')}</h2><p>{t('quiz.topicsHelp')}</p></div></div>
      <ul className="quiz-topics">
        {results.topics.map((topic) => <li key={topic.topic}>
          <span>{topic.topic}</span>
          <div className="mastery-bar"><i style={{ width: `${topic.percent}%` }} /></div>
          <strong>{Math.round(topic.percent)}%</strong>
        </li>)}
      </ul>
    </section>}

    <section className="panel">
      <div className="panel-head"><div><h2>{t('quiz.questionsTitle')}</h2><p>{t('quiz.questionsHelp')}</p></div></div>
      <div className="table-wrap"><table>
        <thead><tr><th>#</th><th>{t('quiz.prompt').toUpperCase()}</th><th>{t('quiz.topic').toUpperCase()}</th><th>{t('quiz.col.scored')}</th></tr></thead>
        <tbody>
          {results.questions.map((question) => <tr key={question.position}>
            <td>{question.position}</td>
            <td className="quiz-prompt-cell">{question.prompt}</td>
            <td>{question.topic}</td>
            <td>{question.percent === null ? '—' : `${Math.round(question.percent)}%`}</td>
          </tr>)}
        </tbody>
      </table></div>
    </section>

    {results.can_edit && <>
      <section className="panel">
        <div className="panel-head"><div><h2>{t('quiz.marksTitle')}</h2><p>{t('quiz.marksHelp')}</p></div></div>
        {terms.length === 0
          ? <p className="empty">{t('marks.noTerms')}</p>
          : <form className="attendance-range" onSubmit={send}>
              <TermSelect terms={terms} term={term} setPicked={setPicked} />
              <label>{t('marks.itemWeight')}<input name="weight" type="number" min="0.01" max="100" step="0.01" required /></label>
              <div className="operation-actions"><button className="secondary compact" disabled={busy}>{t('quiz.sendToMarks')}</button></div>
            </form>}
      </section>
      <section className="panel">
        <div className="panel-head"><div><h2>{t('quiz.changeDue')}</h2><p>{t('quiz.changeDueHelp')}</p></div></div>
        <form className="attendance-range" onSubmit={reschedule}>
          <DateField name="due_at" label={t(results.kind === 'quiz' ? 'quiz.dueOptional' : 'quiz.due')} withTime required={results.kind === 'assignment'} />
          <div className="operation-actions"><button className="secondary compact" disabled={busy}>{t('quiz.saveDue')}</button></div>
        </form>
        {!started && <div className="operation-actions quiz-takeback"><button className="secondary compact" disabled={busy} onClick={takeBack}>{t('quiz.takeBack')}</button></div>}
      </section>
    </>}
    <CopyPanel id={id} classes={classes} onCopied={onEdit} />
  </div>
}

function CopyPanel({ id, classes, onCopied }: { id: string; classes: ClassRoom[]; onCopied: (id: string) => void }) {
  const { t } = useI18n()
  const { busy, run } = useAction()
  const refresh = useRefresh()
  const [classID, setClassID] = useState('')
  const [subjectID, setSubjectID] = useState('')
  const subjects = useQuery({ queryKey: ['/class-subjects', classID, ''], queryFn: () => api<ClassSubject[]>(`/classes/${classID}/subjects`), enabled: Boolean(classID) }).data ?? []
  const editable = subjects.filter((subject) => subject.can_edit)
  const copy = () => run(async () => {
    const created = await api<{ id: string }>(`/quizzes/${id}/copy`, { method: 'POST', body: JSON.stringify({ class_subject_id: subjectID || editable[0]?.id }) })
    await refresh(LIST_KEY)
    if (classes.length) onCopied(created.id)
  }, t('quiz.copied'))
  return <section className="panel">
    <div className="panel-head"><div><h2>{t('quiz.copyTitle')}</h2><p>{t('quiz.copyHelp')}</p></div></div>
    <div className="attendance-range">
      <select aria-label={t('attendance.chooseClass')} value={classID} onChange={(event) => { setClassID(event.target.value); setSubjectID('') }}>
        <option value="">{t('attendance.chooseClass')}</option>
        {classes.map((c) => <option key={c.id} value={c.id}>{classLabel(c)}</option>)}
      </select>
      <select aria-label={t('reports.subject')} value={subjectID} disabled={!editable.length} onChange={(event) => setSubjectID(event.target.value)}>
        {editable.map((subject) => <option key={subject.id} value={subject.id}>{subject.name}</option>)}
      </select>
      <div className="operation-actions"><button className="secondary compact" disabled={busy || !editable.length} onClick={() => void copy()}>{t('quiz.copy')}</button></div>
    </div>
  </section>
}
