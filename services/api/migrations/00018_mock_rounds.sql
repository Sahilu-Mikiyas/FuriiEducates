-- +goose Up
-- A mock round is one school-wide sitting of the national exam: one paper per
-- subject, on a timetable, across chosen classes.
CREATE TABLE public.mock_rounds (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 120),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, school_id),
    FOREIGN KEY (created_by, school_id) REFERENCES public.users(id, school_id)
);
CREATE INDEX mock_rounds_school ON public.mock_rounds (school_id, created_at DESC);

ALTER TABLE public.assessments ADD COLUMN mock_round_id uuid;
ALTER TABLE public.assessments ADD CONSTRAINT assessments_mock_round_fk
    FOREIGN KEY (mock_round_id, school_id) REFERENCES public.mock_rounds(id, school_id) ON DELETE CASCADE;
CREATE UNIQUE INDEX assessments_mock_round_paper ON public.assessments (mock_round_id, national_paper) WHERE mock_round_id IS NOT NULL;

-- A stream paper reaches only that stream's students, even in a mixed class.
-- Empty means everyone in the class, as with every other assignment.
ALTER TABLE public.assessment_assignments
    ADD COLUMN stream text CHECK (stream IN ('natural','social'));

ALTER TABLE public.mock_rounds ENABLE ROW LEVEL SECURITY;
REVOKE ALL PRIVILEGES ON TABLE public.mock_rounds FROM PUBLIC, anon, authenticated;

-- +goose Down
ALTER TABLE public.assessment_assignments DROP COLUMN IF EXISTS stream;
DELETE FROM public.assessments WHERE mock_round_id IS NOT NULL;
DROP INDEX IF EXISTS public.assessments_mock_round_paper;
ALTER TABLE public.assessments DROP CONSTRAINT IF EXISTS assessments_mock_round_fk;
ALTER TABLE public.assessments DROP COLUMN IF EXISTS mock_round_id;
DROP TABLE IF EXISTS public.mock_rounds;
