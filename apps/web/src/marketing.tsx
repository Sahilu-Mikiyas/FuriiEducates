import { useEffect, useState, type ReactNode } from 'react'
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

  return <div className="landing-page">
    <a className="landing-skip" href="#main-content">Skip to content</a>
    <header className="landing-header">
      <a className="landing-brand" href="/" aria-label="FURII School OS home">
        <span className="landing-brand-mark">F</span>
        <span><strong>FURII</strong><small>SCHOOL OS</small></span>
      </a>
      <nav className="landing-links" aria-label="Main navigation">
        <a href="#platform">Platform</a><a href="#how-it-works">How it works</a><a href="#school-size">For every school</a><a href="#trust">Trust</a>
      </nav>
      <a className="landing-signin" href="/login">Staff sign in <span aria-hidden="true">↗</span></a>
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
            <div className="product-welcome"><small>MONDAY · SCHOOL OVERVIEW</small><strong>Good morning, team.</strong><span>Here’s what’s moving across your school.</span></div>
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
            <h2 id="demo-title">Everyone sees what helps them do their best work.</h2>
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

      <section className="landing-final">
        <div className="final-orbit" />
        <Reveal><p className="landing-kicker">MAKE THE NEXT SCHOOL DAY CLEARER</p><h2>Give your school one place<br />to move learning forward.</h2><p>See the workspace through your school’s staff sign-in.</p><a className="landing-button landing-button-primary" href="/login">Go to staff sign in <span aria-hidden="true">↗</span></a></Reveal>
        <div className="final-aside"><span>F</span><small>FURII SCHOOL OS<br />MADE FOR LEARNING</small></div>
      </section>
    </main>

    <footer className="landing-footer"><a className="landing-brand" href="/"><span className="landing-brand-mark">F</span><span><strong>FURII</strong><small>SCHOOL OS</small></span></a><span>One connected school. Every learner in view.</span><a href="/login">Staff sign in ↗</a><span>© {new Date().getFullYear()} FURII School OS</span></footer>
  </div>
}
