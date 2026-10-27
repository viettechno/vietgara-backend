-- password_hash is only set for e-mail users (bcrypt, never plaintext).
-- identity_platform_uid is the Google subject id of Google users.
-- phone is an optional profile field used to find staff (FR-TEN-04).
-- email_verified_at stays NULL until the OTP is confirmed; Google users
-- are verified on sign-up.

CREATE TYPE public.user_auth_provider AS ENUM (
    'email',
    'google'
);

CREATE TYPE public.user_locale AS ENUM (
    'vi',
    'en'
);

CREATE TYPE public.user_name_source AS ENUM (
    'google',
    'user'
);

CREATE TYPE public.user_status AS ENUM (
    'active',
    'suspended'
);

CREATE TABLE public.users (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email character varying(255) NOT NULL,
    phone character varying(20),
    auth_provider public.user_auth_provider DEFAULT 'email'::public.user_auth_provider NOT NULL,
    identity_platform_uid character varying(128),
    full_name character varying(255) NOT NULL,
    full_name_source public.user_name_source DEFAULT 'user'::public.user_name_source NOT NULL,
    password_hash character varying(255),
    status public.user_status DEFAULT 'active'::public.user_status NOT NULL,
    email_verified_at timestamp with time zone,
    locale public.user_locale DEFAULT 'vi'::public.user_locale NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_by uuid
);

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_email_key UNIQUE (email);

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_identity_platform_uid_key UNIQUE (identity_platform_uid);

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_phone_key UNIQUE (phone);

CREATE UNIQUE INDEX idx_users_email_lower_unique ON public.users USING btree (lower((email)::text));

CREATE INDEX idx_users_identity_platform_uid ON public.users USING btree (identity_platform_uid);

CREATE INDEX idx_users_phone ON public.users USING btree (phone);

CREATE TRIGGER trigger_users_row_audit BEFORE INSERT OR UPDATE ON public.users FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();
