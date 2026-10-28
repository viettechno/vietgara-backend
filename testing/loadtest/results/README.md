# Load test results

One directory per run, `<stamp>-<target>`: `run.txt` (date, versions, host), `summary.md` and `summary.json` (k6), and for stubbed runs `database.txt` (event latency and table scans read from the database). Add a row here when a run is committed.

| Run | Target | Load | Reads P95 | Writes P95 | Events P95 (API / database) | Failed flows | Verdict |
| --- | --- | --- | --- | --- | --- | --- | --- |
| [2026-10-27-stubbed](2026-10-27-stubbed/summary.md) | stubbed stack, one Mac (10 cores), relay 1 s | 50 garages, 50 flow + 25 browse users, 10 min | 10 ms | 12 ms | 1.04 s / 25 ms | 0 | Targets met |
| [2026-10-27-headroom-stubbed](2026-10-27-headroom-stubbed/summary.md) | same | 3x the users, 5 min (about 73 documents and payments a second) | 11 ms | 15 ms | 1.06 s / 63 ms | 0 | Targets met |
| [2026-10-27-staging](2026-10-27-staging/summary.md) | staging VM in Vietnam, relay 1 s; k6 on a GitHub-hosted runner | 50 garages, 50 flow + 25 browse users, 10 min | 194 ms | 196 ms | 1.08 s / not recorded | 0 | Targets met (P95). Reads P99 369 ms |

## Reading these numbers

- **Stubbed runs:** backend, database, Kafka, gateway and k6 shared one machine, so the figures show that the code path and queries have headroom, **not** how the staging VM performs.
- **Staging run** (Actions run 40514630466, backend `20a8655`): k6 ran on a GitHub-hosted runner outside Vietnam, so every request includes the network round trip to the VM; the median of about 190 ms for reads and writes alike is mostly that, not server time. The database figure (`events.sql` on the VM, relay-published to consumer done) was not taken for this run.
- The event figure through the API is dominated by the outbox relay's 1 s polling (staging's setting); the database figure (relay-published to consumer done) is the consumer's own time.
- The runs used backend `v1.16.0-4-g5f659ea` and testdata `v0.4.0`; later commits on the branch changed only CI and error text.
- Tables hold a few thousand rows at the end of a run; sequential scans on them are the planner's choice and say nothing about large tables.
