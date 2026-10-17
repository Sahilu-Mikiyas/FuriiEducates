-- +goose Up
ALTER TABLE questions ADD CONSTRAINT questions_id_school_id_unique UNIQUE (id, school_id);

CREATE TABLE assessments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 200),
    instructions TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','archived')),
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id,school_id),
    FOREIGN KEY (created_by,school_id) REFERENCES users(id,school_id)
);

CREATE TABLE assessment_items (
    assessment_id UUID NOT NULL,
    school_id UUID NOT NULL,
    question_id UUID NOT NULL,
    position INT NOT NULL CHECK (position > 0),
    points NUMERIC(7,2) NOT NULL DEFAULT 1 CHECK (points > 0),
    PRIMARY KEY (assessment_id,position),
    UNIQUE (assessment_id,question_id),
    FOREIGN KEY (assessment_id,school_id) REFERENCES assessments(id,school_id) ON DELETE CASCADE,
    FOREIGN KEY (question_id,school_id) REFERENCES questions(id,school_id)
);

CREATE TABLE assessment_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    assessment_id UUID NOT NULL,
    class_id UUID NOT NULL,
    assigned_by UUID NOT NULL,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    due_at TIMESTAMPTZ,
    UNIQUE (assessment_id,class_id),
    UNIQUE (id,school_id),
    FOREIGN KEY (assessment_id,school_id) REFERENCES assessments(id,school_id) ON DELETE CASCADE,
    FOREIGN KEY (class_id,school_id) REFERENCES classes(id,school_id) ON DELETE CASCADE,
    FOREIGN KEY (assigned_by,school_id) REFERENCES users(id,school_id)
);
CREATE INDEX assessment_assignments_class ON assessment_assignments (class_id,assigned_at DESC);

-- +goose Down
DROP TABLE IF EXISTS assessment_assignments, assessment_items, assessments;
ALTER TABLE questions DROP CONSTRAINT IF EXISTS questions_id_school_id_unique;
