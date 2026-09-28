-- Identity & Access (Database Design 2.1): e-mail + password and Google are
-- the only sign-in methods, the e-mail must be verified by OTP before the
-- account can use garages (FR-IAM-01), and each account carries its
-- interface/e-mail language (NFR-US-01).

CREATE TYPE account_status AS ENUM ('active', 'suspended');
CREATE TYPE account_auth_provider AS ENUM ('email', 'google');
CREATE TYPE account_locale AS ENUM ('vi', 'en');

-- password_hash is only set for e-mail accounts (bcrypt, never plaintext).
-- identity_platform_uid is the Google subject id of Google accounts.
-- phone is an optional profile field used to find staff (FR-TEN-04).
-- email_verified_at stays NULL until the OTP is confirmed; Google accounts
-- are verified on sign-up.
CREATE TABLE accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) NOT NULL UNIQUE,
    phone VARCHAR(20) UNIQUE,
    auth_provider account_auth_provider NOT NULL DEFAULT 'email',
    identity_platform_uid VARCHAR(128) UNIQUE,
    full_name VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255),
    status account_status NOT NULL DEFAULT 'active',
    email_verified_at TIMESTAMP WITH TIME ZONE,
    locale account_locale NOT NULL DEFAULT 'vi',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- E-mails are unique case-insensitively.
CREATE UNIQUE INDEX idx_accounts_email_lower_unique ON accounts (lower(email));
CREATE INDEX idx_accounts_phone ON accounts (phone);
CREATE INDEX idx_accounts_identity_platform_uid ON accounts (identity_platform_uid);

CREATE OR REPLACE FUNCTION update_accounts_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_accounts_updated_at
BEFORE UPDATE ON accounts
FOR EACH ROW
EXECUTE FUNCTION update_accounts_updated_at();
