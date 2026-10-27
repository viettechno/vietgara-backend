-- operating_hours: [{"day": 1..7 (Monday..Sunday), "closed", "open", "close"}].
-- The numbering templates use {YYYY}, {YY}, {MM}, {DD} and one {SEQ:n}.

CREATE TYPE public.garage_status AS ENUM (
    'active',
    'inactive'
);

CREATE TYPE public.stock_issue_mode AS ENUM (
    'on_close',
    'on_start'
);

CREATE TABLE public.garages (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    name character varying(255) NOT NULL,
    tax_code character varying(20),
    legal_name character varying(255),
    legal_address text,
    representative character varying(255),
    bank_details jsonb DEFAULT '{}'::jsonb NOT NULL,
    address text,
    phone character varying(20),
    email character varying(255),
    logo_key character varying(500),
    timezone character varying(64) DEFAULT 'Asia/Ho_Chi_Minh'::character varying NOT NULL,
    currency character varying(3) DEFAULT 'VND'::character varying NOT NULL,
    status public.garage_status DEFAULT 'active'::public.garage_status NOT NULL,
    operating_hours jsonb DEFAULT '[{"day": 1, "open": "08:00", "close": "17:30", "closed": false}, {"day": 2, "open": "08:00", "close": "17:30", "closed": false}, {"day": 3, "open": "08:00", "close": "17:30", "closed": false}, {"day": 4, "open": "08:00", "close": "17:30", "closed": false}, {"day": 5, "open": "08:00", "close": "17:30", "closed": false}, {"day": 6, "open": "08:00", "close": "12:00", "closed": false}, {"day": 7, "open": "", "close": "", "closed": true}]'::jsonb NOT NULL,
    quote_number_template character varying(50) DEFAULT 'BG-{YYYY}{MM}-{SEQ:4}'::character varying NOT NULL,
    repair_order_number_template character varying(50) DEFAULT 'LSC-{YYYY}{MM}-{SEQ:4}'::character varying NOT NULL,
    settlement_number_template character varying(50) DEFAULT 'QT-{YYYY}{MM}-{SEQ:4}'::character varying NOT NULL,
    payment_voucher_number_template character varying(50) DEFAULT 'PT-{YYYY}{MM}-{SEQ:4}'::character varying NOT NULL,
    goods_receipt_number_template character varying(50) DEFAULT 'PN-{YYYY}{MM}-{SEQ:4}'::character varying NOT NULL,
    stock_issue_number_template character varying(50) DEFAULT 'PX-{YYYY}{MM}-{SEQ:4}'::character varying NOT NULL,
    stock_count_number_template character varying(50) DEFAULT 'KK-{YYYY}{MM}-{SEQ:4}'::character varying NOT NULL,
    write_off_number_template character varying(50) DEFAULT 'XH-{YYYY}{MM}-{SEQ:4}'::character varying NOT NULL,
    supplier_return_number_template character varying(50) DEFAULT 'TN-{YYYY}{MM}-{SEQ:4}'::character varying NOT NULL,
    stock_issue_mode public.stock_issue_mode DEFAULT 'on_close'::public.stock_issue_mode NOT NULL,
    default_quote_approval_mode public.quote_approval_mode DEFAULT 'link'::public.quote_approval_mode NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.garages FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.garages
    ADD CONSTRAINT garages_pkey PRIMARY KEY (id);

CREATE INDEX idx_garages_status ON public.garages USING btree (status);

CREATE INDEX idx_garages_user_id ON public.garages USING btree (user_id);

ALTER TABLE ONLY public.garages
    ADD CONSTRAINT garages_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_garages_row_audit BEFORE INSERT OR UPDATE ON public.garages FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.garages ENABLE ROW LEVEL SECURITY;

CREATE POLICY garages_delete ON public.garages FOR DELETE USING ((id = public.app_current_garage_id()));

CREATE POLICY garages_insert ON public.garages FOR INSERT WITH CHECK (((id = public.app_current_garage_id()) AND (user_id = public.app_current_user_id())));

CREATE POLICY garages_select_directory ON public.garages FOR SELECT USING ((current_setting('app.garage_directory'::text, true) = 'on'::text));

CREATE POLICY garages_update ON public.garages FOR UPDATE USING ((id = public.app_current_garage_id())) WITH CHECK ((id = public.app_current_garage_id()));
