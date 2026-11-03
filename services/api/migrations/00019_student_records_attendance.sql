-- +goose Up
ALTER TABLE public.students
    ADD COLUMN gender text CHECK (gender IN ('female','male')),
    ADD COLUMN address text,
    ADD COLUMN medical_notes text;

-- A guardian belongs to one student for now. A parent with two children has two
-- rows, which the parent portal can later link through their phone or email.
CREATE TABLE public.guardians (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    student_id uuid NOT NULL,
    full_name text NOT NULL CHECK (length(trim(full_name)) BETWEEN 1 AND 200),
    relationship text NOT NULL CHECK (relationship IN ('mother','father','guardian','other')),
    phone text NOT NULL,
    email text,
    is_primary boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (student_id, school_id) REFERENCES public.students(id, school_id) ON DELETE CASCADE
);
CREATE INDEX guardians_student ON public.guardians (student_id);
CREATE UNIQUE INDEX guardians_one_primary ON public.guardians (student_id) WHERE is_primary;

-- One mark per student, class and day. A day with no rows for a class means its
-- register was not taken, which is different from everyone being absent.
CREATE TABLE public.attendance_records (
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    student_id uuid NOT NULL,
    class_id uuid NOT NULL,
    attendance_date date NOT NULL,
    status text NOT NULL CHECK (status IN ('present','absent','late','excused')),
    note text NOT NULL DEFAULT '',
    recorded_by uuid NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (student_id, class_id, attendance_date),
    FOREIGN KEY (student_id, school_id) REFERENCES public.students(id, school_id) ON DELETE CASCADE,
    FOREIGN KEY (class_id, school_id) REFERENCES public.classes(id, school_id) ON DELETE CASCADE,
    FOREIGN KEY (recorded_by, school_id) REFERENCES public.users(id, school_id)
);
CREATE INDEX attendance_class_date ON public.attendance_records (school_id, class_id, attendance_date);
CREATE INDEX attendance_student_date ON public.attendance_records (student_id, attendance_date DESC);

ALTER TABLE public.guardians ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.attendance_records ENABLE ROW LEVEL SECURITY;
REVOKE ALL PRIVILEGES ON TABLE public.guardians FROM PUBLIC, anon, authenticated;
REVOKE ALL PRIVILEGES ON TABLE public.attendance_records FROM PUBLIC, anon, authenticated;

-- +goose Down
DROP TABLE IF EXISTS public.attendance_records, public.guardians;
ALTER TABLE public.students
    DROP COLUMN IF EXISTS medical_notes,
    DROP COLUMN IF EXISTS address,
    DROP COLUMN IF EXISTS gender;
