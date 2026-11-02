-- +goose Up
-- A Grade 11 or 12 student sits the natural or the social science papers.
ALTER TABLE public.students
    ADD COLUMN stream text CHECK (stream IN ('natural','social')),
    ADD COLUMN prep_onboarded_at timestamptz;

-- A subject can belong to one of the nine national exam papers. A question's
-- paper comes from the subject of the skill it is tagged to.
ALTER TABLE public.subjects
    ADD COLUMN national_paper text CHECK (national_paper IN ('english','mathematics','aptitude','physics','chemistry','biology','history','geography','economics'));
CREATE INDEX subjects_national_paper ON public.subjects (national_paper) WHERE national_paper IS NOT NULL;

-- Exam-only questions stay out of student practice, so school mocks can use
-- questions students have not already seen answered.
ALTER TABLE public.questions ADD COLUMN exam_only boolean NOT NULL DEFAULT false;

-- A school mock paper runs on the secure exam engine and counts toward readiness.
ALTER TABLE public.assessments
    ADD COLUMN national_paper text CHECK (national_paper IN ('english','mathematics','aptitude','physics','chemistry','biology','history','geography','economics'));

-- A full mock national exam: six timed papers taken by one student.
CREATE TABLE public.prep_mocks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    student_id uuid NOT NULL,
    stream text NOT NULL CHECK (stream IN ('natural','social')),
    status text NOT NULL DEFAULT 'in_progress' CHECK (status IN ('in_progress','finished')),
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    UNIQUE (id, school_id),
    FOREIGN KEY (student_id, school_id) REFERENCES public.students(id, school_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX prep_mocks_one_open ON public.prep_mocks (student_id) WHERE status = 'in_progress';

-- One sitting of practice. Drills show the answer after each question; timed
-- papers show nothing until they finish.
CREATE TABLE public.prep_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    student_id uuid NOT NULL,
    mock_id uuid,
    mode text NOT NULL CHECK (mode IN ('drill','weak','review','timed')),
    paper text NOT NULL CHECK (paper IN ('english','mathematics','aptitude','physics','chemistry','biology','history','geography','economics')),
    status text NOT NULL DEFAULT 'in_progress' CHECK (status IN ('in_progress','finished')),
    started_at timestamptz NOT NULL DEFAULT now(),
    deadline_at timestamptz,
    finished_at timestamptz,
    correct_count int NOT NULL DEFAULT 0,
    question_count int NOT NULL CHECK (question_count > 0),
    UNIQUE (id, school_id),
    UNIQUE (mock_id, paper),
    CHECK (mode = 'timed' OR deadline_at IS NULL),
    CHECK (mock_id IS NULL OR mode = 'timed'),
    FOREIGN KEY (student_id, school_id) REFERENCES public.students(id, school_id) ON DELETE CASCADE,
    FOREIGN KEY (mock_id, school_id) REFERENCES public.prep_mocks(id, school_id) ON DELETE CASCADE
);
CREATE INDEX prep_sessions_student ON public.prep_sessions (student_id, started_at DESC);
CREATE INDEX prep_sessions_open ON public.prep_sessions (started_at) WHERE status = 'in_progress';

CREATE TABLE public.prep_session_items (
    session_id uuid NOT NULL,
    school_id uuid NOT NULL,
    question_id uuid NOT NULL,
    position int NOT NULL CHECK (position > 0),
    option_order text[] NOT NULL,
    answer text,
    is_correct boolean,
    flagged boolean NOT NULL DEFAULT false,
    answered_at timestamptz,
    PRIMARY KEY (session_id, question_id),
    UNIQUE (session_id, position),
    FOREIGN KEY (session_id, school_id) REFERENCES public.prep_sessions(id, school_id) ON DELETE CASCADE,
    FOREIGN KEY (question_id, school_id) REFERENCES public.questions(id, school_id)
);
CREATE INDEX prep_session_items_question ON public.prep_session_items (question_id);

-- Practice counts as skill evidence as well as exam attempts.
ALTER TABLE public.skill_evidence
    ALTER COLUMN attempt_id DROP NOT NULL,
    ADD COLUMN prep_session_id uuid,
    ADD CONSTRAINT skill_evidence_one_source CHECK ((attempt_id IS NULL) <> (prep_session_id IS NULL)),
    ADD CONSTRAINT skill_evidence_prep_unique UNIQUE (prep_session_id, question_id, skill_id),
    ADD CONSTRAINT skill_evidence_prep_session_fk FOREIGN KEY (prep_session_id, school_id) REFERENCES public.prep_sessions(id, school_id) ON DELETE CASCADE;

ALTER TABLE public.prep_mocks ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.prep_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.prep_session_items ENABLE ROW LEVEL SECURITY;
REVOKE ALL PRIVILEGES ON TABLE public.prep_mocks FROM PUBLIC, anon, authenticated;
REVOKE ALL PRIVILEGES ON TABLE public.prep_sessions FROM PUBLIC, anon, authenticated;
REVOKE ALL PRIVILEGES ON TABLE public.prep_session_items FROM PUBLIC, anon, authenticated;

-- +goose Down
DELETE FROM public.skill_evidence WHERE prep_session_id IS NOT NULL;
ALTER TABLE public.skill_evidence
    DROP CONSTRAINT IF EXISTS skill_evidence_prep_session_fk,
    DROP CONSTRAINT IF EXISTS skill_evidence_prep_unique,
    DROP CONSTRAINT IF EXISTS skill_evidence_one_source,
    DROP COLUMN IF EXISTS prep_session_id,
    ALTER COLUMN attempt_id SET NOT NULL;
DROP TABLE IF EXISTS public.prep_session_items, public.prep_sessions, public.prep_mocks;
ALTER TABLE public.assessments DROP COLUMN IF EXISTS national_paper;
ALTER TABLE public.questions DROP COLUMN IF EXISTS exam_only;
DROP INDEX IF EXISTS public.subjects_national_paper;
ALTER TABLE public.subjects DROP COLUMN IF EXISTS national_paper;
ALTER TABLE public.students
    DROP COLUMN IF EXISTS prep_onboarded_at,
    DROP COLUMN IF EXISTS stream;
