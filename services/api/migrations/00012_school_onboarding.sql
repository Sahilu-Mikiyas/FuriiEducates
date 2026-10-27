-- +goose Up
ALTER TABLE public.demo_requests
    ADD COLUMN timezone text NOT NULL DEFAULT 'Africa/Addis_Ababa',
    ADD COLUMN region text NOT NULL DEFAULT 'ET',
    ADD COLUMN default_language text NOT NULL DEFAULT 'en',
    ADD COLUMN priorities text[] NOT NULL DEFAULT ARRAY['student-records'],
    ADD COLUMN prepared_school_id uuid UNIQUE REFERENCES public.schools(id);

UPDATE public.demo_requests SET priorities = ARRAY['student-records'] WHERE cardinality(priorities) = 0;

CREATE TABLE public.school_setup_profiles (
    school_id uuid PRIMARY KEY REFERENCES public.schools(id) ON DELETE CASCADE,
    learner_count_band text NOT NULL CHECK (learner_count_band IN ('1-100','101-500','501-1500','1501+')),
    region text NOT NULL CHECK (char_length(region) BETWEEN 2 AND 80),
    priorities text[] NOT NULL DEFAULT '{}',
    source_request_id uuid UNIQUE REFERENCES public.demo_requests(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (cardinality(priorities) BETWEEN 1 AND 7)
);

ALTER TABLE public.school_setup_profiles ENABLE ROW LEVEL SECURITY;
REVOKE ALL PRIVILEGES ON TABLE public.school_setup_profiles FROM PUBLIC, anon, authenticated;

CREATE UNIQUE INDEX users_email_global_unique ON public.users (lower(email)) WHERE email IS NOT NULL;
CREATE UNIQUE INDEX staff_admin_invites_pending_email ON public.staff_invitations (lower(email)) WHERE role = 'school_admin' AND accepted_at IS NULL AND revoked_at IS NULL;

ALTER TABLE public.staff_invitations ALTER COLUMN created_by DROP NOT NULL;
ALTER TABLE public.staff_invitations DROP CONSTRAINT staff_invitations_role_check;
ALTER TABLE public.staff_invitations ADD CONSTRAINT staff_invitations_role_check CHECK (role IN ('teacher','school_admin'));

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION '00012_school_onboarding is intentionally irreversible because it stores school setup data';
END
$$;
-- +goose StatementEnd

