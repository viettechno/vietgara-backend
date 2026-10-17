-- Optional TOTP second factor (FR-IAM-09, NFR-SE-04). A user_mfa row exists
-- from the start of the enrolment; the factor counts only once enabled_at
-- is set by confirming a code. The secret is encrypted by the application
-- (AES-GCM) and never stored in clear.
CREATE TABLE user_mfa (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    secret_encrypted BYTEA NOT NULL,
    enabled_at TIMESTAMP WITH TIME ZONE,
    -- The newest TOTP step accepted, so a code cannot be replayed.
    last_used_step BIGINT NOT NULL DEFAULT 0,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- One-time recovery codes; only a hash is kept, used_at marks a spent code.
CREATE TABLE user_mfa_recovery_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    used_at TIMESTAMP WITH TIME ZONE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT user_mfa_recovery_codes_unique UNIQUE (user_id, code_hash)
);

CREATE TRIGGER trigger_user_mfa_row_audit BEFORE INSERT OR UPDATE ON user_mfa
    FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_user_mfa_recovery_codes_row_audit BEFORE INSERT OR UPDATE ON user_mfa_recovery_codes
    FOR EACH ROW EXECUTE FUNCTION set_row_audit();
