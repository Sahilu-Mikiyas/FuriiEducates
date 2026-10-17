# Phase 1: Pilot readiness

This checklist prepares the first FURII School OS pilot after the initial Render, Supabase, and Vercel deployment.

## Current baseline

- The Go API is deployed at `https://furii-school-api.onrender.com`.
- The API health endpoint returned `{"data":{"status":"ok"}}`, confirming database connectivity at that time.
- Administrator sign-in succeeded in the Vercel frontend.
- The frontend has a stable domain, `https://furii-educates.vercel.app`.
- A Vercel preview deployment was also used during setup. Preview URLs are temporary and may change.

## Before entering real student records

### Production domain and browser access

- [ ] Keep `VITE_API_BASE_URL` in Vercel set to `https://furii-school-api.onrender.com` for the production and preview environments that need the API.
- [ ] Keep `CORS_ALLOWED_ORIGINS` in Render restricted to the stable production origin `https://furii-educates.vercel.app`.
- [ ] Remove temporary Vercel preview origins from Render after preview testing. Add a preview origin only when actively testing that exact deployment.
- [ ] Redeploy the Render API after changing CORS, then redeploy the Vercel frontend after changing its build-time API URL.
- [ ] Confirm `https://furii-school-api.onrender.com/healthz` returns `{"data":{"status":"ok"}}` after each infrastructure change.

### Secrets and administrator access

- [ ] Keep `DATABASE_URL` and `SESSION_HASH_KEY` only in Render's service environment. Never commit them or put them in Vercel.
- [ ] Keep `HTTP_ADDR=:10000`, `COOKIE_SECURE=true`, and `SESSION_TTL=12h` in Render.
- [ ] Use a unique administrator password before the pilot; do not reuse setup or sample passwords.
- [ ] Store recovery credentials in the school's approved password manager and limit access to authorized administrators.
- [ ] Do not enable public account creation. Provision staff accounts through the controlled administrator workflow.

### Database recovery and operations

- [ ] Verify Supabase backup availability and retention for the selected plan.
- [ ] Create or identify a restore point before the pilot, and document who can restore it.
- [ ] Document the migration procedure for applying SQL files in `services/api/migrations` before deploying schema changes.
- [ ] Confirm the Supabase project is active and its database password in Render matches the current Supabase password.
- [ ] Record the Render free-instance sleep behavior and expected wake-up delay; decide whether the pilot needs always-on hosting.

### Access and data safeguards

- [ ] Use synthetic records during setup and demonstrations.
- [ ] Confirm an administrator can set the school profile, academic year, and classes.
- [ ] Confirm staff permissions are scoped to the correct school and role before inviting staff.
- [ ] Confirm a disabled account cannot sign in and a logged-out session cannot access protected API routes.
- [ ] Agree on who may view, correct, import, export, and delete student records.

## Phase 1 exit criteria

Phase 1 is complete when the stable production domain is the one used for the pilot, CORS allows only intended origins, secrets and administrator access are controlled, a database restore path is understood, and the school has verified access rules using synthetic records.

Until these checks are complete, use synthetic data only.
