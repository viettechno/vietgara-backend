-- Idempotency keys (API Specification Section 1.1): creation APIs with
-- financial impact -- creating a settlement, recording a payment -- take an
-- Idempotency-Key header. The first request with a key runs and its
-- response is stored here in the same transaction as the change it made,
-- so the two commit or roll back together; a retry with the same key gets
-- that response back instead of creating a second settlement or payment.
-- A concurrent duplicate waits on the primary key until the first request
-- commits, then replays it. A request that fails leaves no row, so it can
-- simply be retried.

CREATE TABLE idempotency_keys (
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    -- Keys are per caller: two staff members can never collide.
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    idempotency_key VARCHAR(255) NOT NULL,
    -- The full gRPC method and a SHA-256 of the request message: the same
    -- key reused for another method or a different request is refused.
    method TEXT NOT NULL,
    request_hash BYTEA NOT NULL,
    -- The stored response: its message type, its encoding and the HTTP
    -- status the handler chose (201 for creations).
    response_type TEXT,
    response BYTEA,
    http_status INTEGER,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (garage_id, account_id, idempotency_key)
);

-- purge_idempotency_keys() scans by age.
CREATE INDEX idx_idempotency_keys_created_at ON idempotency_keys (created_at);

ALTER TABLE idempotency_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE idempotency_keys FORCE ROW LEVEL SECURITY;

CREATE POLICY idempotency_keys_rls_policy ON idempotency_keys
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY idempotency_keys_rls_policy ON idempotency_keys IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- Keys are kept for a day (idempotency.Retention): long enough for any
-- client retry. Deleting expired keys crosses every garage, which the
-- garage-scoped RLS above forbids the application role, so this function
-- runs with its owner's rights and does nothing else.
CREATE OR REPLACE FUNCTION purge_idempotency_keys(older_than TIMESTAMP WITH TIME ZONE)
RETURNS BIGINT
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = public AS $$
    WITH deleted AS (
        DELETE FROM idempotency_keys WHERE created_at < older_than RETURNING 1
    )
    SELECT count(*) FROM deleted
$$;

GRANT EXECUTE ON FUNCTION purge_idempotency_keys(TIMESTAMP WITH TIME ZONE) TO vietgara_app;
