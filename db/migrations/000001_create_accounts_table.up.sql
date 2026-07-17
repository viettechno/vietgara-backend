-- Create enum type for account status
CREATE TYPE account_status AS ENUM ('active', 'inactive', 'suspended');

-- Create accounts table
CREATE TABLE accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) UNIQUE,
    phone VARCHAR(20) UNIQUE,
    auth_provider VARCHAR(20) NOT NULL,
    identity_platform_uid VARCHAR(128) UNIQUE NOT NULL,
    full_name VARCHAR(255) NOT NULL,
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
