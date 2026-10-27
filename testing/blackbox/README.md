# Blackbox tests

API-level tests of the deployed backend. They call the public REST API
(`/api/v1`, through Kong) as the web and mobile apps do, and check
workflows across modules. They never call Go code from the backend.

```bash
make blackbox-stubbed          # build + start the stubbed stack, run the suite, tear down
make blackbox-stubbed-up       # or keep the stack running while you iterate:
make blackbox-test             #   run the suite against it (as often as you like)
make blackbox-stubbed-down     #   and stop it
make blackbox-staging          # run the suite against staging (see Targets)
```

## How it differs from the other tests

| Suite | Lives in | Sees | Talks to |
| --- | --- | --- | --- |
| Unit | next to the code | one package, with mocks/fakes | nothing |
| Integration (`//go:build integration`) | next to the code | repositories and migrations | a disposable Postgres (`internal/platform/db/pgtest`) |
| **Blackbox** | `testing/blackbox` | **the public API contract only** | a running deployment |
| E2E ([`vietgara-e2e`](https://github.com/viettechno/vietgara-e2e)) | separate repo | the web UI | a running deployment |

`testing/blackbox` is its **own Go module**. Go does not let another module
import `vietgara-backend/internal/...`, so the compiler stops a blackbox test
from depending on backend internals. Requests and responses use the
`vietgara-proto` messages, encoded with `protojson` the way grpc-gateway
decodes them. That makes a contract change show up here as a compile error
or a failing decode.

Blackbox tests check the API and backend workflows: every client (web,
mobile, integrations) goes through the API, so the API, not the UI, is
where its behaviour is tested. E2E checks what only the UI can show. If a
workflow is already tested here, an E2E spec only needs the UI part.

## Targets

The same scenarios run on every target. `BLACKBOX_TARGET` picks one.

### `stubbed` (default)

`compose.yml` builds the backend image from this repository and runs it
behind Kong with the real `kong/kong.yml`. It uses **real infrastructure
that we run ourselves** and **stubs for every third party**:

| Dependency | Production | Stubbed target |
| --- | --- | --- |
| Postgres, Redis, Kafka | Cloud SQL, Memorystore, Kafka | the same software in containers (Postgres matches `docker-compose.yml`) |
| E-mail | Mailjet | Mailpit; the suite reads OTPs from its API |
| Google sign-in | Google | `stubs/google`: publishes a JWKS (`GOOGLE_CERTS_URL`) and mints ID tokens on request |
| File storage | GCS | local disk |

The stack uses its own Compose project name and host ports, so it runs next
to the development `docker-compose.yml`:

| Variable | Default |
| --- | --- |
| `BLACKBOX_API_URL` | `http://localhost:18000` (Kong; `BLACKBOX_KONG_PORT` for Compose) |
| `BLACKBOX_MAILPIT_URL` | `http://localhost:18025` (`BLACKBOX_MAILPIT_PORT`) |
| `BLACKBOX_EMAIL_DOMAIN` | `blackbox.vietgara.test`, the `TEST_USERS_EMAIL_DOMAIN` set in `compose.yml` |
| `BLACKBOX_DATABASE_URL` | `postgres://vietgara:vietgara@localhost:15432/vietgara?sslmode=disable` (`BLACKBOX_PG_PORT` for Compose; stubbed only) |
| `BLACKBOX_GOOGLE_STUB_URL` | `http://localhost:18090` (`BLACKBOX_GOOGLE_STUB_PORT`) |
| `BLACKBOX_GOOGLE_CLIENT_ID` | the `GOOGLE_MOBILE_CLIENT_IDS` set in `compose.yml` |

#### Time-dependent scenarios

Some rules depend on time. Two helpers keep them testable:

| Helper | What it does | Where it runs |
| --- | --- | --- |
| `timeshift` (`BLACKBOX_DATABASE_URL`, default `postgres://vietgara:vietgara@localhost:15432/vietgara`) | The SQL helpers H1 to H4 of the scenarios' README: ends the period or the grace period, brings the end within 7 days, expires an invitation. Setup only; results are read through the API and Mailpit. The stubbed stack publishes Postgres on `127.0.0.1:${BLACKBOX_PG_PORT:-15432}` and runs the expiry job every 5 s (`LICENSE_EXPIRY_JOB_INTERVAL`). | stubbed only (skipped elsewhere) |
| `BLACKBOX_SLOW=1` | Enables scenarios that wait for real time: the 2 minute verification code (E2E-IAM-005) and the 10 minute reset session (E2E-IAM-013). | any target |

### `staging`

```bash
# On the staging VM, `make blackbox-env` (vietgara-infra) prints these:
export BLACKBOX_TARGET=staging
export BLACKBOX_API_URL=https://vietgara-api-staging.viettechno.com
export BLACKBOX_EMAIL_DOMAIN=blackbox.vietgara.test
export BLACKBOX_MAILPIT_URL=https://blackbox:<password>@vietgara-mailpit-staging.viettechno.com
# The platform admin (admin@<email domain>, listed in PLATFORM_ADMIN_EMAILS):
export BLACKBOX_ADMIN_PASSWORD=<the ADMIN_PASSWORD secret>
make blackbox-staging
```

The dataset's admin password is public, so it is used on the stubbed stack
only: on staging a scenario that needs the platform admin fails without
`BLACKBOX_ADMIN_PASSWORD`.

Or run the **Blackbox tests (staging)** workflow in GitHub Actions, which
reads the same values from repository secrets (see
`.github/workflows/blackbox-staging.yml`).

Staging uses the real Mailjet and Google, except for **test users**.
Every user the suite signs up is on `BLACKBOX_EMAIL_DOMAIN`, which must
equal the deployment's `TEST_USERS_EMAIL_DOMAIN`. For that domain only,
the backend sends e-mail to a Mailpit that the suite reads (the "test
inbox"), and it deletes the users and everything they own after 24 h.
The domain is under a reserved TLD (`.test` and similar), so no real
person's mail is ever routed there. Authentication itself is unchanged:
the OTPs are random, and the suite reads them like a user would.

So every scenario runs on staging except Google sign-in, which needs the
Google stub (`env.RequireStubbed`). Without `BLACKBOX_MAILPIT_URL`, the
scenarios that sign up are skipped, and the read-only checks use a
pre-provisioned owner (`BLACKBOX_OWNER_EMAIL`/`BLACKBOX_OWNER_PASSWORD`)
if one is set.

## Roadmap: Phase 4, Quality Foundation

Per the [Release Plan](https://github.com/viettechno/vietgara-docs/blob/master/docs/01-Product/Release_Plan/Release_Plan_Phase4.md) in `vietgara-docs`:

- **Today:** 6 tests (billing 1, IAM 2, smoke 2, tenant 1) against fixtures that create a unique user per run.
- **Release 4.0:** the scenarios also use a shared dataset from the public `vietgara-testdata` repository (YAML plus a Go loader, pinned by tag) with short names (`owner1`, `gara1-1`, `advisor1-1`). The baseline is seeded through the public API and read-mostly; scenarios that need fresh users (sign-up, password reset) and destructive scenarios keep creating their own data with unique suffixes and delete it in teardown. A test-only purge removes the test users, their garages, files and Mailpit messages; it is disabled in production by configuration and only touches the test domain. Staging uses a shared password from a CI secret, never from the repository.
- **Release 4.1:** every P1 and most P2 scenario of the stable modules (IAM, tenant with cross-tenant isolation, license limits, customers, quotes, repair orders, inventory, settlements, reporting, PDFs and Excel, the mobile BFFs, e-mails), at least 80% of API operations (reported against the OpenAPI document), and the 90% unit coverage gate on the money path.
- **From then on** each release adds its blackbox scenarios in its own Definition of Done.

## Fetching the shared dataset

The backend pins the public dataset tag in
[`testdata.version`](./testdata.version). `make blackbox-test` fetches that
exact tag into `.data/vietgara-testdata` and loads its YAML through the
shared Go loader. Run `make testdata` from the repository root to fetch it
without running tests. No deploy key or other credential is needed. The
fetched checkout is ignored by Git and is not modified by the test suite.

## Seeding the local baseline

Start the stubbed stack with `make blackbox-stubbed-up`, then run
`make seed`. The command creates the dataset owners referenced by garages,
applies each owner's dataset plan when set, then creates the garages through
the public API. It verifies new owners through Mailpit and leaves matching
existing records unchanged. A same-name garage with different dataset values
is an error instead of being silently updated. `make blackbox-stubbed` runs
the seed command before the suite.

For safety, the seeder only accepts loopback HTTP API and Mailpit URLs and
requires a reserved test e-mail domain. It currently seeds owners and
garages on the stubbed stack; platform admins, custom plan definitions,
staff, customers, suppliers, and inventory records are not seeded. Staging
seeding is not supported by this command.

## Purging test users

With the stubbed stack running, `make purge` immediately deletes every user
whose e-mail is on `TEST_USERS_EMAIL_DOMAIN`; foreign-key cascades remove
their database records, and the command deletes their Mailpit messages by
exact recipient. The command runs inside the backend container and requires
`--confirm-domain` to exactly match the configured domain. It refuses to run
if the test domain or Mailpit API URL is unset, or if the domain is not under a
reserved testing TLD. Staging can run the same binary from its Compose network
with the backend container's database and Mailpit configuration; no public
purge endpoint is exposed. It also removes uploaded objects under the exact
garage prefixes owned by eligible test users using the backend's `STORAGE_*`
configuration. File cleanup happens before database deletion; if cleanup
fails, the command stops and leaves the test users in place so it can be
retried safely.

## Traceability and API coverage

Every scenario test names the documented scenario it covers in a comment:

```go
// Scenario: E2E-IAM-001.
func TestEmailSignUp(t *testing.T) {
```

A test that covers a scenario only in part says so, and names what is
still missing:

```go
// Scenario: E2E-TEN-022 (partial): invitations, roles and groups are
// later batches.
```

Guard-only tests (the smoke package) carry no scenario tag. Run the check
from the repository root:

```bash
make blackbox-traceability
```

It reports three things against `scenarios/documented-ids.txt` (a snapshot
of the documented IDs with priorities, regenerated from vietgara-docs with
`check_traceability.py --regen`) and the OpenAPI document in
`vietgara-proto`: tests naming an unknown ID (**fails the build**),
documented P1/P2 scenarios without a test (**fails the build**, unless the
scenario is listed in `MANUAL_ONLY` with the reason), and the share of API
operations the suite calls (path templates resolved from the typed client; only methods
invoked from `scenarios/` or `fixture/` count, `seed/` does not).

Every documented P1/P2 scenario has a test or a `MANUAL_ONLY` entry, so a
release that documents a scenario must add its test (the Phase 4 rule).
Coverage below the 80% gate reports as a warning (`--fail-on-coverage`
makes it a gate).

## Layout

```
env/        target selection and endpoints (BLACKBOX_*)
client/     REST client (protojson over the proto messages), Mailpit and google-stub clients
fixture/    actors: Anonymous, Owner (any target), NewVerifiedOwner (needs the test inbox), RequireAPIError
stubs/      third-party stubs, one directory each, with their Dockerfile
scenarios/  one package per area: smoke, iam, tenant, ...
compose.yml the stubbed stack
```

## Writing a scenario

- **Test a workflow through the API.** Assert what a client can observe:
  status codes, error codes (`error.code` in the body), and response
  fields. Do not assert on database rows or logs.
- **Use fresh actors.** `fixture.NewVerifiedOwner` gives each test a new
  user with a unique e-mail, so tests can run in parallel
  (`t.Parallel()`) and can be re-run against the same stack without
  resetting it.
- **Make it run on staging too.** Create users with the `fixture`
  helpers: they sign up on the test domain, which staging purges. Read
  e-mail with `fixture.Mailpit`, which skips the test when the target has
  no test inbox. Call `env.RequireStubbed(t, "<why>")` only when the
  scenario needs a stub that staging lacks (Google).
- **Add endpoints to the typed client** in `client/api.go` when you need
  them. Use the RPC's HTTP mapping from `vietgara-proto`: its `body` and
  `response_body` decide what is sent and decoded.
- **Third-party calls go to a stub.** If a scenario needs a third party,
  add a stub under `stubs/`, make the backend's URL for that service
  configurable, and point it at the stub in `compose.yml`.
