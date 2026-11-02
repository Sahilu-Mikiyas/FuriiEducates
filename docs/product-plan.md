# FURII School OS — product plan

Written 2026-11-01. Companion to [system-analysis.md](system-analysis.md), which covers code quality and technical risks. This document covers the product: what exists, what is missing, the national exam opportunity, and every school operation the system should eventually run.

---

## 1. Where we are

### What the system does today

| Area | Working today | Gaps |
|---|---|---|
| Onboarding | Landing-page setup request, operator prepares school and admin invite | No email delivery; links copied by hand |
| School setup | School profile, academic years, classes | No terms, no Ethiopian calendar, no editing or archiving |
| Students | Create students, enroll at creation, learner invites | No guardians, no documents, no transfers, no search, 200-row limit |
| Academic history | Prior school results, external exams, CSV import | No verification workflow, no edit or delete |
| Curriculum | Curricula, subjects, skill trees | Shared across schools (security bug P0-1) |
| Question bank | Four question types, publishing with source and license | Raw JSON answer keys; only admins can author |
| Assessments | Timed exams, snapshots, autosave, auto-grading, teacher grading of written answers | Questions visible before start, no auto-finalize, no due dates, no gradebook |
| Learning insight | Skill mastery estimates and study recommendations | Simple rule; no item analysis |
| Teachers | Rosters, grading queue, lesson plans, class resources | Cannot author questions or assessments |
| Finance | Ledger of charges, payments, expenses, salaries | No invoices, receipts, payment gateway, void, or multi-currency totals |
| Staff | Teacher invites, class assignment, disable access | No HR records, leave, payroll calculation |

### Completed so far

- **System audit** (2026-10-31): every file reviewed, problems ranked P0 to P2, phased fix plan.
- **Phase 0, safety net** (2026-11-01, pushed to main): first automated tests, CI quality gates, dependency upgrades, and two production exam bugs fixed. Autosave had always failed, and timed-out students lost their answers.
- **Phase 1, security and tenancy** (2026-11-01, pushed to main): school-private curricula, first-party login cookie, password change and reset links, rate limits.
- **Phase 2, exam correctness and secure exams v1** (2026-11-02): hidden questions until start, server-side finalizing, due dates, gradebook, admin grading, shuffling, start codes, invigilator console, locked mode.

### Foundation work still required before real schools

These come from the system analysis and are non-negotiable before real student data:

1. **Phase 3, frontend restructure:** split the single large screen file into routed pages. Every module below depends on this.

Phases 1 and 2, listed under Completed so far, were the other two requirements.

---

## 2. The national exam opportunity

### What is happening

These facts come from news and education sources listed at the end. Verify them against EAES and Ministry of Education announcements each exam cycle.

- **The exam:** the Grade 12 national exam (ESSLCE), run by the Educational Assessment and Examinations Service (EAES), decides university entry.
- **The format:** six multiple-choice papers of 100 marks each, 600 in total. Every student sits English, Mathematics and the Scholastic Aptitude Test. Natural science students add Physics, Chemistry and Biology. Social science students add History, Geography and Economics. Questions cover the Grade 9 to 12 curriculum, with four options per question.
- **The pass line:** 50%, or 300 of 600, for direct public university admission. The cutoff can shift each cycle.
- **The shift to computers:** the 2025 exam used both paper and computer-based testing. In 2026 the Ministry moved to fully online exams in Addis Ababa, Dire Dawa and Harar, with paper as a fallback where infrastructure was missing. About 560,000 to 700,000 students sit each year.
- **The results gap:** in the 2018 E.C. results, about 18% of computer-based candidates passed, against about 4% of paper candidates. The national pass rate was 12.8%. The groups differ in resources, so the gap is not proof that format alone helps.
- **The school pain:**
  - Lawmakers raised unreliable power, poor connectivity and limited computers.
  - Schools have been asking district offices for computers.
  - Research on computer-based exit exams found students struggling with basic operations such as moving between questions.
  - The Ministry promised training and mock exams but gives schools no tool to run their own.
- **Timing:** the next exam is expected around late June 2027. Schools will want practice tools from early 2027 at the latest, so exam prep is the most time-sensitive feature on this plan.

### What schools need from us

Three problems, three answers:

| School problem | Our answer |
|---|---|
| Students have never used a computer exam | A practice environment that looks and behaves like a real computer-based exam, plus basic computer-skills lessons |
| Teachers cannot see who is ready | Readiness analytics with a predicted score out of 600 per student, class and stream |
| Schools cannot run their own secure computer exams | A secure exam module for school mocks, midterms and finals that works even when the internet does not |

### Module A: national exam preparation

