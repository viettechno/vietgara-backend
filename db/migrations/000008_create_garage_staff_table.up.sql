CREATE TYPE public.staff_status AS ENUM (
    'active',
    'inactive'
);

CREATE TABLE public.garage_staff (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    user_id uuid NOT NULL,
    status public.staff_status DEFAULT 'active'::public.staff_status NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.garage_staff FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.garage_staff
    ADD CONSTRAINT garage_staff_pkey PRIMARY KEY (id);

CREATE INDEX idx_garage_staff_garage_id ON public.garage_staff USING btree (garage_id);

CREATE UNIQUE INDEX idx_garage_staff_garage_user_unique ON public.garage_staff USING btree (garage_id, user_id);

CREATE INDEX idx_garage_staff_status ON public.garage_staff USING btree (status);

CREATE INDEX idx_garage_staff_user_id ON public.garage_staff USING btree (user_id);

ALTER TABLE ONLY public.garage_staff
    ADD CONSTRAINT garage_staff_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.garage_staff
    ADD CONSTRAINT garage_staff_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_garage_staff_row_audit BEFORE INSERT OR UPDATE ON public.garage_staff FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.garage_staff ENABLE ROW LEVEL SECURITY;

CREATE POLICY garage_staff_delete ON public.garage_staff FOR DELETE USING ((garage_id = public.app_current_garage_id()));

CREATE POLICY garage_staff_insert ON public.garage_staff FOR INSERT WITH CHECK ((garage_id = public.app_current_garage_id()));

CREATE POLICY garage_staff_select ON public.garage_staff FOR SELECT USING (((garage_id = public.app_current_garage_id()) OR (user_id = public.app_current_user_id())));

CREATE POLICY garage_staff_update ON public.garage_staff FOR UPDATE USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));
