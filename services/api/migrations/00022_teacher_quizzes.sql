-- +goose Up
-- Teachers' quizzes and assignments are assessments on the same engine as exams,
-- so marking, saving answers and skill tracking are shared.
ALTER TABLE public.assessments
    ADD COLUMN kind text NOT NULL DEFAULT 'exam' CHECK (kind IN ('exam','quiz','assignment')),
    ADD COLUMN class_subject_id uuid,
    ADD COLUMN resource_id uuid REFERENCES public.teaching_resources(id) ON DELETE SET NULL,
    ADD COLUMN show_answers boolean NOT NULL DEFAULT true,
    ADD COLUMN mark_item_id uuid REFERENCES public.mark_items(id) ON DELETE SET NULL,
    ADD FOREIGN KEY (class_subject_id, school_id) REFERENCES public.class_subjects(id, school_id);
CREATE INDEX assessments_class_subject ON public.assessments (class_subject_id) WHERE class_subject_id IS NOT NULL;

-- An assignment stays open for days, up to 30.
ALTER TABLE public.assessments DROP CONSTRAINT assessments_duration_minutes_check;
ALTER TABLE public.assessments ADD CONSTRAINT assessments_duration_minutes_check CHECK (duration_minutes BETWEEN 5 AND 43200);

-- +goose Down
UPDATE public.assessments SET duration_minutes=240 WHERE duration_minutes>240;
ALTER TABLE public.assessments DROP CONSTRAINT assessments_duration_minutes_check;
ALTER TABLE public.assessments ADD CONSTRAINT assessments_duration_minutes_check CHECK (duration_minutes BETWEEN 5 AND 240);
DROP INDEX IF EXISTS public.assessments_class_subject;
ALTER TABLE public.assessments
    DROP COLUMN IF EXISTS mark_item_id,
    DROP COLUMN IF EXISTS show_answers,
    DROP COLUMN IF EXISTS resource_id,
    DROP COLUMN IF EXISTS class_subject_id,
    DROP COLUMN IF EXISTS kind;