1. **Exam-format simulator.** Full-screen timed papers with four-option questions, question navigator, flag for review, on-screen clock and a submit-confirmation screen. These are the controls students meet on exam day. Do not claim to replicate the official software; describe it as practice in the same format.
2. **Computer-skills onboarding.** Short guided lessons before the first mock: using a mouse and keyboard, scrolling, moving between questions, flagging, and what happens when time runs out. This targets the skill gap the research found.
3. **Grade 9 to 12 question bank per stream.** Every question tagged to subject, grade, unit, topic and skill, using the existing skill-tagging engine. Content sources:
   - teacher-written questions, with review and approval;
   - licensed partner content;
   - past papers, only where reuse rights are confirmed.
4. **Practice modes:**
   - topic drills with instant explanations;
   - timed single-paper mocks;
   - full mock national exams (six papers, scored out of 600);
   - adaptive practice that targets each student's weakest skills;
   - spaced review of questions answered wrong.
5. **Readiness analytics:**
   - for each student, a predicted score range out of 600, distance to the 300 line, and weakest topics per subject;
   - for teachers, class heatmaps by topic, and students at risk of falling below 300;
   - for school leaders, readiness by stream and section, and the trend across mock rounds.
6. **School mock-exam days.** Administrators schedule a school-wide mock with the real six-paper timetable and stream split, and results roll up to readiness dashboards.
7. **Works on weak connections.** Practice papers download to a phone or lab computer, answers sync later, and the mobile web app installs as an app. Notifications go out by SMS or Telegram.
8. **Parent view.** Parents see readiness and mock results in plain language.
9. **Later, an AI tutor.** Step-by-step explanations and similar practice questions. Teachers review AI-generated content before students see it.

### Module B: secure online school exams

This turns the existing assessment engine into an exam system schools can trust for real grades.

1. **Exam setup.**
   - A blueprint says how many questions come from each topic and at what difficulty.
   - Several versions are generated from the bank, with shuffled question and option order per student.
   - Exams are scheduled with a time window, a room and a seating plan.
2. **Access control.**
   - Questions are released only after an invigilator opens the session, and the student enters a start code shown in the room.
   - One active session per student, restricted to the school network when required.
   - Answer keys never leave the server, and the server keeps the official time.
3. **Integrity monitoring.**
   - The exam runs in a full-screen locked mode that flags leaving the window, switching tabs, pasting, and unusually fast answering.
   - An invigilator console shows each student's live status and lets staff extend time, pause a student, or log an incident.
   - A complete audit trail records every action.
   - Later: locked-down Windows and Android exam apps for higher-stakes exams.
4. **Resilience for Ethiopian conditions.** This is the key differentiator.
   - Answers already save on the device first and sync when the connection returns. This exists and was fixed in Phase 0.
   - Next, an on-site exam server mode: a school laptop or small server runs exams over the school's local network with no internet, then uploads results.
   - Exams pause cleanly during power cuts and resume with the remaining time.
5. **Marking and results.**
   - Objective answers are graded automatically, and written answers go to a marking queue with rubrics.
   - Item analysis shows each question's difficulty and whether it separates strong and weak students. Bad questions are flagged for review.
   - A moderation step comes before release. Results flow into report cards.

---

## 3. Every school operation, as modules

Status key: **Exists**, **Partial**, **New**. Each module lists the operations and what the system can automate.

### 3.1 Admissions and registration (New)
- **Operations:** inquiries, online application forms, document upload (birth certificate, prior report cards, photos, transfer letters), entrance tests, interview scheduling, acceptance and waitlist decisions, registration fees, seat and section allocation.
- **Automate:**
  - application status messages to parents;
  - entrance test delivery and scoring through the exam engine;
  - waitlist promotion when a seat frees;
  - turning an accepted applicant into a student record with an ID number, guardians and a first invoice.

### 3.2 Student information (Partial)
- **Operations:** student profiles, guardians and emergency contacts, medical notes, special needs, documents, photos, ID cards, transfers in and out, promotion and repetition, graduation, alumni.
- **Automate:**
  - ID card generation with QR codes;
  - year-end promotion based on results and promotion rules;
  - transfer certificates;
  - duplicate detection;
  - bulk import from spreadsheets.

### 3.3 Academic structure and calendar (Partial)
- **Operations:** academic years, terms or semesters, grades, sections, natural and social science streams, subjects per grade, teacher assignments, school calendar, holidays and events.
- **Automate:** the Ethiopian calendar (Meskerem to Sene) shown alongside the Gregorian calendar, national holidays, and rollover of classes and subjects into the next year.

