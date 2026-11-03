-- +goose Up
-- A parent with two children has one account linked to two guardian rows.
ALTER TABLE public.guardians ADD COLUMN user_id uuid;
ALTER TABLE public.guardians ADD FOREIGN KEY (user_id, school_id) REFERENCES public.users(id, school_id);
CREATE INDEX guardians_user ON public.guardians (user_id) WHERE user_id IS NOT NULL;

CREATE TABLE public.guardian_invitations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    guardian_id uuid NOT NULL REFERENCES public.guardians(id) ON DELETE CASCADE,
    email text NOT NULL,
    token_hash bytea NOT NULL UNIQUE,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz,
    revoked_at timestamptz,
    FOREIGN KEY (created_by, school_id) REFERENCES public.users(id, school_id)
);
CREATE UNIQUE INDEX guardian_invitations_pending ON public.guardian_invitations (guardian_id) WHERE accepted_at IS NULL AND revoked_at IS NULL;
CREATE INDEX guardian_invitations_email ON public.guardian_invitations (school_id, lower(email)) WHERE accepted_at IS NULL AND revoked_at IS NULL;

ALTER TABLE public.guardian_invitations ENABLE ROW LEVEL SECURITY;
REVOKE ALL PRIVILEGES ON TABLE public.guardian_invitations FROM PUBLIC, anon, authenticated;

-- +goose Down
DROP TABLE IF EXISTS public.guardian_invitations;
DROP INDEX IF EXISTS public.guardians_user;
ALTER TABLE public.guardians DROP COLUMN IF EXISTS user_id;
