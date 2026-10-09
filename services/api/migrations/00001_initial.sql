-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE schools (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'Africa/Addis_Ababa',
    default_language TEXT NOT NULL DEFAULT 'en',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id),
    email TEXT,
    phone TEXT,
    password_hash TEXT NOT NULL,
    display_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled','pending')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (school_id,email)
);
CREATE UNIQUE INDEX users_email_lower_unique ON users (school_id, lower(email)) WHERE email IS NOT NULL;

CREATE TABLE user_roles (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('school_admin','teacher','student','parent')),
    PRIMARY KEY (user_id,role)
);

CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_active ON sessions (user_id,expires_at) WHERE revoked_at IS NULL;

CREATE TABLE students (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id),
    user_id UUID REFERENCES users(id),
    student_number TEXT NOT NULL,
    legal_name TEXT NOT NULL,
    date_of_birth DATE,
    preferred_language TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (school_id,student_number),
    UNIQUE (id,school_id),
    UNIQUE (user_id)
);

CREATE TABLE academic_years (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id),
    name TEXT NOT NULL,
    starts_on DATE NOT NULL,
    ends_on DATE NOT NULL,
    UNIQUE (school_id,name),
    UNIQUE (id,school_id),
    CHECK (ends_on > starts_on)
);

CREATE TABLE classes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id),
    academic_year_id UUID NOT NULL,
    name TEXT NOT NULL,
    grade_level TEXT NOT NULL,
    UNIQUE (id,school_id),
    FOREIGN KEY (academic_year_id,school_id) REFERENCES academic_years(id,school_id)
);

CREATE TABLE enrollments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id),
    student_id UUID NOT NULL,
    class_id UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    enrolled_at DATE NOT NULL DEFAULT CURRENT_DATE,
    ended_at DATE,
    CHECK (ended_at IS NULL OR ended_at >= enrolled_at),
    FOREIGN KEY (student_id,school_id) REFERENCES students(id,school_id),
    FOREIGN KEY (class_id,school_id) REFERENCES classes(id,school_id)
);
CREATE INDEX enrollments_student ON enrollments(student_id);
CREATE INDEX enrollments_class_active ON enrollments(class_id) WHERE status='active';

CREATE TABLE grading_scales (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID REFERENCES schools(id),
    name TEXT NOT NULL,
    scale_definition JSONB NOT NULL,
    source_description TEXT
);

CREATE TABLE academic_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    student_id UUID NOT NULL REFERENCES students(id),
    school_name TEXT NOT NULL,
    academic_year_label TEXT NOT NULL,
    grade_level TEXT NOT NULL,
    subject_name TEXT NOT NULL,
    score NUMERIC(8,3),
    maximum_score NUMERIC(8,3),
    grade_label TEXT,
    grading_scale_id UUID REFERENCES grading_scales(id),
    source_type TEXT NOT NULL CHECK (source_type IN ('school_import','staff_entry','verified_document')),
    verification_status TEXT NOT NULL DEFAULT 'unverified',
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (score IS NULL OR score >= 0),
    CHECK (maximum_score IS NULL OR maximum_score > 0),
    CHECK (score IS NULL OR maximum_score IS NULL OR score <= maximum_score)
);
CREATE INDEX academic_records_student_date ON academic_records(student_id,recorded_at DESC);

CREATE TABLE external_exams (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    student_id UUID NOT NULL REFERENCES students(id),
    exam_name TEXT NOT NULL,
    exam_provider TEXT,
    exam_date DATE,
    subject_or_section TEXT,
    score NUMERIC(10,3),
    maximum_score NUMERIC(10,3),
    score_label TEXT,
    verification_status TEXT NOT NULL DEFAULT 'unverified',
    source_metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE external_exam_breakdowns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_exam_id UUID NOT NULL REFERENCES external_exams(id) ON DELETE CASCADE,
    section_name TEXT NOT NULL,
    score NUMERIC(10,3),
    maximum_score NUMERIC(10,3),
    score_label TEXT,
    source_provided BOOLEAN NOT NULL DEFAULT true
);

CREATE TABLE audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id),
    actor_user_id UUID REFERENCES users(id),
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id UUID,
    changes JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_school_date ON audit_logs(school_id,created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS audit_logs, external_exam_breakdowns, external_exams, academic_records,
    grading_scales, enrollments, classes, academic_years, students, sessions, user_roles, users, schools CASCADE;
