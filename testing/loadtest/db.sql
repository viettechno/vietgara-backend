-- Database view of a load-test run: which tables were read by sequential scan
-- (a missing index shows as many rows read per scan on a big table), how many
-- rows the tables hold, and how many connections are open now.
SELECT relname AS table,
       n_live_tup AS rows,
       seq_scan,
       seq_tup_read,
       CASE WHEN seq_scan = 0 THEN 0 ELSE seq_tup_read / seq_scan END AS rows_per_seq_scan,
       coalesce(idx_scan, 0) AS idx_scan
FROM pg_stat_user_tables
WHERE seq_scan > 0
ORDER BY seq_tup_read DESC
LIMIT 12;

SELECT numbackends AS open_connections, xact_commit, xact_rollback, deadlocks
FROM pg_stat_database
WHERE datname = current_database();
