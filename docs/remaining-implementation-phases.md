# Remaining implementation phases

This release implements the core Phase 5–7 product path in the engineering blueprint. It is a pilot-ready foundation, not a claim that production rollout checks have already been completed.

## Phase 5 — Assessment delivery and grading

Implemented in this branch:

- Assessment duration is configurable (5–240 minutes) and the server assigns an attempt deadline.
- A started attempt stores immutable question snapshots, including grading data, so later question edits cannot change an in-flight result.
- Learners can navigate questions, flag items, autosave with revisions/idempotency keys, and recover pending saves from browser IndexedDB after reconnecting.
- The server checks the deadline and grades from the attempt snapshot. Repeated submission is idempotent; after expiry, only server-acknowledged answers count.
- Written answers use the existing teacher review workflow; final results release when required grading is complete.

## Phase 6 — Mastery and recommendations

Implemented in this branch:

- Automatically graded, skill-tagged responses produce versioned evidence and mastery snapshots.
- The `beta-weighted-v1` estimate exposes evidence count and confidence separately and is labeled as an initial pilot estimate.
- Low-mastery skills with enough evidence create explainable recommendations. Learners can accept and complete them; subsequent skill evidence records pre/post change.
- Teachers see class-scoped mastery and active recommendation counts. Administrators see school-wide assessment and recommendation summary metrics.

## Phase 7 — Dashboards, resilience, and pilot readiness

Implemented in this branch:

- Student, teacher, and administrator views expose assessment status and the learning information appropriate to each role.
- Answer-save retries are bounded and pending local answers remain queued until acknowledged or submitted.
- Teacher profile access checks class assignment; student and school queries remain scoped by authenticated school/student identity.
- The pilot readiness checklist documents domain/CORS, credential, backup, restore, and data-handling checks.

Required before a real-student pilot:

1. Apply migrations `00004` through `00009` to Supabase in order, then deploy the same revision to Render and Vercel.
2. Confirm `/healthz`, sign-in, student invitation, assessment start/save/reconnect/submit, deadline enforcement, teacher written grading, and result release using synthetic records.
3. Verify school and class isolation with separate test accounts, review the access-control policy, and confirm account deactivation/session revocation behavior.
4. Confirm Supabase backup retention and complete a documented restore rehearsal; assign an operational owner for incidents and data corrections.
5. Review browser-local answer retention and shared-device handling with the school. IndexedDB improves reconnect recovery but does not make the browser a trusted offline examination environment.

## Later releases

The blueprint's subsequent releases—production-grade psychometrics, personalized content generation, external curriculum data integrations, and broader rollout operations—remain separate roadmap work. Mastery and recommendations here are deliberately deterministic and should not be treated as validated predictions or automated high-stakes placement decisions.
