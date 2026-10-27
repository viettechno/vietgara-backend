-- Event latency of a load-test run (NFR-PF-05), measured in the database after
-- the run: for every consumed event, how long from the moment the relay
-- published it (outbox_events.published_at) and from the moment the business
-- change wrote it (outbox_events.created_at) until the consumer finished
-- (processed_events.processed_at). Run with: psql -v since=<ISO time> -f events.sql
SELECT oe.event_type,
       pe.consumer,
       count(*) AS events,
       round(1000 * extract(epoch FROM percentile_cont(0.5)  WITHIN GROUP (ORDER BY pe.processed_at - oe.published_at))::numeric) AS published_to_done_p50_ms,
       round(1000 * extract(epoch FROM percentile_cont(0.95) WITHIN GROUP (ORDER BY pe.processed_at - oe.published_at))::numeric) AS published_to_done_p95_ms,
       round(1000 * extract(epoch FROM max(pe.processed_at - oe.published_at))::numeric)                                       AS published_to_done_max_ms,
       round(1000 * extract(epoch FROM percentile_cont(0.95) WITHIN GROUP (ORDER BY pe.processed_at - oe.created_at))::numeric)   AS created_to_done_p95_ms,
       count(*) FILTER (WHERE oe.status <> 'published') AS not_published
FROM outbox_events oe
JOIN processed_events pe ON pe.event_id = oe.id
WHERE oe.created_at >= :'since'::timestamptz
GROUP BY oe.event_type, pe.consumer
ORDER BY oe.event_type, pe.consumer;

-- Events still waiting or parked (the outbox backlog after the run).
SELECT status, count(*) AS events, min(created_at) AS oldest
FROM outbox_events
WHERE created_at >= :'since'::timestamptz AND status <> 'published'
GROUP BY status;
