# Load test (Release 9.0, NFR-PF-06)

A [k6](https://k6.io) script that drives the repair-shop flow of about 50 garages through the gateway and checks the latency targets of the Non-Functional Requirements:

| Requirement | Target | How it is measured |
| --- | --- | --- |
| NFR-PF-01 | read APIs P95 < 300 ms | k6 threshold on requests tagged `kind:read` |
| NFR-PF-02 | write APIs P95 < 800 ms | k6 threshold on requests tagged `kind:write` |
| NFR-PF-05 | event latency P95 < 2 s | two ways, below |
| NFR-PF-06 | 50,000 business transactions a day at 1,000 garages | the arithmetic in [The load](#the-load) |

The thresholds are in the script, so `k6` exits non-zero when a target is missed and the workflow fails.

## Run it

Needs Docker, Go, [k6](https://grafana.com/docs/k6/latest/set-up/install-k6/) (`brew install k6`) and the repository's `make`.

```bash
make loadtest-stubbed          # stubbed stack: start, seed, run (about 13 min), report, stop
make loadtest-staging          # staging: seed, run (see the variables below)
```

`loadtest-stubbed` is the one-command, repeatable run from the dataset: it starts the stack with staging's 1 s outbox relay, creates the population from the pinned `vietgara-testdata` (`testing/blackbox/testdata.version`), runs the script, prints the event latency from the database and stops the stack. The pieces are separate targets (`loadtest-seed`, `loadtest-run`, `loadtest-report`) for iterating on a stack you keep running (`make blackbox-stubbed-up`).

Staging needs `BLACKBOX_API_URL`, `BLACKBOX_MAILPIT_URL`, `BLACKBOX_EMAIL_DOMAIN` (as for `make blackbox-staging`), and the dataset's secrets `TEST_PASSWORD` and `ADMIN_PASSWORD`. The **Load test (staging)** workflow does the same on demand and uploads the results. The seeder registers users on the reserved test domain only, so the normal test-user purge removes the population after 24 hours; seeding again is idempotent.

| Variable | Default | Meaning |
| --- | --- | --- |
| `DURATION` | `10m` | steady state, after a `RAMP` of `1m` up and 30 s down |
| `FLOW_VUS` | one per garage | garages running the full flow at once; more than the garages gives several users per garage |
| `BROWSE_VUS` | half the garages | users looking things up |
| `THINK_FLOW`, `THINK_BROWSE` | `8`, `3` | average seconds between two flows or two lookups (random, 0.5x to 1.5x) |
| `LOADTEST_STAMP` | today | names the results directory, `<stamp>-<target>` |

## The population

`loadseed` (`testing/blackbox/cmd/loadseed`) reads the `load` section of the shared dataset (profile `garages50`): 50 owners `loadowner<n>@<test domain>` on the *Garage chain* plan, one garage each (`QA Load Gara <n>`), and in every garage 10 stocked parts (5,000 each), 5 jobs with a suggested part, and 30 customers with 2 vehicles. It writes `population.json` (ids only, no credentials) for the script. The plan matters: a quote, a repair order and a settlement each count as a transaction against the plan's daily limit, and *Pro* allows 100 a day, *Garage chain* 1,000. One flow is three transactions, so a 10-minute run uses about 250 of an owner's 1,000: **do not run more than three times a day against the same population** (a fresh stubbed stack resets the count).

## The flow

Per garage, one virtual user loops: pick a returning customer (20 % create a walk-in customer and vehicle) → search parts and list jobs → create a quote with a labor and a part line, half of them with a job from the job picker → send it → the customer opens the public link and approves every line → convert to a repair order (the stock reservation is an event) → complete the lines → close it (posting the stock issue is an event) → create the settlement → record the payment → read the revenue report (the dashboard). A second group of users browses: customer and vehicle lookup, part search, stock levels, lists of quotes, repair orders and settlements, the revenue report and the stock valuation.

## The load

At the default settings a run completes about 5 flows a second, about 23 business documents and payments a second together with the browsing, from 50 garages. NFR-PF-06 asks for 50,000 transactions a day at 1,000 garages, 50 per garage; if a quarter of the day's work falls in the busiest hour (an assumption, not a measured figure) that is about 3.5 a second for the whole platform. The default run is therefore several times the peak the requirement describes, concentrated on 50 garages instead of spread over 1,000. It shows that the code path has headroom; it cannot show how the platform behaves with 1,000 garages' worth of data (see [Limits of this test](#limits-of-this-test)).

## Event latency (NFR-PF-05)

- **From the API (every run).** After the commands that publish `repair_order.created` and `repair_order.closed`, the script polls the stock issue until the consumer's result shows and records `event_latency_ms`. It includes the outbox relay's polling interval (staging: 1 s), so it is an upper bound of "publish to consumer completion".
- **From the database (`make loadtest-report`, stubbed).** `events.sql` joins `outbox_events` and `processed_events`: the time from the relay marking the event published to the consumer finishing, per event type, and from the business change writing it. For staging, run the same file with `psql` on the VM.

## Results

Each run writes `results/<stamp>-<target>/`: `run.txt` (versions and host), `summary.md` and `summary.json` (k6), and for stubbed runs `database.txt`. They are kept in the repository; [results/README.md](results/README.md) lists them with the conclusions.

## Limits of this test

- The stubbed run puts the backend, Postgres, Kafka, Kong and k6 on one machine; it shows regressions and bottlenecks in the code, not what the staging VM will do. The Definition of Done is a run against staging; the **Load test (staging)** workflow exists for it.
- The data is small (about 1,500 customers, 3,000 vehicles and a few thousand documents at the end of a run). Index problems that appear only with millions of rows are not found; `database.txt` lists the sequential scans to read with that in mind.
- All traffic comes from one address; staging's gateway limits (30 times the production profile) leave room for it, production's would not. Public quote approvals are limited per address (staging: 900 a minute), the first limit a bigger run would meet.
