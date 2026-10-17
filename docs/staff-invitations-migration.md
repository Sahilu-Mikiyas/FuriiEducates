# Staff invitations deployment

Staff access, assessment delivery/grading, and learning intelligence require migrations `00004_staff_invitations.sql` through `00009_assessment_recovery.sql`. Apply all six, in numerical order, to the production database before deploying or opening the staff, assessment, student, teacher review, and learning profile screens. These migrations create invitation, class assignment, assessment, attempt, written response review, skill evidence, mastery, recommendation, intervention outcome, immutable attempt snapshot, and attempt event tables; migration 00009 also adds server-enforced duration and answer revision/flag metadata.

## Apply through Supabase SQL Editor

1. Open the FURII Supabase project and choose **SQL Editor → New query**.
2. Open `services/api/migrations/00004_staff_invitations.sql` from this repository.
3. Copy the SQL statements under `-- +goose Up`. Do not include the Goose marker lines or the statements under `-- +goose Down`.
4. Paste those statements into the SQL Editor and run them once.
5. Repeat for `services/api/migrations/00005_assessments.sql`, again running only the statements under `-- +goose Up`.
6. Repeat for `services/api/migrations/00006_student_accounts_attempts.sql`, running only the statements under `-- +goose Up`.
7. Repeat for `services/api/migrations/00007_written_answer_grading.sql`, running only the statements under `-- +goose Up`.
8. Repeat for `services/api/migrations/00008_learning_intelligence.sql`, running only the statements under `-- +goose Up`.
9. Repeat for `services/api/migrations/00009_assessment_recovery.sql`, running only the statements under `-- +goose Up`.
10. Confirm all six queries report success. The created tables include `staff_invitations`, `class_teachers`, `assessments`, `assessment_items`, `assessment_assignments`, `student_invitations`, `assessment_attempts`, `assessment_answers`, `assessment_attempt_items`, `assessment_attempt_events`, `skill_evidence`, `mastery_snapshots`, `recommendations`, and `intervention_outcomes`.

The migration only changes schema; it does not create staff accounts or change existing users. Never paste a database URL or password into the SQL Editor.

## Apply with Goose

From `services/api`, with a trusted shell that already has the production `DATABASE_URL` set:

```sh
go run github.com/pressly/goose/v3/cmd/goose@v3.24.1 -dir migrations postgres "$DATABASE_URL" up
```

Do not put the connection string in a committed file or share it in a chat. Goose tracks applied migrations and applies pending migrations in order.

After all six migrations are applied, deploy the API and frontend from the same repository revision. In the admin workspace, create a learner invitation for an active student and share the one-time link privately with the learner. The student can then sign in and access published assessments assigned to an actively enrolled class. Objective question types are graded by the API; teachers assigned to the class can review written responses and leave feedback. Skill estimates use automatically graded objective answers linked to curriculum skills. The initial `beta-weighted-v1` estimate uses a Beta(2,2) prior and confidence bands based on evidence count; it is a pilot estimate, not a validated prediction. Recommendations start after at least five tagged responses show estimated mastery below 65%.

Each attempt receives a server-set deadline and question/answer-key snapshot. Learner answer drafts and retry metadata are stored in browser IndexedDB until the API acknowledges them; this is reconnect recovery, not secure full offline exam delivery. The server remains authoritative for deadlines and grading. The final score and feedback appear once every answered written response has been graded.