### 3.4 Timetable (New)
- **Operations:** class timetables, teacher timetables, room allocation, substitutions, exam timetables.
- **Automate:**
  - timetable generation from teacher load, room and period constraints;
  - substitute suggestions when a teacher is absent;
  - conflict detection.

### 3.5 Attendance (New)
- **Operations:** daily and per-period student attendance, lateness, excused absences, staff attendance.
- **Automate:**
  - one-tap class registers on a phone;
  - QR or card check-in;
  - same-day SMS to parents for unexplained absence;
  - chronic-absence alerts;
  - monthly attendance reports.

### 3.6 Teaching and learning (Partial)
- **Operations:** lesson plans (exists), class resources (exists), homework and assignments, submissions, feedback, curriculum coverage tracking, department planning.
- **Automate:** homework reminders, late-submission tracking, coverage reports comparing plans to the syllabus, and copying plans to the next year.

### 3.7 Assessment, grading and report cards (Partial)
- **Operations:** continuous assessment (tests, quizzes, assignments, projects), mark entry, weighting rules, grade rosters, ranking, report cards, transcripts, promotion decisions.
- **Automate:**
  - weighted totals and grades from the school's rules;
  - class rank;
  - report card PDFs in the school's language;
  - missing-mark alerts to teachers;
  - transcript generation.

### 3.8 National exam preparation (New)
Module A above.

### 3.9 Secure examinations (Partial)
Module B above.

### 3.10 Student welfare and activities (New)
- **Operations:** discipline and behaviour records, counselling notes (restricted access), clinic visits, special needs plans, clubs, sports, events.
- **Automate:** parent notification of serious incidents, follow-up reminders for counsellors, club membership and attendance.

### 3.11 Communication and parent portal (New)
- **Operations:** announcements, class news, parent and teacher messaging, meeting scheduling, emergency alerts, circulars.
- **Automate:**
  - SMS, Telegram and email delivery from one place;
  - audience targeting by grade, class or fee status;
  - delivery tracking;
  - meeting booking;
  - a parent portal showing attendance, marks, fees, readiness and messages.

### 3.12 Finance and fees (Partial)
- **Operations:** fee structures per grade, discounts and scholarships, invoices, payment collection, receipts, arrears, refunds, expenses, budgets, cash book, financial reports.
- **Automate:**
  - term invoices generated from fee structures;
  - mobile money payments through Telebirr, CBE Birr or a gateway such as Chapa, with automatic matching;
  - instant digital receipts;
  - arrears reminders;
  - optional exam or report-card holds for unpaid fees;
  - income and expense reports.

### 3.13 HR and payroll (New)
- **Operations:** recruitment, staff records, contracts, qualifications, leave, staff attendance, evaluations, training, payroll.
- **Automate:**
  - payroll with Ethiopian income tax brackets and pension contributions (verify current rates with an accountant);
  - payslips;
  - leave balances and approvals;
  - contract-expiry reminders.

### 3.14 Library (New)
- **Operations:** catalogue, borrowing and returns, reservations, fines, digital books.
- **Automate:** barcode checkout, due-date reminders, overdue fines added to the student's account.

### 3.15 Inventory, assets and procurement (New)
- **Operations:** lab equipment, computers, furniture, consumables, purchase requests, suppliers, maintenance.
- **Automate:** low-stock alerts, an asset register with QR tags, approval workflows for purchases, maintenance schedules.

### 3.16 Transport (New)
- **Operations:** routes, stops, vehicles, drivers, student assignments, transport fees.
- **Automate:** route fees on invoices, boarding check-in, delay alerts to parents, and later live bus location.

### 3.17 Cafeteria and boarding (New)
- **Operations:** meal plans, menus, dormitory rooms, bed allocation, boarder attendance, leave passes.
- **Automate:** meal-plan billing, room allocation, night roll call, and parent notification of leave passes.

### 3.18 Front office and security (New)
- **Operations:** visitor log, student pick-up authorisation, gate passes.
- **Automate:** pick-up checks against authorised guardians, and visitor badges.

### 3.19 Reporting and compliance (New)
- **Operations:** Ministry and regional reports (EMIS statistics), enrolment and staffing reports, national exam registration data, audits.
- **Automate:** EMIS-ready exports, national exam candidate lists by stream, and scheduled reports to school owners.

### 3.20 Analytics and early warning (Partial)
- **Operations:** school dashboards, academic trends, at-risk students.
- **Automate:** an early-warning score combining attendance, marks and behaviour, plus weekly digests for principals.

### 3.21 Platform foundations (Partial)
- **Branches:** multiple schools and branches under one owner.
- **Roles:** granular roles and permissions.
- **Languages:** English, Amharic, Afaan Oromo and Tigrinya.
- **Mobile and offline:** a mobile web app that works offline.
- **Data protection:** privacy controls aligned with Ethiopia's personal data protection law (confirm the current proclamation with a lawyer).
- **Operations:** backups, single sign-on, and a full audit log.

