CREATE TYPE public.permission AS ENUM (
    'garage.write',
    'staff.read',
    'staff.write',
    'customer.read',
    'customer.write',
    'customer.correct',
    'supplier.read',
    'supplier.write',
    'subscription.read',
    'quote.read',
    'quote.write',
    'repair_order.read',
    'repair_order.write',
    'inventory.read',
    'inventory.write',
    'inventory.cost.read',
    'inventory.adjust',
    'inventory.count',
    'catalogue.read',
    'catalogue.write',
    'billing.read',
    'billing.write',
    'report.read'
);

CREATE TABLE public.role_permissions (
    role_id uuid NOT NULL,
    permission public.permission NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid
);

ALTER TABLE ONLY public.role_permissions FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.role_permissions
    ADD CONSTRAINT role_permissions_pkey PRIMARY KEY (role_id, permission);

ALTER TABLE ONLY public.role_permissions
    ADD CONSTRAINT role_permissions_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_role_permissions_created_audit BEFORE INSERT ON public.role_permissions FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();

ALTER TABLE public.role_permissions ENABLE ROW LEVEL SECURITY;

CREATE POLICY role_permissions_select ON public.role_permissions FOR SELECT USING ((EXISTS ( SELECT 1
   FROM public.roles r
  WHERE (r.id = role_permissions.role_id))));

CREATE POLICY role_permissions_write ON public.role_permissions USING ((EXISTS ( SELECT 1
   FROM public.roles r
  WHERE ((r.id = role_permissions.role_id) AND (r.owner_user_id = public.app_current_user_id()))))) WITH CHECK ((EXISTS ( SELECT 1
   FROM public.roles r
  WHERE ((r.id = role_permissions.role_id) AND (r.owner_user_id = public.app_current_user_id())))));
