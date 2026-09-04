-- Create enum type for account status
CREATE TYPE account_status AS ENUM ('active', 'inactive', 'suspended');

-- Create enum type for auth provider
-- Values follow Database Design Section 2.1 ('email' | 'google' |
-- 'facebook'); phone-only registration still records 'email' so the
-- account can upgrade to a password later.
CREATE TYPE account_auth_provider AS ENUM ('email', 'phone', 'google', 'facebook');

-- Create accounts table
-- password_hash is only set for accounts registered via email/password
-- (bcrypt, never plaintext) -- see internal/identity/usecase/register.go.
-- identity_platform_uid is the GCP Identity Platform UID and only exists
-- for social-login accounts, so it must be nullable (Database Design 2.1).
CREATE TABLE accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) UNIQUE,
    phone VARCHAR(20) UNIQUE,
    auth_provider account_auth_provider NOT NULL DEFAULT 'email',
    identity_platform_uid VARCHAR(128) UNIQUE,
    full_name VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255),
    status account_status NOT NULL DEFAULT 'active',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Create indexes for better query performance
CREATE INDEX idx_accounts_email ON accounts (email);
CREATE INDEX idx_accounts_phone ON accounts (phone);
CREATE INDEX idx_accounts_identity_platform_uid ON accounts (identity_platform_uid);
CREATE INDEX idx_accounts_auth_provider ON accounts (auth_provider);

-- Create trigger function to automatically update updated_at
CREATE OR REPLACE FUNCTION update_accounts_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger
CREATE TRIGGER trigger_accounts_updated_at
BEFORE UPDATE ON accounts
FOR EACH ROW
EXECUTE FUNCTION update_accounts_updated_at();
