-- +goose Up
CREATE TABLE public.finance_entries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    entry_type text NOT NULL CHECK (entry_type IN ('tuition_charge','payment','expense','salary')),
    category text NOT NULL,
    description text NOT NULL,
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency_code char(3) NOT NULL DEFAULT 'ETB',
    student_id uuid,
    staff_user_id uuid,
    due_on date,
    paid_on date,
    status text NOT NULL DEFAULT 'recorded' CHECK (status IN ('recorded','paid','void')),
    reference text NOT NULL DEFAULT '',
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (student_id, school_id) REFERENCES public.students(id, school_id),
    FOREIGN KEY (staff_user_id, school_id) REFERENCES public.users(id, school_id),
    FOREIGN KEY (created_by, school_id) REFERENCES public.users(id, school_id),
    CHECK ((entry_type <> 'salary') OR staff_user_id IS NOT NULL),
    CHECK ((entry_type <> 'tuition_charge' AND entry_type <> 'payment') OR student_id IS NOT NULL)
);
CREATE INDEX finance_entries_school_date ON public.finance_entries(school_id, created_at DESC);

CREATE TABLE public.lesson_plans (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    class_id uuid NOT NULL,
    teacher_id uuid NOT NULL,
    subject text NOT NULL,
    lesson_date date NOT NULL,
    duration_minutes integer NOT NULL CHECK(duration_minutes BETWEEN 5 AND 480),
    topic text NOT NULL,
    objectives text NOT NULL,
    prior_knowledge text NOT NULL DEFAULT '',
    materials text NOT NULL DEFAULT '',
    lesson_phases jsonb NOT NULL DEFAULT '[]',
    differentiation text NOT NULL DEFAULT '',
    assessment text NOT NULL DEFAULT '',
    homework text NOT NULL DEFAULT '',
    reflection text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','submitted','approved','needs_revision')),
    reviewed_by uuid,
    reviewed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(class_id, school_id) REFERENCES public.classes(id, school_id) ON DELETE CASCADE,
    FOREIGN KEY(teacher_id, school_id) REFERENCES public.users(id, school_id),
    FOREIGN KEY(reviewed_by, school_id) REFERENCES public.users(id, school_id)
);
CREATE INDEX lesson_plans_teacher_date ON public.lesson_plans(school_id, teacher_id, lesson_date DESC);

CREATE TABLE public.teaching_resources (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    class_id uuid NOT NULL,
    uploaded_by uuid NOT NULL,
    title text NOT NULL,
    description text NOT NULL DEFAULT '',
    subject text NOT NULL DEFAULT '',
    file_name text NOT NULL,
    media_type text NOT NULL,
    file_size bigint NOT NULL CHECK(file_size BETWEEN 1 AND 15728640),
    file_data bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(class_id, school_id) REFERENCES public.classes(id, school_id) ON DELETE CASCADE,
    FOREIGN KEY(uploaded_by, school_id) REFERENCES public.users(id, school_id)
);
CREATE INDEX teaching_resources_class_date ON public.teaching_resources(school_id, class_id, created_at DESC);

ALTER TABLE public.finance_entries ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.lesson_plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.teaching_resources ENABLE ROW LEVEL SECURITY;
REVOKE ALL PRIVILEGES ON TABLE public.finance_entries, public.lesson_plans, public.teaching_resources FROM PUBLIC, anon, authenticated;

-- +goose Down
DROP TABLE IF EXISTS public.teaching_resources;
DROP TABLE IF EXISTS public.lesson_plans;
DROP TABLE IF EXISTS public.finance_entries;
