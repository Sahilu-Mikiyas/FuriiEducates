-- +goose Up
ALTER TABLE students ADD CONSTRAINT students_user_school_fk
    FOREIGN KEY (user_id, school_id) REFERENCES users(id, school_id);

CREATE TABLE student_invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    student_id UUID NOT NULL,
    email TEXT NOT NULL,
    token_hash BYTEA NOT NULL UNIQUE,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    FOREIGN KEY (student_id,school_id) REFERENCES students(id,school_id) ON DELETE CASCADE,
    FOREIGN KEY (created_by,school_id) REFERENCES users(id,school_id)
);
CREATE UNIQUE INDEX student_invitations_pending_student
    ON student_invitations (student_id) WHERE accepted_at IS NULL AND revoked_at IS NULL;

CREATE TABLE assessment_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    assignment_id UUID NOT NULL,
    student_id UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'in_progress' CHECK (status IN ('in_progress','submitted')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    submitted_at TIMESTAMPTZ,
    score NUMERIC(9,2),
    max_score NUMERIC(9,2) NOT NULL DEFAULT 0,
    UNIQUE (assignment_id,student_id),
    UNIQUE (id,school_id),
    FOREIGN KEY (assignment_id,school_id) REFERENCES assessment_assignments(id,school_id) ON DELETE CASCADE,
    FOREIGN KEY (student_id,school_id) REFERENCES students(id,school_id) ON DELETE CASCADE
);
CREATE INDEX assessment_attempts_student ON assessment_attempts (student_id,started_at DESC);

CREATE TABLE assessment_answers (
    attempt_id UUID NOT NULL,
    school_id UUID NOT NULL,
    question_id UUID NOT NULL,
    answer JSONB,
    is_correct BOOLEAN,
    awarded_points NUMERIC(9,2) NOT NULL DEFAULT 0,
    PRIMARY KEY (attempt_id,question_id),
    FOREIGN KEY (attempt_id,school_id) REFERENCES assessment_attempts(id,school_id) ON DELETE CASCADE,
    FOREIGN KEY (question_id,school_id) REFERENCES questions(id,school_id)
);

-- +goose Down
DROP TABLE IF EXISTS assessment_answers, assessment_attempts, student_invitations;
ALTER TABLE students DROP CONSTRAINT IF EXISTS students_user_school_fk;
