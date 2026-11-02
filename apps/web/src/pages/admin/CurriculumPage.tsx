import { useEffect, useState } from 'react'
import { useAction } from '../../app/feedback'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import { useCurricula, useNodes, useQuestions, useRefresh, useSubjects } from '../../lib/queries'

export default function CurriculumPage() {
  const { t, label } = useI18n()
  const { busy, run, onSubmit } = useAction()
  const refresh = useRefresh()
  const curricula = useCurricula().data ?? []
  const questions = useQuestions().data ?? []
  const [curriculumID, setCurriculumID] = useState('')
  const [subjectID, setSubjectID] = useState('')
  const subjects = useSubjects(curriculumID).data ?? []
  const nodes = useNodes(subjectID).data ?? []

  useEffect(() => { setCurriculumID((current) => current || curricula[0]?.id || '') }, [curricula])
  useEffect(() => { setSubjectID((current) => (subjects.some((s) => s.id === current) ? current : subjects[0]?.id || '')) }, [subjects])

  const addCurriculum = onSubmit(async (f) => {
    const created = await api<{ id: string }>('/curricula', { method: 'POST', body: JSON.stringify({ name: f.get('curriculum_name'), provider: f.get('provider'), country_code: f.get('country_code'), version_label: f.get('version_label'), language_code: f.get('language_code') || 'en', source_reference: f.get('curriculum_source'), license_notes: f.get('curriculum_license') }) })
    await refresh('/curricula')
    setCurriculumID(created.id)
  }, t('curriculum.added'))

  const addSubject = onSubmit(async (f) => {
    await api(`/curricula/${curriculumID}/subjects`, { method: 'POST', body: JSON.stringify({ name: f.get('subject_name'), grade_level: f.get('grade_level'), subject_code: f.get('subject_code') }) })
    await refresh(`/curricula/${curriculumID}/subjects`)
  }, t('curriculum.subjectAdded'))

  const addNode = onSubmit(async (f) => {
    await api(`/subjects/${subjectID}/nodes`, { method: 'POST', body: JSON.stringify({ parent_id: f.get('parent_id'), node_type: f.get('node_type'), code: f.get('node_code'), title: f.get('node_title'), description: f.get('node_description'), sort_order: Number(f.get('sort_order') || 0) }) })
    await refresh(`/subjects/${subjectID}/nodes`)
  }, t('curriculum.nodeAdded'))

  const addQuestion = onSubmit(async (f) => {
    const type = String(f.get('question_type'))
    const options = String(f.get('options') || '').split('\n').map((line) => line.trim()).filter(Boolean).map((line) => {
      const [optionID, ...text] = line.split('|')
      return { id: optionID.trim(), text: text.join('|').trim() }
    })
    let answerKey: unknown
    try { answerKey = JSON.parse(String(f.get('answer_key') || 'null')) } catch { throw new Error(t('questions.badAnswerKey')) }
    const skill = String(f.get('skill_id') || '')
    await api('/questions', { method: 'POST', body: JSON.stringify({ question_type: type, prompt: { text: f.get('prompt') }, options: type === 'single_choice' || type === 'multiple_choice' ? options : null, answer_key: answerKey, explanation: { text: f.get('explanation') }, difficulty: Number(f.get('difficulty')), language_code: f.get('language_code') || 'en', source_name: f.get('source_name'), source_reference: f.get('source_reference'), license_notes: f.get('license_notes'), skill_tags: skill ? [{ skill_id: skill, weight: 1 }] : [] }) })
    await refresh('/questions')
  }, t('questions.saved'))

  const publishQuestion = (questionID: string) => run(async () => {
    await api(`/questions/${questionID}/publish`, { method: 'POST', body: '{}' })
    await refresh('/questions')
  }, t('questions.published'))

  return <section className="panel curriculum-panel" id="curriculum">
    <div className="panel-head"><div><p className="eyebrow">{t('curriculum.eyebrow')}</p><h2>{t('curriculum.heading')}</h2><p>{t('curriculum.help')}</p></div><span className="year-icon">⌘</span></div>
    <div className="curriculum-grid">
      <div className="curriculum-column">
        <h3>{t('curriculum.structure')}</h3>
        <form className="stack-form curriculum-form" onSubmit={addCurriculum}>
          <strong>{t('curriculum.newVersion')}</strong>
          <div className="history-fields">
            <input name="curriculum_name" placeholder={t('curriculum.name')} aria-label={t('curriculum.name')} required />
            <input name="provider" placeholder={t('curriculum.provider')} aria-label={t('curriculum.provider')} required />
            <input name="country_code" placeholder={t('curriculum.country')} aria-label={t('curriculum.country')} />
            <input name="version_label" placeholder={t('curriculum.version')} aria-label={t('curriculum.version')} required />
            <input name="language_code" placeholder={t('curriculum.language')} aria-label={t('curriculum.language')} defaultValue="en" />
            <input name="curriculum_source" placeholder={t('curriculum.source')} aria-label={t('curriculum.source')} />
            <input name="curriculum_license" placeholder={t('curriculum.license')} aria-label={t('curriculum.license')} />
          </div>
          <button className="secondary" disabled={busy}>{t('curriculum.add')} <span>→</span></button>
        </form>
        {curricula.length > 0 && <>
          <label className="student-picker">{t('curriculum.curriculum')}<select value={curriculumID} onChange={(e) => setCurriculumID(e.target.value)}>
            {curricula.map((c) => <option key={c.id} value={c.id}>{c.name} · {c.provider} · {c.version_label}</option>)}
          </select></label>
          <form className="inline-form curriculum-form" onSubmit={addSubject}>
            <input name="subject_name" placeholder={t('curriculum.subjectName')} aria-label={t('curriculum.subjectName')} required />
            <input name="grade_level" placeholder={t('curriculum.gradeLevel')} aria-label={t('curriculum.gradeLevel')} />
            <input name="subject_code" placeholder={t('curriculum.subjectCode')} aria-label={t('curriculum.subjectCode')} />
            <button className="secondary" disabled={busy}>{t('curriculum.addSubject')}</button>
          </form>
        </>}
        {subjects.length > 0 && <>
          <label className="student-picker">{t('plans.subject')}<select value={subjectID} onChange={(e) => setSubjectID(e.target.value)}>
            {subjects.map((s) => <option key={s.id} value={s.id}>{s.name}{s.grade_level ? ` · ${s.grade_level}` : ''}</option>)}
          </select></label>
          <form className="stack-form curriculum-form" onSubmit={addNode}>
            <strong>{t('curriculum.addNodeTitle')}</strong>
            <div className="history-fields">
              <select name="node_type" aria-label={t('curriculum.nodeType')}>
                {['unit', 'topic', 'concept', 'skill', 'objective'].map((type) => <option key={type} value={type}>{label('node', type)}</option>)}
              </select>
              <select name="parent_id" aria-label={t('curriculum.parentNode')}>
                <option value="">{t('curriculum.topLevel')}</option>
                {nodes.map((n) => <option key={n.id} value={n.id}>{label('node', n.node_type)}: {n.title}</option>)}
              </select>
              <input name="node_title" placeholder={t('curriculum.nodeTitle')} aria-label={t('curriculum.nodeTitle')} required />
              <input name="node_code" placeholder={t('curriculum.nodeCode')} aria-label={t('curriculum.nodeCode')} />
              <input name="sort_order" type="number" placeholder={t('curriculum.order')} aria-label={t('curriculum.order')} defaultValue="0" />
              <input name="node_description" placeholder={t('finance.description')} aria-label={t('finance.description')} />
            </div>
            <button className="secondary" disabled={busy}>{t('curriculum.addNode')} <span>→</span></button>
          </form>
          <div className="curriculum-node-list">
            {nodes.map((n) => <article key={n.id} className={`node-${n.node_type}`}><span>{label('node', n.node_type)}</span><strong>{n.title}</strong>{n.code && <small>{n.code}</small>}</article>)}
          </div>
        </>}
        {!curricula.length && <p className="empty">{t('curriculum.empty')}</p>}
      </div>
      <div className="curriculum-column">
        <h3>{t('questions.title')} <span>{questions.length}</span></h3>
        <form className="stack-form question-form" onSubmit={addQuestion}>
          <select name="question_type" aria-label={t('questions.type')}>
            {['single_choice', 'multiple_choice', 'numeric', 'text'].map((type) => <option key={type} value={type}>{label('questionType', type)}</option>)}
          </select>
          <textarea name="prompt" placeholder={t('questions.prompt')} aria-label={t('questions.prompt')} required rows={3} />
          <textarea name="options" placeholder={t('questions.options')} aria-label={t('questions.optionsLabel')} rows={3} />
          <textarea name="answer_key" placeholder={t('questions.answerKey')} aria-label={t('questions.answerKeyLabel')} required rows={2} />
          <textarea name="explanation" placeholder={t('questions.explanation')} aria-label={t('questions.explanationLabel')} rows={2} />
          <div className="history-fields">
            <select name="skill_id" aria-label={t('questions.skillTag')}>
              <option value="">{t('questions.skillTag')}</option>
              {nodes.filter((n) => n.node_type === 'skill').map((n) => <option key={n.id} value={n.id}>{n.title}</option>)}
            </select>
            <select name="difficulty" aria-label={t('questions.difficulty')} defaultValue="3">
              <option value="1">{t('questions.difficulty1')}</option>
              <option value="2">{t('questions.difficultyN', { n: 2 })}</option>
              <option value="3">{t('questions.difficulty3')}</option>
              <option value="4">{t('questions.difficultyN', { n: 4 })}</option>
              <option value="5">{t('questions.difficulty5')}</option>
            </select>
            <input name="language_code" placeholder={t('curriculum.language')} aria-label={t('curriculum.language')} defaultValue="en" />
            <input name="source_name" placeholder={t('questions.source')} aria-label={t('questions.source')} required />
            <input name="source_reference" placeholder={t('questions.reference')} aria-label={t('questions.reference')} />
            <input name="license_notes" placeholder={t('curriculum.license')} aria-label={t('curriculum.license')} required />
          </div>
          <p className="form-hint">{t('questions.hint')}</p>
          <button className="secondary" disabled={busy || !nodes.some((n) => n.node_type === 'skill')}>{t('questions.save')} <span>→</span></button>
        </form>
        <div className="question-list">
          {questions.map((q) => <article key={q.id}>
            <div className="question-card-head">
              <span className={`status-pill ${q.status}`}>{label('status', q.status)}</span>
              <small>{label('questionType', q.question_type)} · {q.difficulty === null ? t('questions.unrated') : t('questions.level', { n: q.difficulty })}</small>
            </div>
            <strong>{q.prompt?.text || t('questions.promptFallback')}</strong>
            {q.options?.map((o) => <span className="question-option" key={o.id}>{o.id}. {o.text}</span>)}
            <small>{q.skill_tags.map((tag) => `${tag.title} (${Number(tag.weight).toFixed(2)})`).join(' · ') || t('questions.noTag')}</small>
            {q.source_name && <small>{t('questions.sourceLine', { source: q.source_name })}{q.license_notes ? ` · ${q.license_notes}` : ''}</small>}
            {q.status === 'draft' && <button className="secondary compact" disabled={busy || !q.skill_tags.length} onClick={() => void publishQuestion(q.id)}>{t('questions.publish')}</button>}
          </article>)}
          {!questions.length && <p className="empty">{t('questions.empty')}</p>}
        </div>
      </div>
    </div>
  </section>
}
