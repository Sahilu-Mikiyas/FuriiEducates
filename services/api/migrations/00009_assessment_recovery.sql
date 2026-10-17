-- +goose Up
ALTER TABLE assessments
    ADD COLUMN duration_minutes INT NOT NULL DEFAULT 45 CHECK (duration_minutes BETWEEN 5 AND 240);

ALTER TABLE assessment_attempts
    ADD COLUMN deadline_at TIMESTAMPTZ;
UPDATE assessment_attempts at
SET deadline_at = at.started_at + make_interval(mins => a.duration_minutes)
FROM assessment_assignments aa
JOIN assessments a ON a.id=aa.assessment_id AND a.school_id=aa.school_id
WHERE aa.id=at.assignment_id;
ALTER TABLE assessment_attempts ALTER COLUMN deadline_at SET NOT NULL;

ALTER TABLE assessment_answers
    ADD COLUMN flagged BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN revision INT NOT NULL DEFAULT 1 CHECK (revision > 0),
    ADD COLUMN idempotency_key UUID,
    ADD COLUMN saved_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE TABLE assessment_attempt_items (
    attempt_id UUID NOT NULL,
    school_id UUID NOT NULL,
    question_id UUID NOT NULL,
    position INT NOT NULL CHECK (position > 0),
    points NUMERIC(7,2) NOT NULL CHECK (points > 0),
    question_snapshot JSONB NOT NULL,
    PRIMARY KEY (attempt_id,question_id),
    UNIQUE (attempt_id,position),
    FOREIGN KEY (attempt_id,school_id) REFERENCES assessment_attempts(id,school_id) ON DELETE CASCADE,
    FOREIGN KEY (question_id,school_id) REFERENCES questions(id,school_id)
);

CREATE TABLE assessment_attempt_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    attempt_id UUID NOT NULL,
    event_type TEXT NOT NULL CHECK (event_type IN ('started','answer_saved','answer_flagged','reconnected','submitted','deadline_submission')),
    event_data JSONB NOT NULL DEFAULT '{}',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (attempt_id,school_id) REFERENCES assessment_attempts(id,school_id) ON DELETE CASCADE
);
CREATE INDEX assessment_attempt_events_attempt ON assessment_attempt_events(attempt_id,occurred_at);

-- +goose Down
DROP TABLE IF EXISTS assessment_attempt_events, assessment_attempt_items;
ALTER TABLE assessment_answers
    DROP COLUMN IF EXISTS saved_at,
    DROP COLUMN IF EXISTS idempotency_key,
    DROP COLUMN IF EXISTS revision,
    DROP COLUMN IF EXISTS flagged;
ALTER TABLE assessment_attempts DROP COLUMN IF EXISTS deadline_at;
ALTER TABLE assessments DROP COLUMN IF EXISTS duration_minutes;
