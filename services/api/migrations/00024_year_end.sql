-- +goose Up
-- A closed year is kept as a record: its registers, marks and work can be read
-- but not changed until an administrator reopens it.
ALTER TABLE public.academic_years
    ADD COLUMN closed_at timestamptz,
    ADD COLUMN closed_by uuid;

-- How a student's time in a class ended: moved during the year, promoted,
-- repeating the grade, graduated, or left the school.
UPDATE public.enrollments SET status='ended' WHERE status NOT IN ('active','ended');
ALTER TABLE public.enrollments ADD CONSTRAINT enrollments_status_check
    CHECK (status IN ('active','ended','promoted','repeated','graduated','left'));

-- A student who finished school or left keeps their records but cannot sign in.
UPDATE public.students SET status='inactive' WHERE status NOT IN ('active','inactive');
ALTER TABLE public.students ADD CONSTRAINT students_status_check
    CHECK (status IN ('active','inactive','graduated','left'));

-- +goose Down
ALTER TABLE public.students DROP CONSTRAINT IF EXISTS students_status_check;
UPDATE public.students SET status='inactive' WHERE status IN ('graduated','left');
ALTER TABLE public.enrollments DROP CONSTRAINT IF EXISTS enrollments_status_check;
ALTER TABLE public.academic_years DROP COLUMN IF EXISTS closed_by, DROP COLUMN IF EXISTS closed_at;
