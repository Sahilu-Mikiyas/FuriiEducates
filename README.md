# FURII School OS

School-scoped, assessment-first school platform. The first pilot can start with one school; new school workspaces are created through a reviewed setup request and one-time administrator invitation.

## Repository layout

- `services/api`: Go HTTP API and domain packages
- `services/api/migrations`: PostgreSQL schema migrations (Goose)
- `apps/web`: React, Vite, and TypeScript application
- `api-spec/openapi.yaml`: REST API contract
- `docs`: architecture and operational decisions
- `compose.yaml`: local PostgreSQL service

## Local development

Requirements: Go 1.26+ (the pinned 1.27.3 toolchain downloads automatically), Node.js 20.19+ or 22.12+, npm, and Docker Compose or another PostgreSQL 17 server.

1. Copy `.env.example` to `.env` as a reference and set a long random `SESSION_HASH_KEY` (at least 32 bytes). Export its values in the shell running the Go commands; the API does not load dotenv files.
2. Start PostgreSQL with `docker compose up -d db`.
3. Start the API from `services/api`: `go run ./cmd/api`. The API applies any pending migrations in `services/api/migrations` before it starts serving, under an advisory lock, and refuses to start if a migration fails.
4. Start the web application from `apps/web`: `npm ci` then `npm run dev`.

## Tests

Run the API tests from `services/api`:

```sh
go test ./...
```

Unit tests always run. Database integration tests run only when `TEST_DATABASE_URL` points at a PostgreSQL superuser connection, for example the Compose database: `TEST_DATABASE_URL=postgres://furii:furii-dev-only@localhost:5432/furii?sslmode=disable go test ./...`. The suite creates a fresh database for each run, applies every migration, exercises the real HTTP handlers, and drops the database afterwards. Set `FURII_TEST_KEEP_DB=1` to keep it, or `FURII_TEST_LOG=1` to print API server logs. CI runs the same suite, plus gofmt, go vet, govulncheck, and npm audit.

Tests marked as skipped document known issues from [the system analysis](docs/system-analysis.md); remove the skip when the issue is fixed.

The API exposes health and authentication endpoints plus administrator workflows for school settings, academic years, classes, students, historical academic records, external examinations, curriculum versions, subject skill trees, and a reusable question bank. Academic history CSV uploads are previewed with row-level validation before a single atomic commit. Questions support single choice, multiple choice, numeric, and text answer types; publishing requires source and licensing notes plus skill-tag weights that sum to 1. Answer keys remain server-side and are omitted from question responses. Add only curriculum content approved for use by your school. Provision the first school administrator once with `BOOTSTRAP_SCHOOL_NAME`, `BOOTSTRAP_ADMIN_EMAIL`, `BOOTSTRAP_ADMIN_NAME`, and `BOOTSTRAP_ADMIN_PASSWORD` set in the environment, then run `go run ./cmd/bootstrap` from `services/api`. The bootstrap command refuses to run after a school exists, and public signup is not enabled. Production deployments must use HTTPS, set `COOKIE_SECURE=true`, restrict database access, and protect backups and secrets.

School administrators can invite teachers and learners using single-use, seven-day links, assign teachers to classes, and disable teacher accounts. Teachers can view rosters and class learning evidence, grade submitted written answers, create structured lesson plans (objectives, sequence, differentiation, assessment, and homework), and share resources with their classes. Administrators can review and approve or return submitted plans. Resource files are stored privately in PostgreSQL, limited to 15 MB each, and downloadable only by school administrators, teachers assigned to the class, and currently enrolled learners. Administrators can record tuition charges, payments, operating expenses, and salary entries in a school-scoped ledger; amounts use minor units, salary details are administrator-only, and the ledger does not process payments or payroll transfers. Administrators can create timed assessments from published questions and assign them to classes. Learner attempts use server-set deadlines and immutable question snapshots; autosaves use answer revisions and an IndexedDB retry queue for reconnect recovery. The server remains authoritative for deadlines and grading. Final scores and teacher feedback are released after all answered written responses are graded. See the [remaining implementation phases](docs/remaining-implementation-phases.md) for rollout work and limits before a real-student pilot.

