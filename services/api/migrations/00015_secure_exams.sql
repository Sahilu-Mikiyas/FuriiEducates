-- +goose Up
-- Per-assessment delivery settings.
ALTER TABLE public.assessments
    ADD COLUMN shuffle_questions boolean NOT NULL DEFAULT false,
    ADD COLUMN shuffle_options boolean NOT NULL DEFAULT false,
    ADD COLUMN locked_mode boolean NOT NULL DEFAULT false;

-- Per-class window and invigilated session state. A start code is shown in
-- the room by the invigilator, so it is stored readable and replaced each
-- time the session opens.
ALTER TABLE public.assessment_assignments
    ADD COLUMN opens_at timestamptz,
    ADD COLUMN requires_start_code boolean NOT NULL DEFAULT false,
    ADD COLUMN start_code text,
    ADD COLUMN session_open boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT assessment_assignments_window CHECK (opens_at IS NULL OR due_at IS NULL OR due_at > opens_at);

ALTER TABLE public.assessment_attempt_events DROP CONSTRAINT assessment_attempt_events_event_type_check;
ALTER TABLE public.assessment_attempt_events ADD CONSTRAINT assessment_attempt_events_event_type_check
    CHECK (event_type IN ('started','answer_saved','answer_flagged','reconnected','submitted','deadline_submission','force_submitted','time_extended','incident','integrity_flag'));

-- Finds attempts whose deadline has passed without scanning finished ones.
CREATE INDEX assessment_attempts_open_deadline
    ON public.assessment_attempts (deadline_at) WHERE status = 'in_progress';

-- +goose Down
DROP INDEX IF EXISTS public.assessment_attempts_open_deadline;
DELETE FROM public.assessment_attempt_events WHERE event_type IN ('force_submitted','time_extended','incident','integrity_flag');
ALTER TABLE public.assessment_attempt_events DROP CONSTRAINT assessment_attempt_events_event_type_check;
ALTER TABLE public.assessment_attempt_events ADD CONSTRAINT assessment_attempt_events_event_type_check
    CHECK (event_type IN ('started','answer_saved','answer_flagged','reconnected','submitted','deadline_submission'));
ALTER TABLE public.assessment_assignments
    DROP CONSTRAINT IF EXISTS assessment_assignments_window,
    DROP COLUMN IF EXISTS session_open,
    DROP COLUMN IF EXISTS start_code,
    DROP COLUMN IF EXISTS requires_start_code,
    DROP COLUMN IF EXISTS opens_at;
ALTER TABLE public.assessments
    DROP COLUMN IF EXISTS locked_mode,
    DROP COLUMN IF EXISTS shuffle_options,
    DROP COLUMN IF EXISTS shuffle_questions;
