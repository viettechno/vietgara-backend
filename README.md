# VietGara Backend

The Go modular monolith behind the VietGara front ends: the garage web admin
(`vietgara-owner-web`), the platform admin (`vietgara-admin-web`) and the
mobile apps (Release 1.0–1.5). One
process serves:

- **gRPC** on `GRPC_PORT`, using the contracts in `vietgara-proto`.
- The **REST/JSON API** on `HTTP_PORT` under `/api/v1`. grpc-gateway builds
  it from the same contracts and serves it in-process, so every REST call
  passes through the same interceptors as a gRPC call. With
  `API_DOCS_ENABLED`, a [Scalar](https://scalar.com) API reference to read and
  try it is served at `/api/docs` (`http://localhost:8000/api/docs` in Compose).

```mermaid
flowchart LR
  W[Web admin] -->|/api| K[Kong]
  P[Platform admin] -->|/api| K
  K --> H["HTTP :8080<br/>grpc-gateway"]
  H -->|in-process| I["Interceptors<br/>Auth → EmailVerified →<br/>PlatformAdmin → Garage"]
  G["gRPC :50051"] --> I
  I --> M["Modules<br/>identity · tenant · license · customer"]
  M --> PG[(PostgreSQL + RLS)]
  M --> R[(Redis)]
  M -->|outbox relay| KF[(Kafka)]
  M --> MAIL["Mail<br/>Mailpit / Mailjet"]
  M --> ST["Files<br/>local disk / GCS"]
```

**Contents:** [Modules](#modules) · [Run locally](#run-locally) ·
[Configuration](#configuration) · [Platform admins](#platform-admins) ·
[E-mail](#e-mail) · [File storage](#file-storage) · [Events](#events) ·
[Development](#development) · [Container image and staging](#container-image-and-staging)

## Modules

| Module | Package | Owns |
| --- | --- | --- |
| Identity | `internal/identity` | Accounts, sessions, e-mail verification and password-reset OTPs, Google sign-in, platform admins |
| Tenant | `internal/tenant` | Garages, staff, invitations, roles and staff groups |
| License | `internal/license` | Plans, per-account subscriptions, entitlements, plan limits |
| Customer | `internal/customer` | Customers, vehicles, partners |

Shared infrastructure (db, access, mail, storage, kafka, outbox, audit,
httpapi, auth) is in `internal/platform`. The gRPC interceptors are in
`internal/middleware`.

## Run locally

Start the full stack with Docker Compose:

```bash
cp .env_example .env
docker compose up --build        # Postgres, Redis, Kafka, Mailpit, backend, web admin, platform admin, Kong
open http://localhost:8000       # garage web admin through Kong
open http://admin.localhost:8000 # platform admin through Kong (plans, subscriptions)
open http://localhost:8025       # Mailpit: every e-mail sent locally
```

Or run only the dependencies in Compose and the backend on the host:

```bash
docker compose up -d vietgara-pgsql vietgara-redis vietgara-kafka vietgara-mailpit
make run                         # loads .env when present
```

To get into the admin pages, sign up with the e-mail set in
`PLATFORM_ADMIN_EMAILS` (`admin@viettechno.com` in `.env_example`) and then
restart the backend. See [Platform admins](#platform-admins).

### Database roles

On startup the server:

1. Applies the migrations as the schema owner (`MIGRATION_DATABASE_URL`).
2. Creates the `APP_DB_USER` login in the `vietgara_app` role, if that login
   does not exist yet.
3. Connects with `DATABASE_URL` as that login.

`vietgara_app` does not have `BYPASSRLS`, so row-level security applies to
every query the application runs. [`db/README.md`](db/README.md) covers
migrations and RLS.

## Configuration

Every setting is an environment variable. `.env_example` lists them all with
local defaults.

**Database and server**

| Variable | Purpose |
| --- | --- |
| `DATABASE_URL` | Application connection, as a `vietgara_app` login |
| `MIGRATION_DATABASE_URL` | Schema-owner connection for migrations. Defaults to `DATABASE_URL` |
| `APP_DB_USER`, `APP_DB_PASSWORD` | Login created in `vietgara_app` on startup, if set |
| `RUN_MIGRATIONS`, `MIGRATIONS_PATH` | Apply `db/migrations` on startup |
| `GRPC_PORT`, `HTTP_PORT` | Listener ports |
| `API_DOCS_ENABLED` | Serve the API reference at `/api/docs` and the OpenAPI document at `/api/openapi.json`. Defaults to `false`; Compose turns it on |

**Identity and access**

| Variable | Purpose |
| --- | --- |
| `JWT_SIGNING_KEY`, `JWT_ACCESS_TTL`, `JWT_REFRESH_TTL` | Token signing and lifetimes. Generate the key with `openssl rand -hex 32` |
| `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | Google sign-in popup. Register `APP_BASE_URL` + `/api/v1/auth/google/callback` as the redirect URI |
| `GOOGLE_MOBILE_CLIENT_IDS` | Google sign-in from the mobile apps (`POST /api/v1/auth/google/id-token`): comma-separated OAuth client IDs the apps' ID tokens are issued to (the Android "web/server" client and the iOS client). Empty disables it |
| `GOOGLE_AUTH_ENDPOINT`, `GOOGLE_TOKEN_ENDPOINT`, `GOOGLE_USERINFO_URL`, `GOOGLE_CERTS_URL` | Google's OAuth endpoints and the JWKS that mobile ID tokens are verified against. Default to Google's own; override them only to point the backend at a Google stub (the blackbox tests) |
| `PLATFORM_ADMIN_EMAILS` | Comma-separated e-mails granted platform admin. See [Platform admins](#platform-admins) |
| `APP_BASE_URL` | Public URL of the deployment, such as `https://gara.viettechno.com`. The web admin origin for the Google popup, the Google callback host, and the base of e-mailed links. Defaults to `http://localhost:8000` |

**Cache, licensing and events**

| Variable | Purpose |
| --- | --- |
| `REDIS_ADDR`, `REDIS_PASSWORD` | Sessions, OTPs, entitlement cache and transaction counters. If empty, these are kept in memory (single instance only) |
| `ENTITLEMENT_CACHE_TTL`, `LICENSE_EXPIRY_JOB_INTERVAL` | License enforcement |
| `KAFKA_BROKERS`, `KAFKA_TOPIC_PREFIX`, `KAFKA_CONSUMER_GROUP`, `OUTBOX_RELAY_INTERVAL` | Domain events. If `KAFKA_BROKERS` is empty, events are delivered in-process. See [Events](#events) |

**E-mail and files**

| Variable | Purpose |
| --- | --- |
| `MAIL_PROVIDER`, `MAIL_FROM_ADDRESS`, `MAIL_FROM_NAME` | E-mail provider and sender. See [E-mail](#e-mail) |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD` | SMTP provider (Mailpit) |
| `MAILJET_API_KEY`, `MAILJET_SECRET_KEY` | Mailjet provider |
| `STORAGE_BACKEND`, `STORAGE_LOCAL_DIR`, `STORAGE_PUBLIC_BASE_URL`, `STORAGE_GCS_BUCKET` | Uploaded files. See [File storage](#file-storage) |

## Platform admins

Platform admins manage plans and subscriptions (FR-IAM-08, FR-LIC-05). They
do not belong to any garage and cannot see a garage's business data. There
can be any number of them. Admin rights come only from configuration: no
screen or API grants them.

### Granting admin rights

1. The person signs up for a normal account, with e-mail/password or Google.
2. Add their e-mail to `PLATFORM_ADMIN_EMAILS` for that environment:

   ```bash
   PLATFORM_ADMIN_EMAILS=admin@viettechno.com,ops@viettechno.com
   ```

3. Restart the backend. On startup it adds every account whose e-mail is in
   the list (matched case-insensitively) to the `platform_admins` table, then
   logs `platform admins granted` with the number of new grants.

The `platform_admin` flag in the access token is read from the database each
time a session is issued or refreshed. It therefore takes effect within
`JWT_ACCESS_TTL` (20 minutes by default), or at once if the person signs out
and back in. The platform admin app (`vietgara-admin-web`) accepts only accounts that
have the flag; others see an access-denied page. The garage web admin has no
platform-admin pages.

Things to know:

- **The account must exist before the restart.** The server skips e-mails
  that have no account. A skipped e-mail is not granted later when that person
  signs up, so they must sign up first and the backend must restart after
  that.
- **The list only adds admins.** Removing an e-mail from
  `PLATFORM_ADMIN_EMAILS` does not revoke anyone. To revoke an admin, delete
  their row by hand:

  ```sql
  DELETE FROM platform_admins
  WHERE account_id = (SELECT id FROM accounts WHERE lower(email) = 'ops@viettechno.com');
  ```

  The change takes effect when that person's current access token expires.
- **Each environment has its own list.** Locally it comes from `.env`, through
  `docker-compose.yml`. In staging and production, set it in that deployment's
  configuration.

## E-mail

E-mails (verification OTP, password-reset OTP, staff invitation) are rendered
from `internal/platform/mail/templates/{vi,en}` in the recipient's language.
One of two providers sends them:

| Environment | `MAIL_PROVIDER` | Where mail goes |
| --- | --- | --- |
| Local, unit and integration tests | `smtp` | [Mailpit](https://mailpit.axllent.org/): SMTP on port `1025`, inbox UI on port `8025` |
| Staging, production | `mailjet` | [Mailjet](https://www.mailjet.com/) Send API v3.1 |

How failures are handled:

- The server refuses to start if `MAIL_PROVIDER=mailjet` and a Mailjet key is
  missing.
- If an OTP e-mail cannot be sent, the request fails and the code is
  discarded, so the user can retry right away.
- Invitation e-mails are sent after the invitation is committed. If sending
  fails, the error is logged and the invitation still appears on the
  invitee's Invitations page.

### Setting up Mailjet

1. Create a Mailjet account. Under **Account settings → Senders & domains**,
   add the sending domain (for example `viettechno.com`).
2. Publish the SPF and DKIM DNS records that Mailjet shows for the domain.
   Wait until both are validated: mail from an unvalidated sender is rejected
   or lands in spam.
3. Under **Account settings → API key management**, create a key pair. Use a
   separate pair for each environment.
4. Configure the backend. Keep the secret in Secret Manager, never in the
   repository.

   ```bash
   MAIL_PROVIDER=mailjet
   MAIL_FROM_ADDRESS=support@viettechno.com   # must be a validated sender
   MAIL_FROM_NAME=VietGara
   MAILJET_API_KEY=<api key>
   MAILJET_SECRET_KEY=<secret key>
   ```

5. Trigger a verification e-mail, then check delivery under **Statistics →
   Messages** in Mailjet. In staging, use a sender such as
   `no-reply@stagging.gara.viettechno.com` so test mail is easy to tell apart.

## File storage

`internal/platform/storage` stores garage logos:

| Environment | `STORAGE_BACKEND` | Notes |
| --- | --- | --- |
| Local, staging | `local` | Files are written under `STORAGE_LOCAL_DIR` (Compose mounts the `vietgara-uploads` volume there) and served at `/api/v1/files/…` |
| Production | `gcs` | Files go to the `STORAGE_GCS_BUCKET` bucket. Credentials come from the workload identity (Application Default Credentials). `STORAGE_PUBLIC_BASE_URL` is the public URL prefix, either the bucket or a CDN |

## Events

1. Modules write domain events to the transactional outbox.
2. A relay publishes them to the Kafka topics `<prefix>.<context>.events`,
   for example `vietgara.identity.events`.
3. Consumers record the ids of handled events in `processed_events`, retry
   with backoff, and move events that keep failing to `<topic>.dlq`.

Without `KAFKA_BROKERS`, events are delivered in-process after commit. The
event catalogue is in the Event Design document.

## Development

```bash
make help               # list every target
make test-unit          # unit tests with the 90% coverage gate
make test-integration   # repository and migration tests on a disposable Postgres (testcontainers)
make lint               # golangci-lint
make blackbox-stubbed   # API-level tests of the built image with stubbed third parties (testing/blackbox)
make generate-mocks     # mockery
make migrate-up         # apply migrations; also migrate-down N=…, migrate-force VERSION=…, migrate-version
```

Integration tests carry `//go:build integration` and live next to the code
they test. They get their database from `internal/platform/db/pgtest`, which
starts the same Postgres version as `docker-compose.yml`, applies
`db/migrations` the way the server does, and hands out a superuser
connection (for seeding past RLS) and a `vietgara_app` pool (subject to RLS).
`make lint` lints them too.

Locally, `go.work` in the parent directory builds against your
`vietgara-proto` checkout. CI (`.github/workflows/ci.yml`) builds against the
version pinned in `go.mod` instead. For every contract change, push and tag
`vietgara-proto`, then bump the version here.

The blackbox suite in [`testing/blackbox`](testing/blackbox/README.md) is a
separate Go module that drives the built image over the public REST API,
locally against stubbed third parties or against staging.

## Container image and staging

CI (`.github/workflows/ci.yml`, job **Docker image**) builds the image on
every run and, after all checks pass, pushes it to the GitHub Container
Registry on merges to `master` and `v*` tags, as
`ghcr.io/viettechno/vietgara-backend:<git describe>` (public).

To push by hand instead, `make image-push` builds the same image for
`linux/amd64` (cross-compiled, so it also works on Apple Silicon):

```bash
echo "$GHCR_TOKEN" | docker login ghcr.io -u <github user> --password-stdin   # classic token, write:packages
make image-check        # builds without go.work, as Docker does
make image-push
```

Inside Docker there is no `go.work`, so the image uses the `vietgara-proto`
version pinned in `go.mod`. `image-check`, which `image-build` and
`image-push` run first, fails early when the backend depends on unpublished
contract changes. Pushing uncommitted work is refused unless `ALLOW_DIRTY=1`.
`make help` lists the variables (`IMAGE_NAMESPACE`, `IMAGE_TAG`,
`IMAGE_PLATFORM`).

Staging runs the published images on a VM. The Compose file, settings template
and step-by-step guide are in the `vietgara-infra` repository
(`staging/README.md`).
