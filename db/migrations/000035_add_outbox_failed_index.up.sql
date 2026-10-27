-- Observability (Release 9.1, NFR-OB-02): the metrics count events parked as
-- 'failed' on every scrape. Only the pending rows had an index, so the count
-- would scan the whole table, which grows with every published event.
CREATE INDEX idx_outbox_events_failed ON outbox_events (created_at) WHERE status = 'failed';
-- A plain CREATE INDEX holds a SHARE lock on outbox_events while it builds,
-- which blocks writes to the outbox for the build. It is quick while the table
-- is small (before launch); rebuild it with CONCURRENTLY by hand on a large one.
