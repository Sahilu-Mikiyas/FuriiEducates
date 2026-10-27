-- +goose Up
CREATE TABLE public.demo_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (char_length(name) BETWEEN 2 AND 120),
    email text NOT NULL CHECK (char_length(email) <= 254),
    school_name text NOT NULL CHECK (char_length(school_name) BETWEEN 2 AND 180),
    role text NOT NULL CHECK (char_length(role) BETWEEN 2 AND 80),
    school_size text NOT NULL CHECK (school_size IN ('1-100', '101-500', '501-1500', '1501+')),
    message text NOT NULL DEFAULT '' CHECK (char_length(message) <= 3000),
    contact_consent boolean NOT NULL CHECK (contact_consent),
    status text NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'contacted', 'closed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX demo_requests_created_at_idx ON public.demo_requests (created_at DESC);
CREATE INDEX demo_requests_status_created_at_idx ON public.demo_requests (status, created_at DESC);
CREATE INDEX demo_requests_email_created_at_idx ON public.demo_requests (lower(email), created_at DESC);

ALTER TABLE public.demo_requests ENABLE ROW LEVEL SECURITY;
REVOKE ALL PRIVILEGES ON TABLE public.demo_requests FROM PUBLIC, anon, authenticated;

-- +goose Down
-- Preserve prospect contact records; this migration is intentionally irreversible.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION '00011_demo_requests is intentionally irreversible';
END
$$;
-- +goose StatementEnd
