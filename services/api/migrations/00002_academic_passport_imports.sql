-- +goose Up
CREATE TABLE import_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id),
    requested_by UUID NOT NULL REFERENCES users(id),
    status TEXT NOT NULL DEFAULT 'previewed' CHECK (status IN ('previewed','committed')),
    payload JSONB NOT NULL,
    row_errors JSONB NOT NULL DEFAULT '[]',
    row_count INT NOT NULL CHECK (row_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    committed_at TIMESTAMPTZ
);
CREATE INDEX import_jobs_school_recent ON import_jobs(school_id,created_at DESC);

ALTER TABLE external_exams
    ADD CONSTRAINT external_exams_score_nonnegative CHECK (score IS NULL OR score >= 0),
    ADD CONSTRAINT external_exams_maximum_positive CHECK (maximum_score IS NULL OR maximum_score > 0),
    ADD CONSTRAINT external_exams_score_within_maximum CHECK (score IS NULL OR maximum_score IS NULL OR score <= maximum_score);

ALTER TABLE external_exam_breakdowns
    ADD CONSTRAINT external_exam_breakdowns_score_nonnegative CHECK (score IS NULL OR score >= 0),
    ADD CONSTRAINT external_exam_breakdowns_maximum_positive CHECK (maximum_score IS NULL OR maximum_score > 0),
    ADD CONSTRAINT external_exam_breakdowns_score_within_maximum CHECK (score IS NULL OR maximum_score IS NULL OR score <= maximum_score);

-- +goose Down
ALTER TABLE external_exam_breakdowns DROP CONSTRAINT IF EXISTS external_exam_breakdowns_score_within_maximum;
ALTER TABLE external_exam_breakdowns DROP CONSTRAINT IF EXISTS external_exam_breakdowns_maximum_positive;
ALTER TABLE external_exam_breakdowns DROP CONSTRAINT IF EXISTS external_exam_breakdowns_score_nonnegative;
ALTER TABLE external_exams DROP CONSTRAINT IF EXISTS external_exams_score_within_maximum;
ALTER TABLE external_exams DROP CONSTRAINT IF EXISTS external_exams_maximum_positive;
ALTER TABLE external_exams DROP CONSTRAINT IF EXISTS external_exams_score_nonnegative;
DROP TABLE IF EXISTS import_jobs;
