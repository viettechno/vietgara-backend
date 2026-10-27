CREATE TABLE public.staff_invitations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    user_id uuid NOT NULL,
    group_ids uuid[] DEFAULT '{}'::uuid[] NOT NULL,
    status public.invitation_status DEFAULT 'pending'::public.invitation_status NOT NULL,
    invited_by uuid NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    responded_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_by uuid
);

ALTER TABLE ONLY public.staff_invitations FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.staff_invitations
    ADD CONSTRAINT staff_invitations_pkey PRIMARY KEY (id);

CREATE INDEX idx_staff_invitations_garage_id ON public.staff_invitations USING btree (garage_id, created_at DESC);

CREATE UNIQUE INDEX idx_staff_invitations_pending_unique ON public.staff_invitations USING btree (garage_id, user_id) WHERE (status = 'pending'::public.invitation_status);

CREATE INDEX idx_staff_invitations_user_id ON public.staff_invitations USING btree (user_id, created_at DESC);

ALTER TABLE ONLY public.staff_invitations
    ADD CONSTRAINT staff_invitations_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.staff_invitations
    ADD CONSTRAINT staff_invitations_invited_by_fkey FOREIGN KEY (invited_by) REFERENCES public.users(id);

ALTER TABLE ONLY public.staff_invitations
    ADD CONSTRAINT staff_invitations_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_staff_invitations_row_audit BEFORE INSERT OR UPDATE ON public.staff_invitations FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.staff_invitations ENABLE ROW LEVEL SECURITY;

CREATE POLICY staff_invitations_delete ON public.staff_invitations FOR DELETE USING ((garage_id = public.app_current_garage_id()));

CREATE POLICY staff_invitations_insert ON public.staff_invitations FOR INSERT WITH CHECK ((garage_id = public.app_current_garage_id()));

CREATE POLICY staff_invitations_select ON public.staff_invitations FOR SELECT USING (((garage_id = public.app_current_garage_id()) OR (user_id = public.app_current_user_id())));

CREATE POLICY staff_invitations_update ON public.staff_invitations FOR UPDATE USING (((garage_id = public.app_current_garage_id()) OR (user_id = public.app_current_user_id()))) WITH CHECK (((garage_id = public.app_current_garage_id()) OR (user_id = public.app_current_user_id())));

-- Policies of earlier tables that read this one.

CREATE POLICY garages_select ON public.garages FOR SELECT USING (((id = public.app_current_garage_id()) OR (user_id = public.app_current_user_id()) OR (EXISTS ( SELECT 1
   FROM public.garage_staff s
  WHERE ((s.garage_id = garages.id) AND (s.user_id = public.app_current_user_id())))) OR (EXISTS ( SELECT 1
   FROM public.staff_invitations i
  WHERE ((i.garage_id = garages.id) AND (i.user_id = public.app_current_user_id()))))));
