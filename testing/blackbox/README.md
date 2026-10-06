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
| `BLACKBOX_GOOGLE_STUB_URL` | `http://localhost:18090` (`BLACKBOX_GOOGLE_STUB_PORT`) |
| `BLACKBOX_GOOGLE_CLIENT_ID` | the `GOOGLE_MOBILE_CLIENT_IDS` set in `compose.yml` |

### `staging`

```bash
BLACKBOX_API_URL=https://<staging host> \
BLACKBOX_OWNER_EMAIL=... BLACKBOX_OWNER_PASSWORD=... \
make blackbox-staging
```

Staging uses the real Mailjet and Google, so the suite cannot read OTPs or
mint ID tokens there. It also must not leave throwaway accounts behind.
Scenarios that need any of these call `env.RequireStubbed` and are
**skipped** on staging. What runs there now:

- checks that need no account (anonymous calls are rejected);
- read-only checks as a pre-provisioned, verified owner
  (`BLACKBOX_OWNER_EMAIL`/`PASSWORD`), skipped if those are not set.

Running workflows on staging needs decisions that have not been made yet:
how a test signs up (a test OTP inbox or a staging-only bypass), and how
test data is tagged and cleaned up. Until then, the staging target is a
smoke test.

## Layout

```
env/        target selection and endpoints (BLACKBOX_*)
client/     REST client (protojson over the proto messages), Mailpit and google-stub clients
fixture/    actors: Anonymous, Owner (any target), NewVerifiedOwner (stubbed), RequireAPIError
stubs/      third-party stubs, one directory each, with their Dockerfile
scenarios/  one package per area: smoke, iam, tenant, ...
compose.yml the stubbed stack
```

## Writing a scenario

- **Test a workflow through the API.** Assert what a client can observe:
  status codes, error codes (`error.code` in the body), and response
  fields. Do not assert on database rows or logs.
- **Use fresh actors.** `fixture.NewVerifiedOwner` gives each test a new
  account with a unique e-mail, so tests can run in parallel
  (`t.Parallel()`) and can be re-run against the same stack without
  resetting it.
- **Say which targets it runs on.** If it creates data, reads e-mail or
  uses a stub, call `env.RequireStubbed(t, "<why>")` first. If it is
  read-only, use `fixture.Owner(t)` so it also runs on staging.
- **Add endpoints to the typed client** in `client/api.go` when you need
  them. Use the RPC's HTTP mapping from `vietgara-proto`: its `body` and
  `response_body` decide what is sent and decoded.
- **Third-party calls go to a stub.** If a scenario needs a third party,
  add a stub under `stubs/`, make the backend's URL for that service
  configurable, and point it at the stub in `compose.yml`.
