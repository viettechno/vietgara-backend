DROP TRIGGER IF EXISTS trigger_user_mfa_recovery_codes_row_audit ON user_mfa_recovery_codes;
DROP TABLE IF EXISTS user_mfa_recovery_codes;

DROP TRIGGER IF EXISTS trigger_user_mfa_row_audit ON user_mfa;
DROP TABLE IF EXISTS user_mfa;
