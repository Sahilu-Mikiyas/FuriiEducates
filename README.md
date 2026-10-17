# FURII School OS

Single-school, assessment-first school platform. The first release focuses on secure school setup, student academic history, online assessments, and explainable learning recommendations.

## Repository layout

- `services/api`: Go HTTP API and domain packages
- `services/api/migrations`: PostgreSQL schema migrations (Goose)
- `apps/web`: React, Vite, and TypeScript application
- `api-spec/openapi.yaml`: REST API contract
- `docs`: architecture and operational decisions
- `infrastructure/docker`: local PostgreSQL service

## Local development

Requirements: Go 1.23+, Node.js 20+, npm, and Docker Compose.

1. Copy `.env.example` to `.env` as a reference and set a long random `SESSION_HASH_KEY` (at least 32 bytes). Export its values in the shell running the Go commands; the API does not load dotenv files.
2. Start PostgreSQL with `docker compose up -d db`.
3. Run migrations from `services/api`: `go run github.com/pressly/goose/v3/cmd/goose@v3.24.1 -dir migrations postgres "$DATABASE_URL" up`.
4. Start the API from `services/api`: `go run ./cmd/api`.
5. Start the web application from `apps/web`: `npm install` then `npm run dev`.

The API exposes health and authentication endpoints plus administrator workflows for school settings, academic years, classes, students, historical academic records, external examinations, curriculum versions, subject skill trees, and a reusable question bank. Academic history CSV uploads are previewed with row-level validation before a single atomic commit. Questions support single choice, multiple choice, numeric, and text answer types; publishing requires source and licensing notes plus skill-tag weights that sum to 1. Answer keys remain server-side and are omitted from question responses. Add only curriculum content approved for use by your school. Provision the first school administrator once with `BOOTSTRAP_SCHOOL_NAME`, `BOOTSTRAP_ADMIN_EMAIL`, `BOOTSTRAP_ADMIN_NAME`, and `BOOTSTRAP_ADMIN_PASSWORD` set in the environment, then run `go run ./cmd/bootstrap` from `services/api`. The bootstrap command refuses to run after a school exists, and public signup is not enabled. Production deployments must use HTTPS, set `COOKIE_SECURE=true`, restrict database access, and protect backups and secrets.

## Production web and API connection

The Vercel deployment serves the Vite frontend; it does not run this Go API automatically. The root `render.yaml` prepares a small Render web service for `services/api`. Create it from this repository in Render and provide the Supabase `DATABASE_URL` and exact Vercel frontend origin when prompted. After the API deploys, set `VITE_API_BASE_URL` in the Vercel project to the API's `onrender.com` origin and redeploy the frontend. Do not use the Supabase project URL or anon key as the API URL. The Blueprint generates `SESSION_HASH_KEY`, enables HTTPS cookies, and probes `/healthz`. Render's Free web services sleep after 15 minutes without requests and can take about a minute to wake; use a paid instance for production availability.

## Engineering principles

PostgreSQL is authoritative. The Go API is a modular monolith. Authentication uses opaque, revocable server-side sessions and secure cookies. Authorization is enforced in the API and scoped to school records. Student-facing APIs must never expose question answer keys.
