-- +goose Up
CREATE TABLE public.academic_terms (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    academic_year_id uuid NOT NULL,
    name text NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 80),
    starts_on date NOT NULL,
    ends_on date NOT NULL CHECK (ends_on >= starts_on),
    UNIQUE (id, school_id),
    UNIQUE (academic_year_id, name),
    FOREIGN KEY (academic_year_id, school_id) REFERENCES public.academic_years(id, school_id) ON DELETE CASCADE
);

-- A subject as one class takes it. With no teacher named, any teacher of the
-- class can enter its marks.
CREATE TABLE public.class_subjects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    class_id uuid NOT NULL,
    name text NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 120),
    teacher_id uuid,
    counts_in_average boolean NOT NULL DEFAULT true,
    sort_order integer NOT NULL DEFAULT 0,
    UNIQUE (id, school_id),
    FOREIGN KEY (class_id, school_id) REFERENCES public.classes(id, school_id) ON DELETE CASCADE,
    FOREIGN KEY (teacher_id, school_id) REFERENCES public.users(id, school_id)
);
CREATE UNIQUE INDEX class_subjects_name ON public.class_subjects (class_id, lower(name));

-- One test, assignment or exam. The weights of a subject's items in a term add up to 100.
CREATE TABLE public.mark_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    class_subject_id uuid NOT NULL,
    term_id uuid NOT NULL,
    title text NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 120),
    weight numeric(5,2) NOT NULL CHECK (weight > 0 AND weight <= 100),
    max_score numeric(6,2) NOT NULL CHECK (max_score > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, school_id),
    UNIQUE (class_subject_id, term_id, title),
    FOREIGN KEY (class_subject_id, school_id) REFERENCES public.class_subjects(id, school_id),
    FOREIGN KEY (term_id, school_id) REFERENCES public.academic_terms(id, school_id)
);
CREATE INDEX mark_items_term ON public.mark_items (term_id);

-- A blank mark has no row, so a missing mark is never mistaken for a zero.
CREATE TABLE public.marks (
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    item_id uuid NOT NULL,
    student_id uuid NOT NULL,
    score numeric(6,2) NOT NULL CHECK (score >= 0),
    recorded_by uuid NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (item_id, student_id),
    FOREIGN KEY (item_id, school_id) REFERENCES public.mark_items(id, school_id) ON DELETE CASCADE,
    FOREIGN KEY (student_id, school_id) REFERENCES public.students(id, school_id) ON DELETE CASCADE,
    FOREIGN KEY (recorded_by, school_id) REFERENCES public.users(id, school_id)
);
CREATE INDEX marks_student ON public.marks (student_id);

CREATE TABLE public.report_settings (
    school_id uuid PRIMARY KEY REFERENCES public.schools(id) ON DELETE CASCADE,
    pass_percent numeric(5,2) NOT NULL CHECK (pass_percent BETWEEN 0 AND 100),
    grade_bands jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE public.report_notes (
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    term_id uuid NOT NULL,
    class_id uuid NOT NULL,
    student_id uuid NOT NULL,
    conduct text NOT NULL DEFAULT '',
    comment text NOT NULL DEFAULT '',
    updated_by uuid NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (term_id, class_id, student_id),
    FOREIGN KEY (term_id, school_id) REFERENCES public.academic_terms(id, school_id) ON DELETE CASCADE,
    FOREIGN KEY (class_id, school_id) REFERENCES public.classes(id, school_id) ON DELETE CASCADE,
    FOREIGN KEY (student_id, school_id) REFERENCES public.students(id, school_id) ON DELETE CASCADE,
    FOREIGN KEY (updated_by, school_id) REFERENCES public.users(id, school_id)
);

-- Publishing keeps a copy of each card, so an issued report never changes when
-- a student later moves class or the school changes its grade bands.
CREATE TABLE public.report_publications (
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    term_id uuid NOT NULL,
    class_id uuid NOT NULL,
    published_by uuid NOT NULL,
    published_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (term_id, class_id),
    FOREIGN KEY (term_id, school_id) REFERENCES public.academic_terms(id, school_id),
    FOREIGN KEY (class_id, school_id) REFERENCES public.classes(id, school_id) ON DELETE CASCADE,
    FOREIGN KEY (published_by, school_id) REFERENCES public.users(id, school_id)
);

CREATE TABLE public.report_cards (
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    term_id uuid NOT NULL,
    class_id uuid NOT NULL,
    student_id uuid NOT NULL,
    card jsonb NOT NULL,
    PRIMARY KEY (term_id, class_id, student_id),
    FOREIGN KEY (term_id, class_id) REFERENCES public.report_publications(term_id, class_id) ON DELETE CASCADE,
    FOREIGN KEY (student_id, school_id) REFERENCES public.students(id, school_id) ON DELETE CASCADE
);
CREATE INDEX report_cards_student ON public.report_cards (student_id);

ALTER TABLE public.academic_terms ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.class_subjects ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.mark_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.marks ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.report_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.report_notes ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.report_publications ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.report_cards ENABLE ROW LEVEL SECURITY;
REVOKE ALL PRIVILEGES ON TABLE public.academic_terms, public.class_subjects, public.mark_items, public.marks, public.report_settings, public.report_notes, public.report_publications, public.report_cards FROM PUBLIC, anon, authenticated;

-- +goose Down
DROP TABLE IF EXISTS public.report_cards, public.report_publications, public.report_notes, public.report_settings, public.marks, public.mark_items, public.class_subjects, public.academic_terms;
