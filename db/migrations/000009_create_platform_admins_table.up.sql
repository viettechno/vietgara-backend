-- platform_admins: operators who manage subscription plans (FR-LIC-05,
-- FR-IAM-08). Synchronized at startup from PLATFORM_ADMIN_EMAILS.
CREATE TABLE platform_admins (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
