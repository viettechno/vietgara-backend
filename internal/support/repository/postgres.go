// Package repository stores support grants and their access log in
// PostgreSQL (migration 000034). Both tables have row-level security: the
// owner's side is scoped to the garage, the staff's side to the user.
package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/support/domain"
)

// Postgres implements usecase.Repository.
type Postgres struct {
	pool *platformdb.Pool
}

// NewPostgres returns a repository backed by pool.
func NewPostgres(pool *platformdb.Pool) *Postgres { return &Postgres{pool: pool} }

func (r *Postgres) q(ctx context.Context) platformdb.Querier {
	return platformdb.QuerierFromContext(ctx, r.pool)
}

var errNoTransaction = errors.New("support: the directory setting needs a transaction")

// withDirectory lets fn see garages outside the caller's scope -- their name
// and owner, which is all these queries select -- and closes that again
// before returning (migration 000034).
func (r *Postgres) withDirectory(ctx context.Context, fn func(q platformdb.Querier) error) error {
	if !platformdb.InTransaction(ctx) {
		return errNoTransaction
	}

	q := r.q(ctx)

	if _, err := q.Exec(ctx, `SELECT set_config('app.garage_directory', 'on', true)`); err != nil {
		return err
	}

	err := fn(q)

	_, offErr := q.Exec(ctx, `SELECT set_config('app.garage_directory', '', true)`)

	return errors.Join(err, offErr)
}

const grantSelect = `
	SELECT g.id, g.garage_id, COALESCE(ga.name, ''), g.staff_user_id, su.full_name, su.email, su.locale::text, g.reason,
		g.ttl_seconds, g.requested_at, COALESCE(g.approved_by::text, ''), COALESCE(au.full_name, ''),
		g.approved_at, g.expires_at, g.revoked_at, g.granted_by_platform_admin
	FROM support_access_grants g
	JOIN users su ON su.id = g.staff_user_id
	LEFT JOIN users au ON au.id = g.approved_by
	LEFT JOIN garages ga ON ga.id = g.garage_id`

func scanGrant(row pgx.Row) (*domain.Grant, error) {
	var (
		g   domain.Grant
		ttl int
	)

	err := row.Scan(&g.ID, &g.GarageID, &g.GarageName, &g.StaffUserID, &g.StaffName, &g.StaffEmail, &g.StaffLocale, &g.Reason,
		&ttl, &g.RequestedAt, &g.ApprovedBy, &g.ApproverName, &g.ApprovedAt, &g.ExpiresAt, &g.RevokedAt,
		&g.GrantedByPlatformAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrGrantNotFound
	}

	if err != nil {
		return nil, err
	}

	g.TTL = time.Duration(ttl) * time.Second

	return &g, nil
}

func (r *Postgres) queryGrants(ctx context.Context, where string, args ...any) ([]*domain.Grant, error) {
	var grants []*domain.Grant

	err := r.withDirectory(ctx, func(q platformdb.Querier) error {
		rows, err := q.Query(ctx, grantSelect+where+` ORDER BY g.requested_at DESC, g.id`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			g, err := scanGrant(rows)
			if err != nil {
				return err
			}

			grants = append(grants, g)
		}

		return rows.Err()
	})

	return grants, err
}