### Roles the system must support

- **Platform:** FURII operator.
- **Leadership:** school owner, director, vice principal or academic head, department head.
- **Administration:** registrar, admissions officer, finance officer or cashier, accountant, HR officer.
- **Teaching:** homeroom teacher, subject teacher, invigilator.
- **Support services:** librarian, nurse, counsellor, transport coordinator, storekeeper, front office and gate staff.
- **Families:** student, parent or guardian.

---

## 4. Recommended order

| Step | Contents | Why this order |
|---|---|---|
| 1. Phase 1 | Security and tenancy fixes | Required before any real school data |
| 2. Phase 2, merged with secure exams v1 | Exam correctness fixes plus versions, start codes, invigilator console, locked mode | Same engine; finishing it properly gives us Module B's core |
| 3. Phase 3 | Frontend restructure, plus languages and the Ethiopian calendar | Every later module needs this base |
| 4. National exam prep v1 | Simulator, computer-skills lessons, stream question banks, mock exams, readiness analytics, mobile offline practice | Main selling point; must be live before schools start 2027 exam preparation |
| 5. Core school operations | Admissions, student information, attendance, continuous assessment and report cards, parent portal with SMS | What every school needs daily; makes FURII the system of record |
| 6. Money and people | Fee structures, mobile money, receipts, HR and payroll | High value for owners; needs payment partner agreements |
| 7. Operations | Timetable, library, inventory, transport, cafeteria and boarding, front office | Broadens the platform for larger schools |
| 8. Intelligence and scale | Early warning, AI tutor, on-site exam server, Ministry reporting, branches | Builds on data collected by earlier modules |

Steps 1 to 4 should be the focus between now and early 2027.

---

## 5. Decisions needed from the team

1. **Exam content:** where the Grade 9 to 12 question bank comes from (own teachers, partners, licensed past papers) and who reviews it.
2. **Offline hardware:** whether we offer an on-site exam server, and on what hardware.
3. **Payments:** which payment provider to integrate first.
4. **Languages:** which languages to launch with, and who translates.
5. **Pricing:** per student, per module, or exam prep sold separately.
6. **Hosting:** moving the API off Render's free tier before any pilot.
7. **Pilot schools:** which schools pilot exam prep, and when.

---

## Sources

National exam facts were gathered on 2026-11-01. Several are secondary reports; confirm them against official EAES and Ministry of Education announcements.

- [Ethiopian Monitor: Grade 12 exit exam schedule (2025)](https://ethiopianmonitor.com/2025/05/11/grade-12-exit-exam-schedule-announced/)
- [Ethiopian Monitor: Grade 12 results released (2026)](https://ethiopianmonitor.com/2026/08/25/education-authorities-release-grade-12-exam-results/)
- [Addis Insight: 2018 E.C. pass rate rises to 12.8%](https://addisinsight.net/2026/08/23/ethiopias-grade-12-national-exam-pass-rate-rises-to-12-8-what-education-minister-berhanu-nega-said/)
- [Addis Insight: results and checking scores](https://addisinsight.net/2026/08/25/ethiopian-grade-12-national-exam-results-released-how-to-check-your-score/)
- [Ethiopia Today: schedule for over 563,000 students](https://ethiopiatoday.net/ethiopia-adjusts-national-exam-schedule-for-over-563000-grade-12-students/)
- [The Reporter: digital exam scheme and parliamentary concerns](https://www.thereporterethiopia.com/50289/)
- [Nuffic: Ethiopian secondary education and ESSLCE subjects](https://www.nuffic.nl/en/education-systems/ethiopia/primary-and-secondary-education)
- [Atenu ESSLCE hub: streams, format and preparation features](https://esslce.atenu.org/)
- [Temari: exam format, six papers, 600 marks](https://temari.et/blog/grade-12-exam-format-and-marking)
- [Temari: 2026 dates and results](https://temari.et/blog/grade-12-national-exam-2026)
- [Research: computer-based exit examination in Ethiopia](https://www.researchgate.net/publication/403078356_COMPUTER-BASED_EXIT_EXAMINATION_IN_ETHIOPIA_PROSPECTS_CHALLENGES_AND_STRATEGIES)
- [Ethiopia Insight: exam reforms commentary](https://www.ethiopia-insight.com/2026/08/25/exam-reforms-arent-fixing-ethiopias-institutional-decay/)
- [Existing prep platforms for comparison: TikuretEntrance](https://tikuretentrance.com/)
