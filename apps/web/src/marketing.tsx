import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import './marketing.css'

const views = [
  {
    key: 'leaders',
    label: 'School leaders',
    eyebrow: 'A clearer school picture',
    title: 'See the whole school without chasing the paperwork.',
    description: 'Keep learner records, classes, staff access, and academic history connected. Make the next decision with the context already in front of you.',
    screenTitle: 'School overview',
    metricOne: 'Learners',
    valueOne: '642',
    metricTwo: 'Classes',
    valueTwo: '24',
    signal: 'Academic history organized',
    progress: '78%',
    detail: 'Sample workspace · illustrative data',
  },
  {
    key: 'teachers',
    label: 'Teachers',
    eyebrow: 'More time for teaching',
    title: 'Make curriculum and assessment work together.',
    description: 'Organize skills, build reusable questions, assign class assessments, and review written answers in one focused teacher workspace.',
    screenTitle: 'Class learning',
    metricOne: 'Assessments',
    valueOne: '08',
    metricTwo: 'To review',
    valueTwo: '03',
    signal: 'Class evidence collected',
    progress: '64%',
    detail: 'Sample workspace · illustrative data',
  },
  {
    key: 'learners',
    label: 'Learners',
    eyebrow: 'A focused learner experience',
    title: 'Give every learner a clear next step.',
    description: 'Learners see assigned assessments, work through questions, and get their released scores and teacher feedback in one place.',
    screenTitle: 'My learning',
    metricOne: 'Assigned',
    valueOne: '03',
    metricTwo: 'Completed',
    valueTwo: '02',
    signal: 'This week’s learning',
    progress: '67%',
    detail: 'Sample workspace · illustrative data',
  },
] as const

const useCases = [
  { number: '01', icon: '◉', title: 'Know each learner', text: 'Bring enrollment details, previous school results, and external exam evidence into one academic history.' },
  { number: '02', icon: '⌘', title: 'Connect curriculum to skills', text: 'Map subjects and learning skills, then build a reusable question bank with source and licensing notes.' },
  { number: '03', icon: '↗', title: 'Turn assessment into action', text: 'Create timed assessments, assign them to classes, review written answers, and release feedback.' },
  { number: '04', icon: '✳', title: 'Notice learning patterns', text: 'Use assessment evidence to surface explainable mastery estimates and suggested learning actions.' },
]

const scaleSteps = [
  { label: 'Start simple', title: 'A small school', text: 'Set up your school, add learners and classes, and bring existing academic history into a consistent record.' },
  { label: 'Build the rhythm', title: 'A growing school', text: 'Invite teachers, organize curriculum skills, and share assessment work across classes.' },
  { label: 'Keep the picture connected', title: 'A busy school', text: 'Give leaders, teachers, and learners focused workspaces while school records stay together.' },
]