// Create stores a new grant and fills its ID.
func (r *Postgres) Create(ctx context.Context, g *domain.Grant) error {
	return r.q(ctx).QueryRow(ctx, `
		INSERT INTO support_access_grants
			(garage_id, staff_user_id, reason, ttl_seconds, requested_at, approved_at, expires_at, granted_by_platform_admin)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		g.GarageID, g.StaffUserID, g.Reason, int(g.TTL.Seconds()), g.RequestedAt, g.ApprovedAt, g.ExpiresAt,
		g.GrantedByPlatformAdmin,
	).Scan(&g.ID)
}

// Update stores the answer to a grant: approval and revocation.
func (r *Postgres) Update(ctx context.Context, g *domain.Grant) error {
	tag, err := r.q(ctx).Exec(ctx, `
		UPDATE support_access_grants
		SET approved_by = NULLIF($2, '')::uuid, approved_at = $3, expires_at = $4, revoked_at = $5
		WHERE id = $1 AND revoked_at IS NULL AND (approved_at IS NULL OR approved_at = $3)`,
		g.ID, g.ApprovedBy, g.ApprovedAt, g.ExpiresAt, g.RevokedAt)
	if err != nil {
		return err
	}

	// The grant was loaded a moment ago: no row means another request
	// answered it first (a revoked grant never changes, an approved one is
	// never approved again).
	if tag.RowsAffected() == 0 {
		return domain.ErrGrantNotOpen
	}

	return nil
}

// Find returns a grant the caller's scope can see.
func (r *Postgres) Find(ctx context.Context, id string) (*domain.Grant, error) {
	var grant *domain.Grant

	err := r.withDirectory(ctx, func(q platformdb.Querier) error {
		var err error

		grant, err = scanGrant(q.QueryRow(ctx, grantSelect+` WHERE g.id = $1`, id))

		return err
	})

	return grant, err
}

// ListByStaff returns the grants of a staff member, newest first.
func (r *Postgres) ListByStaff(ctx context.Context, staffUserID string) ([]*domain.Grant, error) {
	return r.queryGrants(ctx, ` WHERE g.staff_user_id = $1`, staffUserID)
}

// ListByGarage returns the grants of a garage, newest first.
func (r *Postgres) ListByGarage(ctx context.Context, garageID string) ([]*domain.Grant, error) {
	return r.queryGrants(ctx, ` WHERE g.garage_id = $1`, garageID)
}

// HasOpen reports whether the staff member has a pending or active grant for
// the garage.
func (r *Postgres) HasOpen(ctx context.Context, garageID, staffUserID string, now time.Time) (bool, error) {
	var open bool

	// Serializes the requests of one staff member for one garage until the
	// transaction ends, so a double submit cannot open two grants.
	if _, err := r.q(ctx).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text || $2::text, 0))`, garageID, staffUserID); err != nil {
		return false, err
	}

	err := r.q(ctx).QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM support_access_grants
			WHERE garage_id = $1 AND staff_user_id = $2 AND revoked_at IS NULL
				AND ((approved_at IS NULL AND requested_at > $3::timestamptz - make_interval(secs => $4::float8))
					OR expires_at > $3::timestamptz))`,
		garageID, staffUserID, now, domain.RequestLifetime.Seconds(),
	).Scan(&open)

	return open, err
}

// ActiveGrant returns the grant that lets the staff member read the garage
// now, with the garage's owner, or domain.ErrGrantNotFound.
func (r *Postgres) ActiveGrant(ctx context.Context, garageID, staffUserID string, now time.Time) (grantID, ownerUserID string, err error) {
	err = r.q(ctx).QueryRow(ctx, `
		SELECT g.id, ga.user_id
		FROM support_access_grants g
		JOIN garages ga ON ga.id = g.garage_id
		WHERE g.garage_id = $1 AND g.staff_user_id = $2 AND g.revoked_at IS NULL AND g.expires_at > $3
		ORDER BY g.expires_at DESC LIMIT 1`, garageID, staffUserID, now,
	).Scan(&grantID, &ownerUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", domain.ErrGrantNotFound
	}

	return grantID, ownerUserID, err
}

// Garage returns the name and owner of a garage, or domain.ErrGarageNotFound.
func (r *Postgres) Garage(ctx context.Context, garageID string) (*domain.Garage, error) {
	var garage domain.Garage

	err := r.withDirectory(ctx, func(q platformdb.Querier) error {
		return q.QueryRow(ctx, `
			SELECT ga.id, ga.name, ga.user_id, u.full_name, u.email, u.locale::text
			FROM garages ga JOIN users u ON u.id = ga.user_id
			WHERE ga.id = $1`, garageID,
		).Scan(&garage.ID, &garage.Name, &garage.OwnerUserID, &garage.OwnerName, &garage.OwnerEmail, &garage.OwnerLocale)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrGarageNotFound
	}

	if err != nil {
		return nil, err
	}

	return &garage, nil
}

// Directory searches garages by name, owner name or owner e-mail.
func (r *Postgres) Directory(ctx context.Context, query string, page domain.Page) ([]*domain.DirectoryEntry, int, error) {
	pattern := "%" + likeEscaper.Replace(strings.TrimSpace(query)) + "%"

	var (
		entries []*domain.DirectoryEntry
		total   int
	)

	err := r.withDirectory(ctx, func(q platformdb.Querier) error {
		const filter = `
			FROM garages ga JOIN users u ON u.id = ga.user_id
			WHERE ga.name ILIKE $1 OR u.full_name ILIKE $1 OR u.email ILIKE $1`

		if err := q.QueryRow(ctx, `SELECT count(*)::int `+filter, pattern).Scan(&total); err != nil {
			return err
		}

		rows, err := q.Query(ctx, `
			SELECT ga.id, ga.name, u.full_name, u.email, ga.status = 'active' `+filter+`
			ORDER BY lower(ga.name), ga.id LIMIT $2 OFFSET $3`, pattern, page.PageSize, page.Offset())
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var e domain.DirectoryEntry
			if err := rows.Scan(&e.GarageID, &e.Name, &e.OwnerName, &e.OwnerEmail, &e.Active); err != nil {
				return err
			}

			entries = append(entries, &e)
		}

		return rows.Err()
	})

	return entries, total, err
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// InsertEvent appends a read to the support access log.
func (r *Postgres) InsertEvent(ctx context.Context, e *domain.AccessEvent) error {
	return r.q(ctx).QueryRow(ctx, `
		INSERT INTO support_access_events (grant_id, garage_id, staff_user_id, method, entity_type, entity_id)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		e.GrantID, e.GarageID, e.StaffUserID, e.Method, e.EntityType, e.EntityID,
	).Scan(&e.ID, &e.CreatedAt)
}

