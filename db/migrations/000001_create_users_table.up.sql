-- Identity & Access (Database Design 2.1): e-mail + password and Google are
-- the only sign-in methods, the e-mail must be verified by OTP before the
-- user can use garages (FR-IAM-01), and each user carries its
-- interface/e-mail language (NFR-US-01).

CREATE TYPE user_status AS ENUM ('active', 'suspended');
CREATE TYPE user_auth_provider AS ENUM ('email', 'google');
CREATE TYPE user_locale AS ENUM ('vi', 'en');

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
    password_hash VARCHAR(255),
    status user_status NOT NULL DEFAULT 'active',
    email_verified_at TIMESTAMP WITH TIME ZONE,
    locale user_locale NOT NULL DEFAULT 'vi',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- E-mails are unique case-insensitively.
CREATE UNIQUE INDEX idx_users_email_lower_unique ON users (lower(email));
CREATE INDEX idx_users_phone ON users (phone);
CREATE INDEX idx_users_identity_platform_uid ON users (identity_platform_uid);

CREATE OR REPLACE FUNCTION update_users_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_users_updated_at
BEFORE UPDATE ON users
FOR EACH ROW
EXECUTE FUNCTION update_users_updated_at();