function Reveal({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <div className={`landing-reveal ${className}`}>{children}</div>
}

export function MarketingLanding() {
  const [activeView, setActiveView] = useState(0)
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  const [walkthroughBusy, setWalkthroughBusy] = useState(false)
  const [walkthroughSent, setWalkthroughSent] = useState(false)
  const [walkthroughError, setWalkthroughError] = useState('')
  const [walkthroughStep, setWalkthroughStep] = useState(0)
  const [setup, setSetup] = useState({ name: '', email: '', role: '', school_name: '', school_size: '', timezone: 'Africa/Addis_Ababa', region: 'ET', default_language: 'en', priorities: [] as string[], message: '', contact_consent: false, website: '' })
  const active = views[activeView]

  useEffect(() => {
    const elements = Array.from(document.querySelectorAll('.landing-reveal'))
    if (!('IntersectionObserver' in window)) {
      elements.forEach((element) => element.classList.add('is-visible'))
      return
    }
    const observer = new IntersectionObserver((entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          entry.target.classList.add('is-visible')
          observer.unobserve(entry.target)
        }
      })
    }, { threshold: 0.12, rootMargin: '0px 0px -35px 0px' })
    elements.forEach((element) => observer.observe(element))
    return () => observer.disconnect()
  }, [])

  function updateSetup<K extends keyof typeof setup>(field: K, value: typeof setup[K]) {
    setSetup((current) => ({ ...current, [field]: value }))
  }

  function nextStep(form: HTMLFormElement) {
    setWalkthroughError('')
    if (walkthroughStep === 1 && setup.priorities.length === 0) {
      setWalkthroughError('Choose at least one area so we can prepare a useful starting point.')
      return
    }
    const fieldset = form.querySelector<HTMLElement>('[data-wizard-step="' + walkthroughStep + '"]')
    const invalid = fieldset?.querySelector<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>(':invalid')
    if (invalid) { invalid.reportValidity(); invalid.focus(); return }
    setWalkthroughStep((step) => Math.min(step + 1, 2))
  }

  async function submitWalkthrough(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (walkthroughBusy) return
    if (walkthroughStep === 0) { nextStep(event.currentTarget); return }
    if (setup.priorities.length === 0) {
      setWalkthroughError('Choose at least one area you want to improve.')
      setWalkthroughStep(1)
      return
    }
    setWalkthroughBusy(true); setWalkthroughError('')
    const apiBase = (import.meta.env.VITE_API_BASE_URL ?? '').trim().replace(/\/+$/, '')
    try {
      if (!apiBase) throw new Error('School setup is not connected right now. Please try again later.')
      const response = await fetch(apiBase + '/api/v1/demo-requests', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'omit',
        body: JSON.stringify(setup),
      })
      const contentType = response.headers.get('content-type')?.toLowerCase() ?? ''
      const body = contentType.includes('json') ? await response.json() : null
      if (!response.ok) throw new Error(body?.error?.message ?? 'We could not save your setup. Please try again shortly.')
      setWalkthroughSent(true); setWalkthroughStep(0)
    } catch (error) {
      setWalkthroughError(error instanceof Error ? error.message : 'We could not save your setup. Please try again shortly.')
    } finally { setWalkthroughBusy(false) }
  }

  return <div className="landing-page">
    <a className="landing-skip" href="#main-content">Skip to content</a>
    <header className="landing-header">
      <a className="landing-brand" href="/" aria-label="FURII School OS home">
        <span className="landing-brand-mark">F</span>
        <span><strong>FURII</strong><small>SCHOOL OS</small></span>
      </a>
      <button type="button" className="landing-menu-toggle" aria-label={mobileNavOpen ? "Close navigation" : "Open navigation"} aria-expanded={mobileNavOpen} aria-controls="landing-navigation" onClick={() => setMobileNavOpen((open) => !open)}><span /><span /></button>
      <nav id="landing-navigation" className={mobileNavOpen ? "landing-links is-open" : "landing-links"} aria-label="Main navigation">
        <a href="#platform" onClick={() => setMobileNavOpen(false)}>Platform</a><a href="#how-it-works" onClick={() => setMobileNavOpen(false)}>How it works</a><a href="#school-size" onClick={() => setMobileNavOpen(false)}>For every school</a><a href="#trust" onClick={() => setMobileNavOpen(false)}>Trust</a><a href="#walkthrough" onClick={() => setMobileNavOpen(false)}>Get started</a>
      </nav>
      <a className="landing-signin" href="/login" onClick={() => setMobileNavOpen(false)}>Sign in <span aria-hidden="true">↗</span></a>
    </header>

    <main id="main-content">
      <section className="landing-hero">
        <div className="landing-hero-copy">
          <p className="landing-kicker"><span className="kicker-dot" /> SCHOOL OPERATIONS, MADE CLEAR</p>
          <h1>One connected school.<br /><span>Every learner in view.</span></h1>
          <p className="landing-lede">Bring student records, curriculum, assessment, and learning progress into one calmer way to run your school.</p>
          <div className="landing-hero-actions">
            <a className="landing-button landing-button-primary" href="#platform">Explore the platform <span aria-hidden="true">↓</span></a>
            <a className="landing-button landing-button-quiet" href="/login">Staff sign in <span aria-hidden="true">↗</span></a>
          </div>
          <div className="landing-proofline"><span className="proof-check">✓</span> Start with the workflows your school needs today.</div>
        </div>
        <div className="landing-hero-visual" aria-label="Illustrative FURII School OS workspace preview">
          <div className="hero-orbit hero-orbit-one" /><div className="hero-orbit hero-orbit-two" />
          <div className="floating-note note-records"><span className="note-icon blue">↗</span><span><strong>Academic history</strong><small>One learner · one clear record</small></span><b>✓</b></div>
          <div className="floating-note note-skill"><span className="note-icon lime">✳</span><span><strong>Learning evidence</strong><small>Connected to curriculum skills</small></span></div>
          <div className="hero-product">
            <div className="product-topline"><span className="product-logo">F</span><span className="product-school">FURII SCHOOL OS<small>LEARNING WORKSPACE</small></span><span className="product-dots">•••</span></div>
            <div className="product-welcome"><small>SAMPLE WORKSPACE · SCHOOL OVERVIEW</small><strong>Good morning, team.</strong><span>Here’s what’s moving across your school.</span></div>
            <div className="product-metrics"><div><small>LEARNERS</small><strong>642</strong><i>Across 24 classes</i></div><div><small>ASSESSMENTS</small><strong>08</strong><i>Active this week</i></div></div>
            <div className="product-learning">
              <div className="learning-head"><span><b>Learning evidence</b><small>Progress across your classes</small></span><span className="learning-arrow">↗</span></div>
              <div className="learning-chart" aria-hidden="true"><span /><span /><span /><span /><span /><span /><span /><svg viewBox="0 0 420 76" preserveAspectRatio="none"><path d="M2 62 C 42 53, 57 60, 89 44 S 132 52, 166 34 S 210 42, 246 27 S 291 35, 326 17 S 372 24, 418 5" /></svg></div>
              <div className="chart-legend"><span><i /> Skills building</span><span>Illustrative data</span></div>
            </div>
            <div className="product-bottom"><span className="avatar-stack"><i>AM</i><i>TK</i><i>+5</i></span><span>Teachers and learners, connected</span></div>
          </div>
          <div className="visual-caption"><span className="caption-pulse" /> A shared view of learning, not another spreadsheet.</div>
        </div>
      </section>

      <section className="landing-ribbon" aria-label="FURII platform areas">
        <span>ONE SCHOOL WORKSPACE</span><b>STUDENT RECORDS</b><i>✳</i><b>CURRICULUM</b><i>✳</i><b>ASSESSMENT</b><i>✳</i><b>LEARNING EVIDENCE</b>
      </section>

      <section className="landing-section landing-platform" id="platform">
        <Reveal className="landing-section-heading">
          <p className="landing-kicker">BUILT AROUND REAL SCHOOL DAYS</p>
          <h2>Less chasing information.<br /><span>More room to move learning forward.</span></h2>
          <p>FURII brings the everyday work of school into a connected flow—from a learner’s history to the next useful teaching step.</p>
        </Reveal>
        <div className="use-case-grid">
          {useCases.map((item) => <Reveal key={item.number} className="use-case-wrap"><article className="use-case-card">
            <div className="use-case-top"><span>{item.number}</span><i>{item.icon}</i></div>
            <h3>{item.title}</h3><p>{item.text}</p><span className="use-case-line" />
          </article></Reveal>)}
        </div>
      </section>

      <section className="landing-demo-section" aria-labelledby="demo-title">
        <div className="landing-demo-inner">
          <Reveal className="demo-copy">
            <p className="landing-kicker">ONE PLATFORM, CLEAR ROLES</p>
            <h2 id="demo-title">Every role gets a clearer way to work.</h2>
            <p>Choose a view to see how the same school system supports leadership, teaching, and learning.</p>
            <div className="demo-tabs" role="tablist" aria-label="Choose a FURII workspace">
              {views.map((view, index) => <button key={view.key} id={`demo-tab-${view.key}`} type="button" role="tab" aria-selected={activeView === index} aria-controls="demo-panel" onClick={() => setActiveView(index)}>{view.label}<span>{activeView === index ? '↗' : '＋'}</span></button>)}
            </div>
            <div className="demo-description" role="tabpanel" id="demo-panel" aria-labelledby={`demo-tab-${active.key}`} key={active.key}>
              <small>{active.eyebrow}</small><h3>{active.title}</h3><p>{active.description}</p><a href="/login">Open staff sign in <span aria-hidden="true">↗</span></a>
            </div>
          </Reveal>
          <Reveal className="demo-window-wrap">
            <div className="demo-window" aria-label={`Illustrative ${active.label.toLowerCase()} workspace preview`} key={active.key}>
              <div className="demo-window-bar"><span className="window-lights"><i /><i /><i /></span><span>furii.school / workspace</span><span className="demo-window-user">MI</span></div>
              <div className="demo-window-body">
                <aside className="demo-mini-sidebar"><span className="mini-mark">F</span><b className="mini-active">Overview</b><span>Classes</span><span>Learning</span><span>Records</span><i>● ● ●</i></aside>
                <div className="demo-main">
                  <small className="demo-date">YOUR SCHOOL · THIS WEEK</small><h3>{active.screenTitle}<span>Live preview</span></h3>
                  <p className="demo-greeting">Good morning, <b>{active.key === 'learners' ? 'Mekdes' : active.key === 'teachers' ? 'Tigist' : 'school team'}.</b></p>
                  <div className="demo-stats"><div><small>{active.metricOne}</small><strong>{active.valueOne}</strong><i>In this workspace</i></div><div><small>{active.metricTwo}</small><strong>{active.valueTwo}</strong><i>Ready to explore</i></div></div>
                  <div className="demo-insight"><div><span><b>{active.signal}</b><small>Example signal · not live data</small></span><strong>{active.progress}</strong></div><div className="demo-progress"><i style={{ width: active.progress }} /></div><p>Bring evidence together, then choose the next step.</p></div>
                  <div className="demo-tasks"><b>Keep the week moving</b><span><i>✓</i> Review student records <small>School-wide</small></span><span><i>↗</i> Continue classroom learning <small>In progress</small></span></div>
                </div>
              </div>
            </div>
            <p className="demo-disclaimer">Illustrative interface with sample data. No real student information is shown.</p>
          </Reveal>
        </div>
      </section>

      <section className="landing-section landing-steps" id="how-it-works">
        <Reveal className="landing-section-heading">
          <p className="landing-kicker">A PRACTICAL WAY TO GET STARTED</p>
          <h2>Start where you are.<br /><span>Build the connected picture over time.</span></h2>
        </Reveal>
        <div className="steps-grid">
          <Reveal><article className="step-card"><span>01</span><i>⌂</i><h3>Set up your school</h3><p>Organize academic years, classes, and learner records in your school workspace.</p><small>FOUNDATION</small></article></Reveal>
          <Reveal><article className="step-card"><span>02</span><i>⌘</i><h3>Bring teaching together</h3><p>Map curriculum skills, build questions, and invite teachers to their classes.</p><small>TEACHING</small></article></Reveal>
          <Reveal><article className="step-card"><span>03</span><i>✳</i><h3>Learn from the evidence</h3><p>Assign assessments, review responses, and use learning signals to plan what comes next.</p><small>GROWTH</small></article></Reveal>
        </div>
      </section>

      <section className="landing-scale" id="school-size">
        <div className="scale-heading"><Reveal><p className="landing-kicker">MADE TO MEET YOUR SCHOOL WHERE IT IS</p><h2>A useful system for a school of <em>your</em> size.</h2><p>Start with the essentials and add the workflows that make sense for your team.</p></Reveal></div>
        <div className="scale-list">{scaleSteps.map((step) => <Reveal key={step.label}><article className="scale-row"><span className="scale-number">{step.label}</span><h3>{step.title}</h3><p>{step.text}</p><i aria-hidden="true">↗</i></article></Reveal>)}</div>
      </section>

      <section className="landing-trust" id="trust">
        <Reveal className="trust-intro"><p className="landing-kicker">TRUST IS PART OF THE PRODUCT</p><h2>School information deserves care at every step.</h2><p>FURII keeps school workflows role-aware and gives administrators control over who can access their workspace.</p></Reveal>
        <div className="trust-points">
          <Reveal><article><span>01</span><div><h3>Access matched to a role</h3><p>Administrators, teachers, and learners each get a focused workspace.</p></div><b>↗</b></article></Reveal>
          <Reveal><article><span>02</span><div><h3>Review before import</h3><p>Check CSV rows and validation feedback before committing academic history.</p></div><b>↗</b></article></Reveal>
          <Reveal><article><span>03</span><div><h3>Private by design</h3><p>School records are served through the authenticated school API, not public browser database access.</p></div><b>↗</b></article></Reveal>
        </div>
      </section>

      <section className="landing-faq">
        <Reveal><p className="landing-kicker">GOOD QUESTIONS</p><h2>What schools usually ask.</h2></Reveal>
        <div className="faq-list">
          <details><summary>Does FURII work for a small school? <span>＋</span></summary><p>Yes. Start with the school records, classes, and assessments you need now. You can bring in more workflows as the school grows.</p></details>
          <details><summary>Can we bring existing academic history? <span>＋</span></summary><p>Administrators can preview a CSV import, review row-level issues, and commit a validated import as one operation.</p></details>
          <details><summary>Who can use FURII? <span>＋</span></summary><p>School administrators manage the workspace, teachers work with assigned classes, and learners complete assigned assessments and see released feedback.</p></details>
          <details><summary>Can anyone create an account? <span>＋</span></summary><p>Access is provisioned by a school administrator. Public self-sign-up is not enabled.</p></details>
        </div>
      </section>

      <section className="landing-walkthrough" id="walkthrough">
        <div className="walkthrough-orbit" aria-hidden="true" />
        <Reveal className="walkthrough-intro"><p className="landing-kicker">LET’S MAP A FIT FOR YOUR SCHOOL</p><h2>See how FURII could work for your team.</h2><p>Share a few details before you create an account. We’ll prepare a school workspace and invite its administrator once the request is reviewed.</p><ul><li>For small, growing, and busy schools</li><li>No learner records needed</li><li>Your school profile ready before you sign in</li></ul></Reveal>
        <Reveal className="walkthrough-form-wrap">
          {walkthroughSent ? <div className="walkthrough-success" role="status"><span>✓</span><p className="landing-kicker">SCHOOL SETUP SAVED</p><h3>Your school profile is in the queue.</h3><p>FURII will review your details, prepare the school workspace, and send a private one-time administrator invitation. Your school size, region, and chosen priorities will be saved in School Profile before you sign in.</p><button className="landing-button landing-button-quiet" type="button" onClick={() => { setWalkthroughSent(false); setSetup({ name: '', email: '', role: '', school_name: '', school_size: '', timezone: 'Africa/Addis_Ababa', region: 'ET', default_language: 'en', priorities: [], message: '', contact_consent: false, website: '' }) }}>Done</button></div> :
          <form className="walkthrough-form setup-wizard" onSubmit={(event) => void submitWalkthrough(event)}>
            <div className="walkthrough-form-heading"><span>F</span><div><strong>School setup</strong><small>Two quick steps · no account needed yet</small></div></div>
            <div className="wizard-progress" aria-label={'Step ' + (walkthroughStep + 1) + ' of 2'}>
              <button type="button" className={walkthroughStep === 0 ? 'active' : 'complete'} aria-current={walkthroughStep === 0 ? 'step' : undefined} onClick={() => walkthroughStep > 0 && setWalkthroughStep(0)}><span>{walkthroughStep > 0 ? '✓' : '01'}</span>School & priorities</button>
              <button type="button" className={walkthroughStep === 1 ? 'active' : ''} aria-current={walkthroughStep === 1 ? 'step' : undefined}><span>02</span>Contact</button>
            </div>
            {walkthroughStep === 0 && <fieldset className="wizard-step" data-wizard-step="0">
              <legend><span>01 · YOUR SCHOOL</span><strong>Tell us what you’re building.</strong><small>Choose a starting point that fits your school.</small></legend>
              <label>School name<input name="school_name" autoComplete="organization" minLength={2} maxLength={180} required value={setup.school_name} onChange={(e) => updateSetup('school_name', e.target.value)} placeholder="e.g. Blue Nile Academy" /></label>
              <div className="walkthrough-fields-two"><label>Learner count<select name="school_size" required value={setup.school_size} onChange={(e) => updateSetup('school_size', e.target.value)}><option value="" disabled>Select range</option><option value="1-100">1–100</option><option value="101-500">101–500</option><option value="501-1500">501–1,500</option><option value="1501+">More than 1,500</option></select></label><label>Country or region<input name="region" required maxLength={80} value={setup.region} onChange={(e) => updateSetup('region', e.target.value)} placeholder="Ethiopia" /></label></div>
              <div className="walkthrough-fields-two"><label>Timezone<select name="timezone" required value={setup.timezone} onChange={(e) => updateSetup('timezone', e.target.value)}><option value="Africa/Addis_Ababa">Addis Ababa</option><option value="Africa/Nairobi">Nairobi</option><option value="Africa/Kampala">Kampala</option><option value="Africa/Lagos">Lagos</option><option value="Africa/Cairo">Cairo</option><option value="Europe/London">London</option><option value="America/New_York">New York</option><option value="UTC">UTC</option></select></label><label>Default language<select name="default_language" value={setup.default_language} onChange={(e) => updateSetup('default_language', e.target.value)}><option value="en">English</option><option value="am">Amharic</option><option value="om">Afaan Oromo</option><option value="ti">Tigrinya</option><option value="sw">Swahili</option><option value="ar">Arabic</option><option value="fr">French</option></select></label></div>
              <div className="priority-heading"><strong>What do you want to improve first?</strong><small>Choose one or more</small></div>
              <div className="priority-options">{[['student-records','Learner records'],['curriculum','Curriculum'],['assessment','Assessment'],['learning-progress','Learning progress'],['staff-coordination','Staff coordination'],['academic-history','Academic history'],['family-communication','Family communication']].map(([value,title]) => <label className={'priority-option ' + (setup.priorities.includes(value) ? 'selected' : '')} key={value}><input type="checkbox" checked={setup.priorities.includes(value)} onChange={(e) => updateSetup('priorities', e.target.checked ? [...setup.priorities, value] : setup.priorities.filter((item) => item !== value))}/><span className="priority-check">{setup.priorities.includes(value) ? '✓' : '+'}</span><strong>{title}</strong></label>)}</div>
              <label>Anything else? <span className="walkthrough-optional">Optional</span><textarea name="message" rows={3} maxLength={3000} value={setup.message} onChange={(e) => updateSetup('message', e.target.value)} placeholder="A challenge or goal for your school…" /></label>
            </fieldset>}
            {walkthroughStep === 1 && <fieldset className="wizard-step" data-wizard-step="1">
              <legend><span>02 · SCHOOL CONTACT</span><strong>Who should we prepare access for?</strong><small>This person receives the administrator invitation after review.</small></legend>
              <label>Your name<input name="name" autoComplete="name" minLength={2} maxLength={120} required value={setup.name} onChange={(e) => updateSetup('name', e.target.value)} placeholder="e.g. Hana Bekele" /></label>
              <div className="walkthrough-fields-two"><label>Work email<input name="email" type="email" autoComplete="email" maxLength={254} required value={setup.email} onChange={(e) => updateSetup('email', e.target.value)} placeholder="you@school.org" /></label><label>Your role<input name="role" maxLength={80} required value={setup.role} onChange={(e) => updateSetup('role', e.target.value)} placeholder="School leader" /></label></div>
              <label className="walkthrough-consent"><input name="contact_consent" type="checkbox" required checked={setup.contact_consent} onChange={(e) => updateSetup('contact_consent', e.target.checked)} /><span>FURII may contact me about setup. I have not included private learner information.</span></label>
              <label className="walkthrough-honeypot" aria-hidden="true">Leave this field blank<input name="website" tabIndex={-1} autoComplete="off" value={setup.website} onChange={(e) => updateSetup('website', e.target.value)} /></label>
              <div className="setup-review"><strong>{setup.school_name || 'Your school'}</strong><span>{setup.school_size || 'Learner count'} learners · {setup.region || 'Region'} · {setup.timezone.replace('Africa/','')}</span><small>Priorities: {setup.priorities.join(', ') || 'None selected'}</small></div>
            </fieldset>}
            {walkthroughError && <p className="walkthrough-error" role="alert">{walkthroughError}</p>}
            <div className="wizard-actions">{walkthroughStep > 0 && <button className="landing-button landing-button-quiet" type="button" onClick={() => { setWalkthroughError(''); setWalkthroughStep(0) }}>Back</button>}{walkthroughStep === 0 ? <button className="landing-button landing-button-primary walkthrough-submit" type="button" onClick={(event) => { const form = event.currentTarget.form; if (form) nextStep(form) }}>Continue <span aria-hidden="true">→</span></button> : <button className="landing-button landing-button-primary walkthrough-submit" type="submit" disabled={walkthroughBusy}>{walkthroughBusy ? 'Saving…' : 'Save my school setup'} <span aria-hidden="true">→</span></button>}</div>
            <small className="walkthrough-privacy">No learner records are requested. A FURII operator reviews each request before activating the school workspace.</small>
          </form>}
        </Reveal>
      </section>

      <section className="landing-final">
        <div className="final-orbit" />
        <Reveal><p className="landing-kicker">MAKE THE NEXT SCHOOL DAY CLEARER</p><h2>Give your school one place<br />to move learning forward.</h2><p>Talk with us about a starting point that fits your school.</p><a className="landing-button landing-button-primary" href="#walkthrough">Set up your school <span aria-hidden="true">↓</span></a></Reveal>
        <div className="final-aside"><span>F</span><small>FURII SCHOOL OS<br />MADE FOR LEARNING</small></div>
      </section>
    </main>

    <footer className="landing-footer"><a className="landing-brand" href="/"><span className="landing-brand-mark">F</span><span><strong>FURII</strong><small>SCHOOL OS</small></span></a><span>One connected school. Every learner in view.</span><a href="/login">Staff sign in ↗</a><span>© {new Date().getFullYear()} FURII School OS</span></footer>
  </div>
}
