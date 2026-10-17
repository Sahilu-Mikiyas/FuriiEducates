-- +goose Up
ALTER TABLE assessment_answers
    ADD COLUMN feedback TEXT NOT NULL DEFAULT '',
    ADD COLUMN reviewed_by UUID,
    ADD COLUMN reviewed_at TIMESTAMPTZ,
    ADD CONSTRAINT assessment_answers_reviewed_by_fk
        FOREIGN KEY (reviewed_by, school_id) REFERENCES users(id, school_id);

ALTER TABLE assessment_attempts
    ADD COLUMN results_released BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN final_score NUMERIC(9,2),
    ADD COLUMN final_max_score NUMERIC(9,2);

UPDATE assessment_attempts at
SET results_released = NOT EXISTS (
        SELECT 1 FROM assessment_answers ans
        JOIN questions q ON q.id=ans.question_id AND q.school_id=ans.school_id
        WHERE ans.attempt_id=at.id AND q.question_type='text'
          AND ans.answer IS NOT NULL AND ans.answer <> 'null'::jsonb AND ans.reviewed_at IS NULL
    ),
    final_score = CASE WHEN NOT EXISTS (
        SELECT 1 FROM assessment_answers ans
        JOIN questions q ON q.id=ans.question_id AND q.school_id=ans.school_id
        WHERE ans.attempt_id=at.id AND q.question_type='text'
          AND ans.answer IS NOT NULL AND ans.answer <> 'null'::jsonb AND ans.reviewed_at IS NULL
    ) THEN COALESCE(score, 0) ELSE NULL END,
    final_max_score = CASE WHEN NOT EXISTS (
        SELECT 1 FROM assessment_answers ans
        JOIN questions q ON q.id=ans.question_id AND q.school_id=ans.school_id
        WHERE ans.attempt_id=at.id AND q.question_type='text'
          AND ans.answer IS NOT NULL AND ans.answer <> 'null'::jsonb AND ans.reviewed_at IS NULL
    ) THEN (
        SELECT COALESCE(sum(ai.points),0)
        FROM assessment_assignments aa
        JOIN assessment_items ai ON ai.assessment_id=aa.assessment_id AND ai.school_id=aa.school_id
        WHERE aa.id=at.assignment_id AND aa.school_id=at.school_id
    ) ELSE NULL END
WHERE status = 'submitted';

-- +goose Down
ALTER TABLE assessment_attempts
    DROP COLUMN IF EXISTS final_max_score,
    DROP COLUMN IF EXISTS final_score,
    DROP COLUMN IF EXISTS results_released;
ALTER TABLE assessment_answers
    DROP CONSTRAINT IF EXISTS assessment_answers_reviewed_by_fk,
    DROP COLUMN IF EXISTS reviewed_at,
    DROP COLUMN IF EXISTS reviewed_by,
    DROP COLUMN IF EXISTS feedback;
