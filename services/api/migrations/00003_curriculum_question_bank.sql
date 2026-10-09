-- +goose Up
CREATE TABLE curricula (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    country_code TEXT,
    version_label TEXT NOT NULL,
    language_code TEXT NOT NULL DEFAULT 'en',
    source_reference TEXT,
    license_notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, name, version_label, language_code)
);

CREATE TABLE subjects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    curriculum_id UUID NOT NULL REFERENCES curricula(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    grade_level TEXT,
    subject_code TEXT,
    UNIQUE (curriculum_id, name, grade_level)
);

CREATE TABLE curriculum_nodes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subject_id UUID NOT NULL REFERENCES subjects(id) ON DELETE CASCADE,
    parent_id UUID REFERENCES curriculum_nodes(id) ON DELETE RESTRICT,
    node_type TEXT NOT NULL CHECK (node_type IN ('unit','topic','concept','skill','objective')),
    code TEXT,
    title TEXT NOT NULL,
    description TEXT,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX curriculum_nodes_subject_tree ON curriculum_nodes(subject_id,parent_id,sort_order);

CREATE TABLE skill_prerequisites (
    skill_id UUID NOT NULL REFERENCES curriculum_nodes(id) ON DELETE CASCADE,
    prerequisite_skill_id UUID NOT NULL REFERENCES curriculum_nodes(id) ON DELETE CASCADE,
    PRIMARY KEY (skill_id,prerequisite_skill_id),
    CHECK (skill_id <> prerequisite_skill_id)
);

CREATE TABLE curriculum_mappings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_node_id UUID NOT NULL REFERENCES curriculum_nodes(id) ON DELETE CASCADE,
    target_node_id UUID NOT NULL REFERENCES curriculum_nodes(id) ON DELETE CASCADE,
    mapping_type TEXT NOT NULL CHECK (mapping_type IN ('equivalent','overlaps','prerequisite','related')),
    confidence NUMERIC(4,3) CHECK (confidence IS NULL OR confidence BETWEEN 0 AND 1),
    reviewed_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source_node_id,target_node_id,mapping_type),
    CHECK (source_node_id <> target_node_id)
);

CREATE TABLE questions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id UUID NOT NULL REFERENCES schools(id),
    question_type TEXT NOT NULL CHECK (question_type IN ('single_choice','multiple_choice','numeric','text')),
    prompt JSONB NOT NULL,
    options JSONB,
    answer_key JSONB NOT NULL,
    explanation JSONB,
    difficulty SMALLINT CHECK (difficulty BETWEEN 1 AND 5),
    language_code TEXT NOT NULL DEFAULT 'en',
    source_name TEXT,
    source_reference TEXT,
    license_notes TEXT,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','archived')),
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX questions_school_status ON questions(school_id,status,created_at DESC);

CREATE TABLE question_skill_tags (
    question_id UUID NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    skill_id UUID NOT NULL REFERENCES curriculum_nodes(id),
    weight NUMERIC(5,4) NOT NULL DEFAULT 1 CHECK (weight > 0 AND weight <= 1),
    PRIMARY KEY (question_id,skill_id)
);

-- +goose Down
DROP TABLE IF EXISTS question_skill_tags, questions, curriculum_mappings, skill_prerequisites,
    curriculum_nodes, subjects, curricula CASCADE;
