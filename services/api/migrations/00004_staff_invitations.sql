-- +goose Up
ALTER TABLE users ADD CONSTRAINT users_id_school_id_unique UNIQUE (id, school_id);

CREATE TABLE staff_invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('teacher')),
    token_hash BYTEA NOT NULL UNIQUE,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    FOREIGN KEY (created_by, school_id) REFERENCES users(id, school_id)
);
CREATE UNIQUE INDEX staff_invitations_pending_email
    ON staff_invitations (school_id, lower(email))
    WHERE accepted_at IS NULL AND revoked_at IS NULL;
CREATE INDEX staff_invitations_school_created
    ON staff_invitations (school_id, created_at DESC);

CREATE TABLE class_teachers (
    school_id UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    class_id UUID NOT NULL,
    teacher_id UUID NOT NULL,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (class_id, teacher_id),
    FOREIGN KEY (class_id, school_id) REFERENCES classes(id, school_id) ON DELETE CASCADE,
    FOREIGN KEY (teacher_id, school_id) REFERENCES users(id, school_id) ON DELETE CASCADE
);
CREATE INDEX class_teachers_teacher ON class_teachers (teacher_id, school_id);

-- +goose Down
DROP TABLE IF EXISTS class_teachers;
DROP TABLE IF EXISTS staff_invitations;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_id_school_id_unique;