## Walkthrough requests

The landing page collects school name, learner-count band, region, timezone, language, priorities, and the intended administrator contact through a two-step setup wizard. POST /api/v1/demo-requests stores this prospect data in public.demo_requests; do not submit learner records. The table has RLS enabled and no Supabase Data API grants or policies. A configured FURII product operator reviews requests in /admin/demo-requests and can prepare a school once. Preparation creates the school, saves the choices in school_setup_profiles, and returns a hashed-at-rest, seven-day, one-time school-administrator invitation token. The operator must copy and share the link privately; this repo has no email delivery service. After the admin accepts the invite and signs in, School Profile shows the saved learner count, region, and priorities, while school name, timezone, and default language are prefilled. The inbox is restricted to school administrators whose email is listed in Render's comma-separated DEMO_REQUEST_ADMIN_EMAILS variable. Set that variable in furii-school-api to the exact operator email(s), then redeploy the API. Requests are limited to 100 inbox rows and repeat submissions from one email are throttled for ten minutes.

## Production web and API connection

The Vercel deployment serves the Vite frontend and proxies `/api/*` to the Go API on Render (see `apps/web/vercel.json`). Browsers therefore call the API on the frontend's own origin, so the session cookie is first-party; Safari and iOS block the third-party cookies the old direct setup needed. Leave `VITE_API_ORIGIN` unset in Vercel; the older `VITE_API_BASE_URL` variable is no longer read and can be deleted. Locally, the Vite dev server proxies `/api` to `localhost:8080` in the same way.

The root `render.yaml` prepares a small Render web service for `services/api`. Create it from this repository in Render and provide the Supabase `DATABASE_URL` and the exact Vercel frontend origin. Keep that origin in `CORS_ALLOWED_ORIGINS` even behind the proxy, because the proxy forwards the browser's `Origin` header, which the API checks on every request to stop cross-site request forgery. The Blueprint generates `SESSION_HASH_KEY`, enables HTTPS cookies, sets `TRUST_PROXY_HEADERS` so rate limits see real client addresses, and probes `/healthz`.

`COOKIE_SAMESITE` defaults to `none` with HTTPS cookies for compatibility. Once sign-in works through the Vercel proxy, set it to `lax` in Render and redeploy; users sign in again after the switch. Render's Free web services sleep after 15 minutes without requests and take about 30 to 60 seconds to wake. Vercel waits up to two minutes for a proxied response, so the first request still succeeds, but use a paid instance before a pilot.

### Accounts and passwords

Users change their own password from their workspace; this signs out their other devices. A school administrator can issue a one-time, 24-hour password reset link for a teacher (Staff access page) or a learner (Students page). A platform operator can issue one for a locked-out school administrator through `POST /api/v1/platform/password-resets`. Links are shown once and must be shared privately; there is no email delivery yet. Login is rate limited per client address and per account (10 failed attempts lock the account for 15 minutes). Expired sessions and old reset links are purged hourly.

### Curriculum ownership

Each curriculum belongs to the school that created it and is invisible to other schools. Curricula without an owning school form a read-only platform library that every school can use, for example to publish an official national curriculum once.

The administrator workspace uses focused pages at `/admin/overview`, `/admin/students`, `/admin/classes`, `/admin/academic-passport`, `/admin/curriculum`, `/admin/assessments`, `/admin/finance`, `/admin/lesson-plans`, `/admin/staff`, and `/admin/settings`. Vercel rewrites those paths to the Vite app so they also load directly and survive a browser refresh.
Before using real student data, complete the [Phase 1 pilot-readiness checklist](docs/phase-1-pilot-readiness.md).

## Engineering principles

PostgreSQL is authoritative. The Go API is a modular monolith. Authentication uses opaque, revocable server-side sessions and secure cookies. Authorization is enforced in the API and scoped to school records. Student-facing APIs must never expose question answer keys.

