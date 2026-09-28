-- platform_admins: operators who manage subscription plans (FR-LIC-05,
-- FR-IAM-08). Synchronized at startup from PLATFORM_ADMIN_EMAILS.
CREATE TABLE platform_admins (
    account_id UUID PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
