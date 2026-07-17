# Database Migrations & Row-Level Security (RLS)

This directory contains golang-migrate compatible PostgreSQL migrations with Row-Level Security (RLS) policies for multi-tenant data isolation.

## Overview

The migrations create three core tables for the Vietgara backend:
- **accounts** - User accounts with authentication details
- **garages** - Garage/tenant information
- **garage_staff** - Staff assignments to garages with roles

Row-Level Security is enabled on `garages` and `garage_staff` tables to automatically enforce data isolation based on a session variable.

## Migration Files

### 000001_create_accounts_table
Creates the `accounts` table with:
- UUID primary key
- Email and phone as unique nullable fields
- Auth provider and identity platform UID tracking
- Status field (active, inactive, suspended)
- Automatic `updated_at` trigger
- Indexes on email, phone, identity_platform_uid, auth_provider

**Relationships**: Accounts are referenced by garages and garage_staff

### 000002_create_garages_table
Creates the `garages` table with:
- UUID primary key (used as tenant_id)
- Foreign key to accounts
- Tenant-specific configuration (timezone, currency, tax code, etc.)
- Status field with enum type
- Automatic `updated_at` trigger
- **Row-Level Security** enabled with policy checking `app.current_garage_id` session variable

**RLS Policy**: 
```sql
CREATE POLICY garages_rls_policy ON garages
FOR ALL
USING (id = current_setting('app.current_garage_id')::uuid)
```

This policy restricts access to only the garage matching the session variable.

### 000003_create_garage_staff_table
Creates the `garage_staff` table with:
- UUID primary key
- Foreign keys to garages and accounts
- Role field with enum type (owner, accountant, technician, service_advisor, inventory_staff)
- Status field with enum type
- Automatic `updated_at` trigger
- Unique constraint on (garage_id, account_id) pairs
- **Row-Level Security** enabled with policy checking `app.current_garage_id` session variable

**RLS Policy**:
```sql
CREATE POLICY garage_staff_rls_policy ON garage_staff
FOR ALL
USING (garage_id = current_setting('app.current_garage_id')::uuid)
```

## Running Migrations

### Using golang-migrate CLI

```bash
# Install golang-migrate
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Run migrations up
migrate -path db/migrations -database "postgres://user:pass@localhost:5432/vietgara?sslmode=disable" up

# Rollback one migration
migrate -path db/migrations -database "postgres://user:pass@localhost:5432/vietgara?sslmode=disable" down

# Force version
migrate -path db/migrations -database "postgres://user:pass@localhost:5432/vietgara?sslmode=disable" force 3
```

### Using Go code

```go
import "github.com/golang-migrate/migrate/v4"
import _ "github.com/golang-migrate/migrate/v4/source/file"
import _ "github.com/golang-migrate/migrate/v4/database/postgres"

m, err := migrate.New(
    "file://db/migrations",
    "postgres://user:pass@localhost:5432/vietgara?sslmode=disable")

err = m.Up() // Run all up migrations
```

## Row-Level Security (RLS)

### How RLS Works

1. Before querying `garages` or `garage_staff` tables, set the session variable:
   ```sql
   SELECT set_config('app.current_garage_id', '12345678-1234-1234-1234-123456789012'::text, false);
   ```

2. All subsequent queries automatically filter rows where the `garage_id` (or `id` for garages) matches the session variable

3. If the session variable is not set, queries return 0 rows (RLS denies access by default)

### Setting Session Variables in Application

**For PostgreSQL connections:**

```go
// Set session variable for the connection
ctx := context.Background()
_, err := db.ExecContext(ctx, 
    "SELECT set_config('app.current_garage_id', $1::text, false)", 
    garageID)

// All subsequent queries in this connection use the variable
// Be careful with connection pooling - variable is per-connection
```

**In middleware (recommended approach):**

```go
func RLSMiddleware(garageID string) middleware.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Set RLS session variable for this request
            ctx := r.Context()
            _, err := db.ExecContext(ctx,
                "SELECT set_config('app.current_garage_id', $1::text, false)",
                garageID)
            if err != nil {
                http.Error(w, "Failed to set session", http.StatusInternalServerError)
                return
            }
            
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

### RLS Policies Explained

The RLS policies use PostgreSQL's `current_setting()` function to read the session variable:

```sql
-- For garages table
id = current_setting('app.current_garage_id')::uuid

-- For garage_staff table
garage_id = current_setting('app.current_garage_id')::uuid
```

**Key behaviors:**
- If `app.current_garage_id` is not set, `current_setting()` raises an exception (unless handled)
- We should wrap in a function that defaults safely:
  ```sql
  id = (current_setting('app.current_garage_id', true) FILTER (WHERE true))::uuid
  ```

### Gotchas & Best Practices

1. **Connection pooling**: Session variables are per-connection. Use connection context properly
2. **Default values**: Set a safe default or handle exceptions
3. **Admin operations**: Bypass RLS using `SECURITY DEFINER` functions
4. **Testing**: Always set the session variable in tests

## Testing

### Run RLS Tests

```bash
# Run all tests (starts PostgreSQL container automatically)
go test ./db -v -tags=testcontainers

# Run specific test
go test ./db -run TestRLS_QueryFailsWithoutSessionVariable -v -tags=testcontainers