// ListEvents returns the reads of a garage, newest first, optionally of one grant.
func (r *Postgres) ListEvents(ctx context.Context, garageID, grantID string, page domain.Page) ([]*domain.AccessEvent, int, error) {
	q := r.q(ctx)
	filter := ` FROM support_access_events e JOIN users u ON u.id = e.staff_user_id
		WHERE e.garage_id = $1 AND ($2 = '' OR e.grant_id = NULLIF($2, '')::uuid)`

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*)::int`+filter, garageID, grantID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := q.Query(ctx, `
		SELECT e.id, e.grant_id, e.staff_user_id, u.full_name, e.method, e.entity_type, e.entity_id, e.created_at`+filter+`
		ORDER BY e.created_at DESC, e.id LIMIT $3 OFFSET $4`, garageID, grantID, page.PageSize, page.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var events []*domain.AccessEvent

	for rows.Next() {
		e := domain.AccessEvent{GarageID: garageID}
		if err := rows.Scan(&e.ID, &e.GrantID, &e.StaffUserID, &e.StaffName, &e.Method, &e.EntityType, &e.EntityID, &e.CreatedAt); err != nil {
			return nil, 0, err
		}

		events = append(events, &e)
	}

	return events, total, rows.Err()
}

// ListOutbox returns the outbox events of a garage, newest first.
func (r *Postgres) ListOutbox(ctx context.Context, garageID, status string, page domain.Page) ([]*domain.OutboxEvent, int, error) {
	q := r.q(ctx)
	filter := ` FROM outbox_events WHERE garage_id = $1 AND ($2 = '' OR status::text = $2)`

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*)::int`+filter, garageID, status).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := q.Query(ctx, `
		SELECT id, event_type, status::text, attempts, COALESCE(last_error, ''), created_at, published_at`+filter+`
		ORDER BY created_at DESC, id LIMIT $3 OFFSET $4`, garageID, status, page.PageSize, page.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var events []*domain.OutboxEvent

	for rows.Next() {
		var e domain.OutboxEvent
		if err := rows.Scan(&e.ID, &e.EventType, &e.Status, &e.Attempts, &e.LastError, &e.CreatedAt, &e.PublishedAt); err != nil {
			return nil, 0, err
		}

		events = append(events, &e)
	}

	return events, total, rows.Err()
}

// ListNotifications returns the notification log of the garage's owner and
// active staff, newest first. The caller is scoped to the garage.
func (r *Postgres) ListNotifications(ctx context.Context, garageID string, page domain.Page) ([]*domain.Notification, int, error) {
	q := r.q(ctx)
	filter := ` FROM notification_log n WHERE n.user_id IN (
			SELECT user_id FROM garages WHERE id = $1
			UNION SELECT user_id FROM garage_staff WHERE garage_id = $1 AND status = 'active')`

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*)::int`+filter, garageID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := q.Query(ctx, `
		SELECT n.id, n.channel::text, n.template, n.recipient, n.status::text, COALESCE(n.error, ''), n.created_at`+filter+`
		ORDER BY n.created_at DESC, n.id LIMIT $2 OFFSET $3`, garageID, page.PageSize, page.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var notifications []*domain.Notification

	for rows.Next() {
		var n domain.Notification
		if err := rows.Scan(&n.ID, &n.Channel, &n.Template, &n.Recipient, &n.Status, &n.Error, &n.CreatedAt); err != nil {
			return nil, 0, err
		}

		notifications = append(notifications, &n)
	}

	return notifications, total, rows.Err()
}

// SetReadOnly makes the rest of the request's transaction read-only: any
// write the handler attempts fails.
func (r *Postgres) SetReadOnly(ctx context.Context) error {
	_, err := r.q(ctx).Exec(ctx, `SET TRANSACTION READ ONLY`)

	return err
}
