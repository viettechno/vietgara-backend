-- The same shape as staff_invitations without a garage: only existing users
-- can be invited, the invitee is e-mailed and joins the group on acceptance.
-- One pending invitation per group and user; they expire after 7 days.

CREATE TABLE public.platform_staff_invitations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    group_id uuid NOT NULL,
    user_id uuid NOT NULL,
    status public.invitation_status DEFAULT 'pending'::public.invitation_status NOT NULL,
    invited_by uuid NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    responded_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_by uuid
);

ALTER TABLE ONLY public.platform_staff_invitations
    ADD CONSTRAINT platform_staff_invitations_pkey PRIMARY KEY (id);

CREATE INDEX idx_platform_staff_invitations_group_id ON public.platform_staff_invitations USING btree (group_id, created_at DESC);

CREATE UNIQUE INDEX idx_platform_staff_invitations_pending_unique ON public.platform_staff_invitations USING btree (group_id, user_id) WHERE (status = 'pending'::public.invitation_status);

CREATE INDEX idx_platform_staff_invitations_user_id ON public.platform_staff_invitations USING btree (user_id, created_at DESC);

ALTER TABLE ONLY public.platform_staff_invitations
    ADD CONSTRAINT platform_staff_invitations_group_id_fkey FOREIGN KEY (group_id) REFERENCES public.platform_staff_groups(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.platform_staff_invitations
    ADD CONSTRAINT platform_staff_invitations_invited_by_fkey FOREIGN KEY (invited_by) REFERENCES public.users(id);

ALTER TABLE ONLY public.platform_staff_invitations
    ADD CONSTRAINT platform_staff_invitations_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_platform_staff_invitations_row_audit BEFORE INSERT OR UPDATE ON public.platform_staff_invitations FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();
