-- +goose Up
-- Office roles beside the administrator: a registrar keeps student records,
-- a finance officer keeps the ledger. A person can hold several roles.
ALTER TABLE public.user_roles DROP CONSTRAINT IF EXISTS user_roles_role_check;
ALTER TABLE public.user_roles ADD CONSTRAINT user_roles_role_check
    CHECK (role IN ('school_admin','teacher','student','parent','registrar','finance_officer'));
ALTER TABLE public.staff_invitations DROP CONSTRAINT IF EXISTS staff_invitations_role_check;
ALTER TABLE public.staff_invitations ADD CONSTRAINT staff_invitations_role_check
    CHECK (role IN ('teacher','school_admin','registrar','finance_officer'));

-- A class's homeroom teacher writes its report card comments and can message
-- its families.
ALTER TABLE public.classes ADD COLUMN homeroom_teacher_id uuid;
ALTER TABLE public.classes ADD CONSTRAINT classes_homeroom_teacher_fk
    FOREIGN KEY (homeroom_teacher_id, school_id) REFERENCES public.users(id, school_id) ON DELETE SET NULL (homeroom_teacher_id);

-- +goose Down
ALTER TABLE public.classes DROP CONSTRAINT IF EXISTS classes_homeroom_teacher_fk;
ALTER TABLE public.classes DROP COLUMN IF EXISTS homeroom_teacher_id;
DELETE FROM public.user_roles WHERE role IN ('registrar','finance_officer');
DELETE FROM public.staff_invitations WHERE role IN ('registrar','finance_officer');
ALTER TABLE public.staff_invitations DROP CONSTRAINT IF EXISTS staff_invitations_role_check;
ALTER TABLE public.staff_invitations ADD CONSTRAINT staff_invitations_role_check CHECK (role IN ('teacher','school_admin'));
ALTER TABLE public.user_roles DROP CONSTRAINT IF EXISTS user_roles_role_check;
ALTER TABLE public.user_roles ADD CONSTRAINT user_roles_role_check CHECK (role IN ('school_admin','teacher','student','parent'));
