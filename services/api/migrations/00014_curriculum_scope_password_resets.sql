-- +goose Up
-- Curricula become private to the school that created them. A curriculum
-- with no school is a platform library: readable by every school, writable
-- by none. Existing curricula take their owner from the audit log; on a
-- single-school installation, that school owns everything left over.
ALTER TABLE public.curricula ADD COLUMN school_id uuid REFERENCES public.schools(id) ON DELETE CASCADE;

UPDATE public.curricula c
SET school_id = created.school_id
FROM (
    SELECT DISTINCT ON (entity_id) entity_id, school_id
    FROM public.audit_logs
    WHERE action = 'curriculum.created' AND entity_type = 'curriculum'
    ORDER BY entity_id, created_at
) AS created
WHERE created.entity_id = c.id;

UPDATE public.curricula
SET school_id = (SELECT id FROM public.schools LIMIT 1)
WHERE school_id IS NULL AND (SELECT count(*) FROM public.schools) = 1;

-- +goose StatementBegin
DO $$
DECLARE
    constraint_name text;
BEGIN
    SELECT conname INTO constraint_name
    FROM pg_constraint
    WHERE conrelid = 'public.curricula'::regclass AND contype = 'u';
    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE public.curricula DROP CONSTRAINT %I', constraint_name);
    END IF;
END
$$;
-- +goose StatementEnd

CREATE UNIQUE INDEX curricula_school_version_unique
    ON public.curricula (COALESCE(school_id, '00000000-0000-0000-0000-000000000000'::uuid), provider, name, version_label, language_code);
CREATE INDEX curricula_school ON public.curricula (school_id);

-- Mappings belong to the school that reviewed them.
ALTER TABLE public.curriculum_mappings ADD COLUMN school_id uuid REFERENCES public.schools(id) ON DELETE CASCADE;

UPDATE public.curriculum_mappings m
SET school_id = u.school_id
FROM public.users u
WHERE u.id = m.reviewed_by;

UPDATE public.curriculum_mappings
SET school_id = (SELECT id FROM public.schools LIMIT 1)
WHERE school_id IS NULL AND (SELECT count(*) FROM public.schools) = 1;

-- +goose StatementBegin
DO $$
DECLARE
    constraint_name text;
BEGIN
    SELECT conname INTO constraint_name
    FROM pg_constraint
    WHERE conrelid = 'public.curriculum_mappings'::regclass AND contype = 'u';
    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE public.curriculum_mappings DROP CONSTRAINT %I', constraint_name);
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE public.curriculum_mappings
    ADD CONSTRAINT curriculum_mappings_school_unique UNIQUE (school_id, source_node_id, target_node_id, mapping_type);

-- One-time password reset links issued by a school administrator or a
-- platform operator. Only a SHA-256 digest of each token is stored.
CREATE TABLE public.password_reset_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    user_id uuid NOT NULL,
    token_hash bytea NOT NULL UNIQUE,
    created_by uuid REFERENCES public.users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    revoked_at timestamptz,
    FOREIGN KEY (user_id, school_id) REFERENCES public.users(id, school_id) ON DELETE CASCADE
);
CREATE INDEX password_reset_tokens_pending
    ON public.password_reset_tokens (user_id) WHERE used_at IS NULL AND revoked_at IS NULL;

ALTER TABLE public.password_reset_tokens ENABLE ROW LEVEL SECURITY;
REVOKE ALL PRIVILEGES ON TABLE public.password_reset_tokens FROM PUBLIC, anon, authenticated;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION '00014_curriculum_scope_password_resets is intentionally irreversible because it assigns curriculum ownership';
END
$$;
-- +goose StatementEnd
