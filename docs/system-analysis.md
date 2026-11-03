# FURII School OS — full system analysis

Analysis date: 2026-10-31. Revision analysed: `5d0bfc1` (main, after PR #8 "finance, lesson plans, and class resources").

Method: every tracked source file was read (Go API, SQL migrations, React app, CI, deploy config, docs). The API and web app were built locally, `go vet` and `govulncheck` were run, `npm audit` was run, and the live deployment was probed with read-only requests.

This document is the working roadmap. Update it as items are fixed.

## Progress log

**2026-11-01 — Phase 0 (safety net) completed, uncommitted on main.**

- Test suite added: unit tests for grading, question validation, CSV numbers, passwords, and config, plus database integration tests covering authentication, role boundaries, malformed IDs, school isolation, teacher class scoping, the full assessment lifecycle, and deadline handling. Known issues P0-1 and P0-4 have skipped tests waiting for their fixes.
- The tests found two production bugs in the exam flow, both fixed:
  - **Autosave always failed** with HTTP 500 because the idempotency-key query compared a UUID column with an empty string. Submission still worked because the browser resends every answer, but a deadline auto-submit keeps only server-saved answers, so timed-out students lost all their work.
  - **Submission could fail** with HTTP 500 because one parameter was inferred as two different types. The idempotency key is now validated as a UUID.
- P0-5 fixed: pgx 5.12.0, x/text 0.43.0, x/crypto 0.58.0, Go toolchain pinned to 1.27.3, npm versions pinned. `govulncheck` reports 0 reachable vulnerabilities.
- P1-2 fixed: malformed route IDs return 404, and malformed IDs in request bodies return 400. The four copy-pasted role guards were merged into one.
- CI now checks module tidiness, gofmt, go vet, govulncheck, the test suite against Postgres, and npm audit.
- README, OpenAPI spec (all 78 routes), and migration docs refreshed. Three Go files were gofmt-formatted.

Next: Phase 1 (security and tenancy).

**2026-11-01 — Phase 1 (security and tenancy) completed.**

- P0-1 fixed: curricula and curriculum mappings now belong to a school (migration 00014). Other schools cannot read, extend, map, or tag questions with them. Curricula without a school are a read-only platform library. Existing data is backfilled from the audit log, or to the only school on single-school installations; tests cover both cases.
- P0-2 fixed: the frontend calls the API through a Vercel `/api` proxy, so the session cookie is first-party and Safari and iOS sign-in work. `COOKIE_SAMESITE` makes the cookie mode configurable (switch Render to `lax` after deploying). The CSRF check is explicit: unknown origins and cross-site fetch metadata are rejected on unsafe requests. The three duplicated frontend API clients were merged into one.
- P0-3 fixed: self-service password change (signs out other devices), admin-issued 24-hour reset links for teachers and learners, and operator-issued links for locked-out school administrators. Verified end to end in a browser.
- P0-6 fixed: per-address limits on login, link redemption, and the public setup form, plus a per-account failed-login lock. Per-address limits stay generous because a computer lab shares one public address.
- Expired sessions and old reset links are purged hourly.
- The school setup requests page is hidden from administrators who are not platform operators, and invitation pages now lead to the sign-in page.
- Browser testing found that the dev proxy needs `CORS_ALLOWED_ORIGINS=http://localhost:5173`; the example environment file now says so.

Next: Phase 2 (exam correctness), merged with secure exams v1 per docs/product-plan.md.

**2026-11-02 — Phase 2 (exam correctness and secure exams v1) completed.**

- P0-4 fixed: questions are returned only after an attempt starts. Abandoned attempts are graded from saved answers by a server job every minute, and when next opened. Answers in flight at the deadline get a 30-second grace window, and the browser now sends queued answers before its deadline submission. Assignments can have opening and due times.
- Score totals now come from the attempt snapshot rather than live assessment items (P1-11). The start-attempt response uses the normal data envelope (part of P1-13).
- Administrators can grade written answers when a class has no teacher (P1-3). Teachers and administrators have a gradebook with CSV download (P1-4).
- Secure exams v1 (Module B in docs/product-plan.md): per-student question and option shuffling, invigilated sessions with start codes and limited guesses, an invigilator console with live status, added time, incidents, timelines and forced finish, and a locked mode that records leaving the exam window as signals for review.
- Migration 00015 adds the exam settings. Tests cover all of the above, and the whole flow was exercised in a browser.
- CI fixes: `go.mod` now requires Go 1.27.3, and govulncheck moved to v1.9.0 because v1.1.4 crashes on Linux with Go 1.27. Earlier CI runs never reached the test steps.

Not yet done from Module B: exam blueprints that draw from the bank by topic and difficulty, item analysis, moderation before release, an on-site exam server, and locked-down native apps.

**2026-11-02 — Phase 3 (frontend restructure, languages, Ethiopian calendar) completed.**

- `apps/web/src/main.tsx` went from 90 KB to a small entry file. Every screen is now its own routed page under `src/pages`, shared features are under `src/features`, and each workspace downloads only when opened. Administrators use `/admin/*`, teachers `/teach/*`, and learners `/learn/*`.
- React Router handles navigation and TanStack Query handles shared data (`src/lib/queries.ts`), so `App` no longer loads every record at sign-in. An expired session now returns to the sign-in page, and signing out clears all cached records, which matters on shared lab computers.
- A user who is both administrator and teacher can now reach both workspaces.
- The app has English and Amharic throughout, except as noted below. The choice is saved per device, and otherwise follows the school's default language (`default_language` on `/me`), then the browser. The Amharic text is a first draft and needs a native speaker's review before schools rely on it.
- Dates show the Ethiopian and Gregorian calendars together, with a per-device choice to show one. Date fields accept either calendar and always send Gregorian dates to the API. Conversion is tested against known dates and every day across 400 years.
- Browser testing found and fixed: text as small as 8px, which is unreadable in Ge'ez script (nothing is below 11px now); a sign-out button pushed below the visible sidebar and missing on phones; unstyled exam status and question buttons; phone sidebars that covered the screen.
- New web tests run in CI (`npm test`): calendar conversion, and Amharic matching English keys and placeholders.

Not yet done: Afaan Oromo and Tigrinya (need translators); the public landing page is English only; field validation messages from the API stay in English; the Ethiopian clock (hours counted from 6 a.m.) and national holidays; generating API types from OpenAPI (types in `src/lib/types.ts` are hand-written); moving the exam console, gradebook and grading queue onto the shared data layer; component-level web tests.

**2026-11-02 — National exam prep v1, first slice, built.**

- Question bank: teachers write four-option questions by paper, grade and topic with a simple form or a CSV import. An administrator approves them before students see them. Teachers see answers only for questions they wrote. Exam-only questions stay out of practice so school mocks can use unseen questions.
- Practice: drills with instant explanations, weak-topic practice driven by skill mastery, review of wrong answers after a day, and timed papers that show nothing until they finish. Timed answers save on the device first and sync. Expired papers and abandoned practice are closed by the existing minute job.
- Simulator: a full-window exam screen with one question at a time, a clock, a navigator, flagging and a submit screen listing what is unanswered. School mock papers open in it too.
- Computer-skills lessons: six guided lessons on the mouse, scrolling, answering, moving, flagging and the clock, using sample questions. Teachers can see who finished.
- Full mocks: six timed papers for the student's stream, scored out of 600. School mock papers are drafted from the bank and run on the secure exam engine.
- Readiness: per-paper estimates with a range, a total out of 600 withheld until all six papers have 15 answers, distance from the 300 line, weak topics, a class table and heatmap, and a school view by stream and class. Estimates describe work on the school's own questions and are labelled as not predicting official results.
- Migration 00016 adds the tables. Practice now feeds skill evidence at half the weight of timed work. New tests cover approval, answer secrecy, grading, expiry, tenancy, mocks and readiness. The student and teacher flows were exercised in a browser with local test questions only. The administrator approval buttons, CSV upload screen and mock paper form were covered by API tests but not clicked through.

Not yet done: parent view, SMS or Telegram notices, Afaan Oromo and Tigrinya, and an AI tutor. There are no real exam questions yet: that needs decision 1 in docs/product-plan.md. The 120-minute default for a timed paper is our assumption and should be checked against the official timetable.

**2026-11-02 — Offline practice and app install built.**

- The app installs on phones and computers and opens with no connection. A service worker stores the app's files only, never API data. New releases download only changed files, and an update waits until no exam or practice is open.
- Students download practice per paper. Each pack holds up to 30 practice questions with answers so the device can mark them. Exam-only questions are never included, because anything downloaded should be treated as readable by the student.
- Drills and client-timed papers run with no connection and save every change on the device. On reconnecting, finished work uploads and the server marks it again, ignoring what the device concluded. Repeating an upload changes nothing. Offline work counts at the practice weight, even when timed, because no server watched the clock.
- A student stays signed in offline from their last visit, and the server checks again on reconnecting. Signing out with no connection forgets the student and removes downloaded questions at once, and ends the server session on the next connection, so a shared computer is safe. Staff get no offline sign-in.
- Migration 00017 adds the upload ID. The in-app test browser cannot run service workers, so offline was tested in Microsoft Edge with a throwaway profile, including truly offline, reloading mid-practice, uploading, offline sign-out, and servers being down.

Limits: secure exams and online timed papers cannot be reopened while offline, though their saved answers stay on the device and sync later. Downloaded questions and answers sit unencrypted in browser storage. Installing on real phones, including the iPhone Add to Home Screen path, has not been tested.

**2026-11-02 — School mock days built.**

- An administrator schedules a mock day once: name, classes, streams, questions per paper, and a time and length for each paper. It creates one published paper per subject on the secure exam engine, with shuffled questions, full screen, and optional start codes.
- Stream papers reach only that stream's students, even in a mixed class. Direct links to the wrong stream's paper are refused, and invigilator counts and rosters match. A student with no stream sees only the common papers, and the administrator is told how many on scheduling.
- Timetables are checked: all three common papers, whole streams only, and no overlap for one student counting the time allowed to start and the paper's length. Natural and social papers may share a time. Every paper short of questions is listed at once and nothing is created. Questions are drawn exam-only first, then least used in earlier mock days.
- A paper can move, and a mock day can be cancelled, only until students start.
- Results roll up by student, stream, class and paper, with a CSV download and a trend across mock days. Totals count only students who sat every paper. Teachers see only their classes. Students see their timetable, scores and total out of 600. Results also feed readiness.
- Migration 00018 adds the tables. New API tests cover all of this, and the whole flow ran in Edge against a production build, from scheduling through sitting all six papers to teacher and administrator results.

Limits: the mock day has one timetable for all chosen classes, with no rooms or seating. Invigilators open each class's session one at a time. The trend compares different questions, so it is labelled as a rough guide.

**2026-11-02 — Core operations started: student records and attendance built.**

- The students list now searches by name or ID, filters by class and pages through results, so it works for a whole school. Without a page number the API still returns the full list that pickers use, up to 5,000.
- Schools can import students from a CSV, with Ethiopian or Gregorian birth dates, class matching by grade and section, and a guardian on each row. Any bad row saves nothing and is named. A student number already in use is refused, and the same name and birth date is held back unless the school confirms.
- Each student has a profile: birth date, gender, address, medical notes and guardians. The first guardian becomes the main contact, and there is always one while any guardian remains. Administrators edit. A teacher of the student's current class reads it, because medical notes matter in the classroom. Medical notes stay out of the audit log.
- A student can move class. Their attendance and results stay with the class they were earned in.
- Teachers and administrators take a daily class register with one tap per student and a "mark the rest present" button. Marks are kept as a draft while switching class or date on the page. Teachers can correct the last 14 days and administrators any past day. Future dates are refused, allowing one day for the school's time zone being ahead of the server.
- The office sees which registers are in, and a list of absent students with their main contact's number to tap. Excused students are left out.
- Repeated absence is flagged: three absences running, or at least three that make up a tenth of at least five marked days in the last 30. An excused day neither adds to a run nor ends it. Reports total a class over any period, with a CSV download. Students see their own last 30 days.
- Migration 00019 adds the tables. New API tests cover all of this, including isolation between schools, and the whole flow ran in Edge against a production build, in English and Amharic and at phone width. That run caught two real bugs, both fixed: making a guardian the main contact and saving a register each sent fields the API refuses. It also showed a page's message scrolling out of view on a long register, so messages now stay in view on the administrator and teacher pages.

Limits: attendance is daily per class, not per period, and staff attendance is not built. Nothing is sent to parents yet: the call list stands in until an SMS provider is chosen. A guardian belongs to one student, so a parent of two children has two records until the parent portal links them. Register dates use the device's date, and a wrong clock on a phone would mark the wrong day. Import row errors and other server validation messages are still in English on the Amharic screens. Admissions, documents, photos, ID cards and promotion are not built. No file of a whole school's size has been imported, and the import has not been timed against the hosted database.

**2026-11-03 — Marks and report cards built.**

- An administrator sets the terms of each academic year, adds subjects to a whole grade at once, names each subject's teacher, and sets the pass mark and grade bands. The starting bands are placeholders and are flagged until the school saves its own.
- A subject's teacher adds tests with a share of the total and a maximum mark, then enters marks in a grid that takes the arrow keys and pastes from a spreadsheet. A blank is a missing mark, never a zero. Shares cannot pass 100, and marks cannot pass a test's maximum. A change to an existing mark is logged with both values.
- Teachers see how many marks are missing per subject. The roster shows every student's subject totals, total, average, grade and rank, with ties sharing a rank. A student with a missing mark has no average or rank. A subject can be left out of the average.
- Report cards add attendance from the term's dates, conduct and a teacher's comment. Cards before publishing are marked as drafts.
- Only an administrator publishes. Publishing locks the term's marks and keeps a copy of each card, so an issued card does not change when grade bands change or a student moves. Shares that do not add to 100 stop publishing outright, and missing marks need confirming. Taking reports back needs a reason, which is logged, and runs latest term first because a later card carries the earlier figures.
- A later term's card shows the earlier published terms with a year average and rank. Students see their own published cards.
- Cards and rosters print, or save as PDF, through the browser: one A4 page per card, and the roster on a landscape page. Names of subjects and terms appear as the school typed them, and the rest follows the language of the person printing.
- Migration 00020 adds the tables. New API tests cover all of this, including isolation between schools. The whole flow ran in Edge against a production build, in English and Amharic and at phone width, from setup through two semesters. Printed pages were generated and checked. A review also found and fixed a server error for a teacher outside a class opening a subject with no named teacher.

Limits: the default grade bands have not been checked against Ministry or regional rules. Promotion decisions and transcripts are not built, so a card makes no pass or fail statement beyond marking totals below the pass mark. Marks are entered by hand or pasted, and online exam scores do not flow into them yet. A report covers the students now in the class, so a student who moves mid-term has missing marks in the new class and no card from the old one for that term. Subject averages are unweighted across subjects. Any teacher of the class can write conduct and comments, because there is no homeroom teacher role yet. Printing depends on the device's browser and has only been tested in Edge. Server validation messages are still in English on Amharic screens, and the Amharic text is a draft that needs a native speaker's review.

**2026-11-03 — Parent portal built.**

- From a student's profile, an administrator invites a guardian with a one-time link, shared by hand for now. Parents choose their own password. Someone who already has an account in the school is linked straight away, and invitations waiting for the same email are all accepted together, so a parent of several children has one account. A student's account cannot become a parent's. Administrators can create a password reset link and remove access, which takes effect at once.
- Parents see only the children linked to them: a summary of each, then attendance with the school's reasons, published report cards they can print, fees with the balance due, and national exam readiness once a stream is chosen. Marks that are not published stay out of view, and reports taken back disappear for parents too.
- "How they are doing" picks the strongest subjects and those with the most room to improve from the latest card, with the change since the last term. It compares a child only with themself.
- Fee balances use the same sum as the finance screen, per currency, leaving out voided entries.
- Migration 00021 adds the link and invitations. New API tests cover all of this, including parents reaching only their own children. The whole flow ran in Edge against a production build, in English and Amharic and at phone width. That run caught parents being turned away by an old role check on the sign-in page, now fixed.
- The Amharic for attendance and report wording used masculine verbs that would misdescribe girls, so it now uses gender-neutral forms.

Limits: nothing is sent to parents, so they must open the portal, and no SMS goes out until a provider is chosen. Sign-in needs an email address, which not every parent has, so sign-in by phone should come with SMS. Invitation links are copied and shared by staff. Parents cannot message the school, see marks before publishing, or see day-to-day learning such as assignments. Attendance reasons and fee descriptions that staff type are now visible to families.

**2026-11-03 — My learning built.**

- A student's home page now opens on one plan: what is going well, where there is most room to improve, and what to do next. Parents see the same plan, and so do administrators and the teachers of the student's class, on the student's profile. Only the student can act on it.
- Subjects come from the latest published report card, with the change since the term before. Nothing below the pass mark is called going well, and the plan says nothing when every subject scored the same. Unpublished marks stay out.
- Topics come from online tests and exam practice, using the same mastery estimate shown lower on the page, which holds back until there is enough evidence. A topic needs at least 5 answers before the plan mentions it, so one bad quiz does not label a student. At 75% or more it is going well, and below 65% it has room to improve, the same line recommendations use. A weak topic sits under its subject where the names match, as "Physics, especially Motion".
- Next steps are real things to open, in this order: unfinished practice, tests the school has set that can be taken now with the nearest deadline first, practice on the weakest topics, class files for those subjects, then questions due for another try. Tests not yet open, past their deadline, or waiting for an invigilator are left out.
- Attendance is mentioned when it needs attention, using the same rule as the office's alerts, and when a student has no absences.
- It uses rules, not AI, so every line can be traced to marks or answers. It compares a student only with themself. New API tests cover all of this, and the whole flow ran in Edge against a production build, in English and Amharic and at phone width.

Limits: most schools will see little here until they have published a report card or students have answered enough tagged questions, since typed marks say nothing about topics. Subject and topic names are matched by spelling, so a file or topic under another spelling, or a subject named in Amharic, is not connected. Files are suggested by subject only and the plan cannot tell whether a student opened one. Practice is offered only for national exam papers. Teachers still cannot set their own online tests or assignments. The older recommendations panel still sits lower on the page, and its text is stored in English, so it stays in English on Amharic screens.

Next: teacher-made quizzes and assignments. SMS and phone sign-in still wait on choosing a provider.

---

## 1. What the system is today

| Layer | Technology | Where | Notes |
|---|---|---|---|
| Frontend | React 19, Vite 8, TypeScript 7 | `apps/web`, hosted on Vercel (`furii-educates.vercel.app`) | Single-page app; marketing landing at `/`, workspace at `/login` and `/admin/*` |
| API | Go `net/http`, pgx v5 | `services/api`, hosted on Render free tier (`furii-school-api.onrender.com`) | Modular monolith, about 4,000 lines, 79 routes |
| Database | PostgreSQL on Supabase | `services/api/migrations` (13 migrations) | Migrations auto-run at API start via `migrations/runner.go` |
| CI | GitHub Actions | `.github/workflows/ci.yml` | Builds API, migrates a fresh Postgres, curl smoke test of the admin flow, builds web |

### Roles and what each can do

- **Platform operator** (a school admin whose email is in `DEMO_REQUEST_ADMIN_EMAILS`): reviews landing-page setup requests and prepares new schools with a one-time admin invite link.
- **School admin**: school profile, academic years, classes, students, academic history (manual and CSV), curriculum and question bank, assessments, staff invites and class assignment, learner invites, finance ledger, lesson-plan review.
- **Teacher**: class rosters, skill-mastery overview, grading of written answers, lesson plans, uploading class resources.
- **Student**: assigned assessments (timed, autosave with IndexedDB recovery), results, skill mastery and recommendations, class resources.
- **Parent**: role exists in the database only. No features.

### Core flows

1. **Onboarding.** A prospect submits the landing form. The operator clicks "Prepare school", which creates a school and a pending admin invitation. The operator copies the link by hand because there is no email service. The admin accepts and signs in.
2. **Assessment loop.** The admin creates curriculum skills, writes questions with a JSON answer key, publishes them, builds an assessment, publishes it, and assigns it to a class. The student starts, which snapshots the questions and sets a server deadline. Answers autosave. On submit, objective answers are auto-graded and written answers go to the assigned teacher. Results release when every written answer is graded. Auto-graded, skill-tagged answers update mastery estimates and recommendations.
3. **Auth.** Email and password (Argon2id). An opaque session token sits in an HttpOnly cookie, and only its HMAC digest is stored in `sessions`. Invitations use 256-bit tokens passed in the URL fragment.

### What is genuinely good

- Sound security fundamentals: Argon2id, hashed session and invite tokens, server-side answer keys, composite `(id, school_id)` foreign keys on most tenant tables, RLS enabled and Supabase API roles revoked.
- A thoughtful exam engine: immutable question snapshots, server deadlines, answer revisions, idempotency keys, an IndexedDB retry queue.
- Audit log entries on most admin mutations.
- A migration runner with an advisory lock and per-migration transactions.
- A CI smoke test that exercises a real database.

---

## 2. Problems, ranked

- **P0**: must be fixed before any real school or real student data.
- **P1**: correctness or data-integrity bugs users will hit.
- **P2**: maintainability, operations, and product gaps.

### P0 — security and trust

**P0-1. Curriculum data is shared across all schools.**
`curricula`, `subjects`, `curriculum_nodes` and `curriculum_mappings` have no `school_id` (`migrations/00003_curriculum_question_bank.sql`). Every handler in `internal/httpapi/curriculum.go` reads and writes them without a school filter. Onboarding now creates multiple schools, so any school admin can read every other school's curricula, add subjects and nodes to them, and overwrite their mappings through `ON CONFLICT ... DO UPDATE`. Question skill tags and mastery records can point at another school's nodes. The unique key on provider, name, version and language also stops a second school from creating the same official curriculum.
Fix: add `school_id`, nullable only for a platform-owned read-only "official library". Backfill it from `audit_logs`, scope every query, validate skill tags against the school, and add tenant-isolation tests.

**P0-2. Login depends on third-party cookies.**
The frontend (vercel.app) and the API (onrender.com) are different sites, so the session cookie is `SameSite=None` (`internal/httpapi/http.go:80`). Safari and every iOS browser block third-party cookies, so sign-in will appear to work and then fail on the next request. Firefox partitions them, which breaks the plain-link resource download in `operations-ui.tsx`.
Fix: add a Vercel rewrite from `/api/:path*` to the Render API, build with an empty `VITE_API_BASE_URL`, and switch the cookie to `SameSite=Lax`. Add an explicit CSRF check on unsafe methods. Long term, serve the API from a subdomain of the product domain.

**P0-3. No password reset and no change-password.**
Nothing in the API or UI lets a user change or recover a password. A forgotten admin password means editing the database directly. A disabled teacher cannot be re-invited because emails are now globally unique.
Fix: an admin-initiated reset link using the existing invite token pattern, a self-service change-password, and email-based reset once email delivery exists.

**P0-4. Exam integrity gaps.**
- `GET /student/assessments/{id}` returns every question before the attempt starts (`internal/httpapi/learner.go:307`). A student can read the whole exam and only then start the timer.
- Abandoned attempts are never finalized. If a student closes the tab, the attempt stays `in_progress` forever, with no score, no mastery update, and nothing visible to teachers. Only the browser auto-submits.
- The browser auto-submit fires exactly at the deadline (`apps/web/src/main.tsx:519`). By then the server ignores the submitted payload, so answers still syncing are lost.
- `due_at` exists in the schema, but no API or UI sets it, so assessments never close.

Fix: return questions only after start, add a server-side finalizer (a ticker in the API or finalize-on-read) that grades expired attempts, allow a short server grace window, and add due dates to assignments.

**P0-5. Vulnerable and unpinned dependencies.**
`govulncheck` reports 4 reachable issues:
- `github.com/jackc/pgx/v5` 5.7.2 has a SQL-injection advisory involving dollar-quoted strings, fixed in 5.9.2.
- `golang.org/x/text` 0.21.0 has two issues, fixed in 0.41.0.
- A standard-library `crypto/x509` issue depends on the Go version Render builds with.

`go.mod` has no `toolchain` line, so Render chooses the Go version. `apps/web/package.json` declares every dependency as `"latest"`.
Fix: bump pgx and x/text, add a `toolchain` line, pin exact npm versions, and add `govulncheck` and `npm audit` to CI.

**P0-6. No rate limiting.**
Login, invitation acceptance, and the public demo-request form have no per-IP limits. The demo form only throttles per email, so random emails bypass it.
Fix: an in-memory per-IP token bucket, which is enough for one instance, plus backoff on repeated failed logins.

**P0-7. No automated tests.**
The repository has no `_test.go` files and no frontend tests. CI only smoke-tests admin happy paths. Nothing checks school isolation, teacher class scoping, or the exam flow.
Fix: Go integration tests against the CI Postgres service, starting with tenant isolation and the assessment lifecycle. Add one Playwright end-to-end test for invite, start, save, submit and grade.

### P1 — correctness and data integrity

1. **Silent list truncation.** Students, classes, questions, assessments and other lists are capped at 200 or 500 rows with no pagination (for example `school.go:279`). A school with more than 200 students loses rows in the UI, and the overview's student count is wrong.
2. **Malformed IDs return HTTP 500.** Postgres error `22P02` is not mapped in `databaseError` (`school.go:430`), so a bad UUID in a URL is logged as a server failure.
3. **Only assigned teachers can grade written answers.** If a class has no teacher, results are never released. Admins need a grading fallback.
4. **No gradebook.** Neither admins nor teachers can see per-student assessment scores. Admins only see a school-wide average.
5. **Most records cannot be edited or removed.** Missing actions:
   - edit or archive classes and academic years;
   - enroll an existing student or move them between classes;
   - edit, delete or verify academic records (`verification_status` is always `unverified`);
   - archive published questions and edit assessments;
   - unassign teachers, void finance entries, delete resources.
6. **Deactivating a student** leaves their login active and their enrollments untouched.
7. **CSV import.** Excel's UTF-8 byte-order mark breaks the header check. Re-importing a file duplicates every record. Preview and commit run one query per row.
8. **Finance currency.** USD and EUR entries can be recorded, but the summary sums only ETB and the UI labels every amount ETB.
9. **Teacher learning profile** returns one row per student per skill, capped at 500 rows, so large classes are truncated.
10. **Email uniqueness.** Invites check the email only within the school, but migration 00012 made emails globally unique. The conflict appears only when the invitee tries to accept.
11. **Score totals** use live `assessment_items` instead of the attempt snapshot (`learner.go:643`, `reviews.go:152`).
12. **Mastery history** inserts a snapshot for every skill on every submission. Accepted recommendations get "superseded" without any outcome recorded.
13. **Smaller API inconsistencies.**
    - The start-attempt response is not wrapped in the usual `{data: ...}` envelope.
    - `listSchoolLessonPlans` and `writeResources` skip the `rows.Err()` check.
    - Admins cannot list a class's resources because the route uses the teacher guard.
    - Lesson-plan review has no comment field and writes no audit entry.

### P2 — architecture, operations, and product gaps

**Frontend structure.**
`apps/web/src/main.tsx` is 90 KB. It holds one `App` component with about 40 `useState` hooks, renders every admin page and hides the inactive ones, and has many lines over 1,000 characters. There is no router, and `operations-ui.tsx` duplicates the API client. Every feature touches this file, which is the biggest drag on development speed.
Target: a router, one component per page, a shared typed API client (ideally generated from OpenAPI), and a data-fetching layer such as TanStack Query.

**Backend structure.**
SQL is written inline in HTTP handlers, although `docs/architecture.md` describes domain services that do not exist. The auth guard is copy-pasted four times, and every request runs two queries to resolve identity and roles.
Target: one middleware that resolves identity once and stores it in the request context, small role-check wrappers, and a domain package per area.

**Operations.**
- The Render free tier took 33 seconds to answer the first request during this analysis. Schools will read that as "the site is broken".
- Teaching files up to 15 MB are stored as `bytea` in Postgres. Each download loads the whole file into memory, and the files count against the Supabase database size limit. They belong in Supabase Storage or S3 behind signed URLs.
- There is no request logging, metrics, or error tracking. Expired sessions are never deleted.
- There is no email delivery. Every invite link is copied by hand.
- The frontend sends no CSP, `X-Frame-Options` or `Referrer-Policy` headers.

**Docs and contract drift.**
- `api-spec/openapi.yaml` is missing 23 live routes (staff, finance, lesson plans, resources, teacher classes) and lists one route that does not exist.
- The README references an `infrastructure/docker` folder that does not exist.
- `docs/staff-invitations-migration.md` still tells operators to paste SQL into the Supabase editor, though the API now migrates itself.
- Three Go files are not gofmt-clean, and CI runs `go mod tidy`, which mutates files, instead of verifying them.

**Product and UX.**
- Every school admin sees the "School setup requests" page, which then fails with a 403 error.
- The topbar's "All changes saved" indicator is static text.
- After accepting an invite, "Continue to sign in" leads to the marketing page instead of `/login`.
- Question authoring needs a raw JSON answer key typed by hand, and only admins can author questions or assessments.
- A user who is both admin and teacher only ever sees the teacher workspace.
- There is no Amharic or other localisation, although each school sets a default language.
- Platform operators are ordinary school admins plus an environment variable, which mixes tenant and platform administration.

---

## 3. How it should work (target state)

- **Same-site API.** Served behind the Vercel domain, with `SameSite=Lax` cookies, a CSRF check, rate limits, and password reset.
- **Strict tenancy.** Every tenant row carries `school_id`, enforced by composite foreign keys and covered by isolation tests. Any platform-owned curriculum library is read-only to schools.
- **Server-authoritative exams.** Questions stay hidden before start. A background finalizer grades expired attempts. Submissions get a short grace window, assignments have due dates, admins can grade when no teacher is assigned, and teachers and admins have a gradebook.
- **Complete record management.** Pagination everywhere, edit and archive for every core entity, enrollment management, and correctable finance through void and reversal entries.
- **Maintainable code.** A routed, componentised frontend. Shared middleware and domain packages in Go. An OpenAPI spec kept in sync and used to generate the client.
- **Operable service.** Always-on hosting for pilots, object storage for files, structured request logs, error tracking, email delivery, and a rehearsed backup restore.

---

## 4. Recommended order of work

Nothing new should be built until Phases 0 and 1 are done. They protect every later change.

| Phase | Goal | Items |
|---|---|---|
| **0. Safety net** | Make change safe | Test harness with first isolation and exam tests (P0-7). gofmt, vet, govulncheck and npm audit in CI. Bump pgx and x/text, pin the Go toolchain and npm versions (P0-5). Map `22P02` to 404. Refresh README, OpenAPI and stale docs. |
| **1. Security and tenancy** | Safe for a real school | Curriculum scoping migration (P0-1). Same-origin API proxy, Lax cookies, CSRF check (P0-2). Rate limits (P0-6). Password change and admin reset (P0-3). Session cleanup. Hide the setup-requests page from non-operators. |
| **2. Exam correctness** | Trustworthy assessments | Hide questions before start, server finalizer, grace window, due dates (P0-4). Admin grading fallback. Gradebook. Snapshot-based totals. |
| **3. Frontend restructure** | Faster feature work | Router and per-page components, shared API client, data-fetching layer. Do this once the Phase 0 end-to-end test exists. |
| **4. Record completeness** | Usable day to day | Pagination. Edit and archive for classes, years, enrollments, records, questions and assessments. Teacher unassign. Finance void and multi-currency. CSV fixes. Student deactivation side effects. |
| **5. Operations** | Ready for a pilot | Paid Render instance or keep-warm. Files to object storage. Logging and error tracking. Email delivery for invites and resets. Backup restore rehearsal. Security headers. |
| **6. Product growth** | Beyond the pilot | Teacher question and assessment authoring with a friendly answer-key editor. Localisation. Parent role. Analytics. |
