-- Observability (Release 9.1, NFR-OB-02): the metrics count events parked as
-- 'failed' on every scrape. Only the pending rows had an index, so the count
-- would scan the whole table, which grows with every published event.
CREATE INDEX idx_outbox_events_failed ON outbox_events (created_at) WHERE status = 'failed';
