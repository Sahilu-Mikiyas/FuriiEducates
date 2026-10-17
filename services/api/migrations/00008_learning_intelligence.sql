-- +goose Up
CREATE TABLE skill_evidence (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    student_id UUID NOT NULL,
    skill_id UUID NOT NULL REFERENCES curriculum_nodes(id),
    attempt_id UUID NOT NULL,
    question_id UUID NOT NULL,
    correctness NUMERIC(5,4) NOT NULL CHECK (correctness BETWEEN 0 AND 1),
    evidence_weight NUMERIC(8,4) NOT NULL CHECK (evidence_weight > 0),
    observed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (attempt_id, question_id, skill_id),
    FOREIGN KEY (student_id, school_id) REFERENCES students(id, school_id) ON DELETE CASCADE,
    FOREIGN KEY (attempt_id, school_id) REFERENCES assessment_attempts(id, school_id) ON DELETE CASCADE,
    FOREIGN KEY (question_id, school_id) REFERENCES questions(id, school_id)
);
CREATE INDEX skill_evidence_student_skill ON skill_evidence(student_id,skill_id,observed_at DESC);

CREATE TABLE mastery_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    student_id UUID NOT NULL,
    skill_id UUID NOT NULL REFERENCES curriculum_nodes(id),
    estimated_mastery NUMERIC(5,2) NOT NULL CHECK (estimated_mastery BETWEEN 0 AND 100),
    confidence NUMERIC(5,2) NOT NULL CHECK (confidence BETWEEN 0 AND 100),
    evidence_count INT NOT NULL CHECK (evidence_count >= 0),
    algorithm_version TEXT NOT NULL,
    calculated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (student_id, school_id) REFERENCES students(id, school_id) ON DELETE CASCADE
);
CREATE INDEX mastery_snapshots_latest ON mastery_snapshots(student_id,skill_id,calculated_at DESC);

CREATE TABLE recommendations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    student_id UUID NOT NULL,
    skill_id UUID NOT NULL REFERENCES curriculum_nodes(id),
    reason_code TEXT NOT NULL,
    evidence_summary JSONB NOT NULL,
    recommended_action JSONB NOT NULL,
    priority TEXT NOT NULL CHECK (priority IN ('low','medium','high','urgent')),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','in_progress','completed','dismissed','superseded')),
    algorithm_version TEXT NOT NULL,
    baseline_mastery NUMERIC(5,2),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    accepted_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    FOREIGN KEY (student_id, school_id) REFERENCES students(id, school_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX recommendations_one_active_skill
    ON recommendations(student_id,skill_id) WHERE status IN ('pending','accepted','in_progress');
CREATE INDEX recommendations_student_status ON recommendations(student_id,status,created_at DESC);

CREATE TABLE intervention_outcomes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    recommendation_id UUID NOT NULL REFERENCES recommendations(id) ON DELETE CASCADE,
    post_attempt_id UUID NOT NULL,
    pre_mastery NUMERIC(5,2),
    post_mastery NUMERIC(5,2) NOT NULL,
    change_points NUMERIC(6,2),
    outcome_summary JSONB NOT NULL DEFAULT '{}',
    evaluated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (recommendation_id,post_attempt_id),
    FOREIGN KEY (post_attempt_id,school_id) REFERENCES assessment_attempts(id,school_id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS intervention_outcomes, recommendations, mastery_snapshots, skill_evidence;
