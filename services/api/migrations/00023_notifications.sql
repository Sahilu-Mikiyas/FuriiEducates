-- +goose Up
-- What a person sees in the app. The words are written when the list is read,
-- in the reader's language, from the kind and its details.
CREATE TABLE public.notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    user_id uuid NOT NULL,
    topic text NOT NULL CHECK (topic IN ('attendance','reports','schoolwork','announcements','account')),
    kind text NOT NULL CHECK (length(kind) BETWEEN 1 AND 60),
    params jsonb NOT NULL DEFAULT '{}'::jsonb,
    link text NOT NULL DEFAULT '' CHECK (length(link) <= 300),
    dedupe_key text CHECK (length(dedupe_key) <= 200),
    announcement_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    read_at timestamptz,
    FOREIGN KEY (user_id, school_id) REFERENCES public.users(id, school_id) ON DELETE CASCADE
);
CREATE INDEX notifications_user ON public.notifications (user_id, created_at DESC);
CREATE INDEX notifications_unread ON public.notifications (user_id) WHERE read_at IS NULL;
CREATE UNIQUE INDEX notifications_dedupe ON public.notifications (user_id, dedupe_key) WHERE dedupe_key IS NOT NULL;

-- Messages leaving the system by email, SMS or Telegram. A guardian without an
-- account is reached by phone through guardian_id. When a channel is not set up
-- the row is kept as skipped, so the school can see what would have gone out.
CREATE TABLE public.notification_deliveries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    notification_id uuid REFERENCES public.notifications(id) ON DELETE SET NULL,
    user_id uuid,
    guardian_id uuid REFERENCES public.guardians(id) ON DELETE SET NULL,
    announcement_id uuid,
    topic text NOT NULL CHECK (topic IN ('attendance','reports','schoolwork','announcements','account')),
    kind text NOT NULL,
    channel text NOT NULL CHECK (channel IN ('email','sms','telegram')),
    address text NOT NULL CHECK (length(address) BETWEEN 1 AND 320),
    subject text NOT NULL DEFAULT '',
    body text NOT NULL,
    sensitive boolean NOT NULL DEFAULT false,
    dedupe_key text,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sending','sent','failed','cancelled','skipped')),
    attempts integer NOT NULL DEFAULT 0,
    send_after timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL DEFAULT now() + interval '1 day',
    claimed_at timestamptz,
    last_error text NOT NULL DEFAULT '',
    provider_ref text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz,
    FOREIGN KEY (user_id, school_id) REFERENCES public.users(id, school_id) ON DELETE CASCADE
);
CREATE INDEX notification_deliveries_due ON public.notification_deliveries (send_after) WHERE status = 'pending';
CREATE INDEX notification_deliveries_school ON public.notification_deliveries (school_id, created_at DESC);
CREATE INDEX notification_deliveries_announcement ON public.notification_deliveries (announcement_id) WHERE announcement_id IS NOT NULL;
CREATE UNIQUE INDEX notification_deliveries_dedupe ON public.notification_deliveries (school_id, channel, address, dedupe_key) WHERE dedupe_key IS NOT NULL;

-- Each person's choices: the language of messages, which channels reach them,
-- topics they do not want outside the app, and their linked Telegram chat.
CREATE TABLE public.notification_settings (
    user_id uuid PRIMARY KEY,
    school_id uuid NOT NULL,
    language text CHECK (language IN ('en','am')),
    email_enabled boolean NOT NULL DEFAULT true,
    sms_enabled boolean NOT NULL DEFAULT true,
    telegram_enabled boolean NOT NULL DEFAULT true,
    muted_topics text[] NOT NULL DEFAULT '{}',
    telegram_chat_id text,
    telegram_link_hash bytea UNIQUE,
    telegram_link_expires_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (user_id, school_id) REFERENCES public.users(id, school_id) ON DELETE CASCADE
);

-- The school decides which automatic messages may use each paid or outside
-- channel. The app inbox always gets everything.
CREATE TABLE public.school_notification_rules (
    school_id uuid PRIMARY KEY REFERENCES public.schools(id) ON DELETE CASCADE,
    email_topics text[] NOT NULL DEFAULT '{attendance,reports,schoolwork,announcements,account}',
    sms_topics text[] NOT NULL DEFAULT '{attendance,reports}',
    telegram_topics text[] NOT NULL DEFAULT '{attendance,reports,schoolwork,announcements}',
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE public.announcements (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id uuid NOT NULL REFERENCES public.schools(id) ON DELETE CASCADE,
    title text NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 120),
    body text NOT NULL CHECK (length(trim(body)) BETWEEN 1 AND 2000),
    audience text NOT NULL CHECK (audience IN ('everyone','staff','parents','students','class')),
    class_id uuid,
    channels text[] NOT NULL DEFAULT '{}',
    recipients integer NOT NULL DEFAULT 0,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (class_id, school_id) REFERENCES public.classes(id, school_id) ON DELETE SET NULL (class_id),
    FOREIGN KEY (created_by, school_id) REFERENCES public.users(id, school_id)
);
CREATE INDEX announcements_school ON public.announcements (school_id, created_at DESC);

-- Where reading Telegram's updates last stopped. One row for the whole service.
CREATE TABLE public.telegram_cursor (
    id integer PRIMARY KEY CHECK (id = 1),
    next_offset bigint NOT NULL DEFAULT 0
);

ALTER TABLE public.notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.notification_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.notification_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.school_notification_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.announcements ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.telegram_cursor ENABLE ROW LEVEL SECURITY;
REVOKE ALL PRIVILEGES ON TABLE public.notifications, public.notification_deliveries, public.notification_settings,
    public.school_notification_rules, public.announcements, public.telegram_cursor FROM PUBLIC, anon, authenticated;

-- +goose Down
DROP TABLE IF EXISTS public.telegram_cursor, public.announcements, public.school_notification_rules,
    public.notification_settings, public.notification_deliveries, public.notifications;