# Run with coverage
go test ./db -v -cover -tags=testcontainers
```

### Test Coverage

The test file (`migrations_test.go`) includes:

1. **TestRLS_QueryFailsWithoutSessionVariable** - Verifies queries return 0 rows without session variable
2. **TestRLS_QuerySucceedsWithCorrectSessionVariable** - Verifies queries work with matching session variable
3. **TestRLS_QueryBlockedWithWrongSessionVariable** - Verifies multi-tenant isolation (can't see other garages)
4. **TestRLS_GarageStaffRowLevelSecurity** - Tests RLS on garage_staff table
5. **TestRLS_SessionVariableHandling** - Tests session variable set/retrieve

### What Tests Verify

- ✅ Queries return 0 rows when session variable is not set
- ✅ Queries return correct rows when session variable matches
- ✅ Users cannot access other garage data (tenant isolation)
- ✅ RLS works on both `garages` and `garage_staff` tables
- ✅ Session variables persist correctly per connection

### Using testcontainers-go

The tests use `testcontainers-go` to automatically:
1. Start a PostgreSQL 15 container
2. Wait for the database to be ready
3. Run migrations
4. Insert test data
5. Verify RLS behavior
6. Clean up the container

**Requirements:**
- Docker must be running
- `github.com/testcontainers/testcontainers-go` installed
- `github.com/lib/pq` installed

**Installation:**
```bash
go get -u github.com/testcontainers/testcontainers-go
go get -u github.com/lib/pq
```

## Database Schema Diagram

```
┌─────────────────────────────────────────┐
│            accounts                     │
├─────────────────────────────────────────┤
│ id (uuid, PK)                           │
│ email (varchar, unique)                 │
│ phone (varchar, unique)                 │
│ auth_provider (varchar)                 │
│ identity_platform_uid (varchar, unique) │
│ full_name (varchar)                     │
│ status (varchar)                        │
│ created_at, updated_at                  │
└─────────────────────────────────────────┘
        ▲                        ▲
        │ (PK ref)              │ (PK ref)
        │ (account_id)          │ (account_id)
        │                       │
┌───────┴─────────────┐    ┌────┴──────────────────┐
│    garages          │    │  garage_staff         │
├─────────────────────┤    ├───────────────────────┤
│ id (uuid, PK)       │◄───│ garage_id (uuid, FK)  │
│ account_id (uuid,FK)│    │ account_id (uuid, FK) │
│ name (varchar)      │    │ role (enum)           │
│ tax_code (varchar)  │    │ status (enum)         │
│ address (text)      │    │ created_at, updated_at│
│ logo_url (varchar)  │    └───────────────────────┘
│ timezone (varchar)  │
│ currency (varchar)  │
│ status (enum)       │
│ created_at,updated_at
└─────────────────────┘

RLS Enabled:
- garages: WHERE id = app.current_garage_id
- garage_staff: WHERE garage_id = app.current_garage_id
```

## Administration & Maintenance

### Viewing Migrations Status

```bash
migrate -path db/migrations -database "postgres://..." version
```

### Viewing RLS Policies

```sql
-- List all policies on garages table
SELECT * FROM pg_policy WHERE relname = 'garages';

-- View policy definition
SELECT pg_get_expr(qual, relname::regclass) FROM pg_policy 
WHERE relname = 'garages' AND polname = 'garages_rls_policy';
```

### Bypassing RLS (Admin Operations)

```sql
-- Temporarily disable RLS for current session
ALTER TABLE garages DISABLE ROW LEVEL SECURITY;

-- Re-enable RLS
ALTER TABLE garages ENABLE ROW LEVEL SECURITY;

-- Or use SECURITY DEFINER function
CREATE OR REPLACE FUNCTION get_all_garages() 
RETURNS TABLE (id uuid, name varchar)
LANGUAGE SQL
SECURITY DEFINER
AS $$
  SELECT id, name FROM garages;
$$;
```

### Troubleshooting

**Problem**: "permission denied for schema public"
- **Solution**: Ensure user has correct schema permissions

**Problem**: "unrecognized configuration parameter 'app.current_garage_id'"
- **Solution**: The parameter is custom, auto-created on first use

**Problem**: RLS not working (returns all rows)
- **Solution**: Check if RLS is actually enabled:
  ```sql
  SELECT relname, relrowsecurity FROM pg_class WHERE relname IN ('garages', 'garage_staff');
  ```

**Problem**: Session variable not persisting across connections
- **Solution**: Session variables are per-connection. Use connection pooling correctly and set before each query batch.

## Performance Considerations

1. **Indexes**: Foreign key columns are indexed for faster filtering
2. **Unique constraints**: (garage_id, account_id) on garage_staff prevents duplicates
3. **RLS overhead**: Minimal - adds one equality check per row
4. **Query optimization**: RLS policies are inline, optimized by query planner

## Future Enhancements

- [ ] Add audit table with RLS
- [ ] Add invitation flow for staff
- [ ] Add role-based permissions table
- [ ] Add activity log with RLS
- [ ] Add soft-delete columns

## Related Files

- [IDENTITY_IMPLEMENTATION.md](../IDENTITY_IMPLEMENTATION.md) - Identity package setup
- [IDENTITY_QUICKSTART.md](../IDENTITY_QUICKSTART.md) - Quick start guide
