-- Identity & Access (Database Design 2.1): e-mail + password and Google are
-- the only sign-in methods, the e-mail must be verified by OTP before the
-- user can use garages (FR-IAM-01), and each user carries its
-- interface/e-mail language (NFR-US-01).

CREATE TYPE user_status AS ENUM ('active', 'suspended');
CREATE TYPE user_auth_provider AS ENUM ('email', 'google');
CREATE TYPE user_locale AS ENUM ('vi', 'en');
-- Where a user's display name came from (FR-IAM-12): taken from Google at
-- first sign-in, or typed by the user. A later Google sign-in refreshes a
-- 'google' name but never overwrites one the user typed.
CREATE TYPE user_name_source AS ENUM ('google', 'user');

-- password_hash is only set for e-mail users (bcrypt, never plaintext).
-- identity_platform_uid is the Google subject id of Google users.
-- phone is an optional profile field used to find staff (FR-TEN-04).
-- email_verified_at stays NULL until the OTP is confirmed; Google users
-- are verified on sign-up.
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) NOT NULL UNIQUE,
    phone VARCHAR(20) UNIQUE,
    auth_provider user_auth_provider NOT NULL DEFAULT 'email',
    identity_platform_uid VARCHAR(128) UNIQUE,
    full_name VARCHAR(255) NOT NULL,
    full_name_source user_name_source NOT NULL DEFAULT 'user',
    password_hash VARCHAR(255),
    status user_status NOT NULL DEFAULT 'active',
    email_verified_at TIMESTAMP WITH TIME ZONE,
    locale user_locale NOT NULL DEFAULT 'vi',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID,
    updated_by UUID
);

-- E-mails are unique case-insensitively.
CREATE UNIQUE INDEX idx_users_email_lower_unique ON users (lower(email));
CREATE INDEX idx_users_phone ON users (phone);
CREATE INDEX idx_users_identity_platform_uid ON users (identity_platform_uid);

