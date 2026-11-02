-- +goose Up
-- A practice sitting done offline arrives later under an ID the device chose, so
-- retrying an upload after a dropped connection cannot record it twice.
ALTER TABLE public.prep_sessions
    ADD COLUMN client_id uuid,
    ADD COLUMN offline boolean NOT NULL DEFAULT false;
CREATE UNIQUE INDEX prep_sessions_client ON public.prep_sessions (student_id, client_id) WHERE client_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS public.prep_sessions_client;
ALTER TABLE public.prep_sessions
    DROP COLUMN IF EXISTS offline,
    DROP COLUMN IF EXISTS client_id;
