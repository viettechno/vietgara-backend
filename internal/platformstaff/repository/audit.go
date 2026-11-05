package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/domain"
)

var errAuditNoTransaction = errors.New("platformstaff: reading the audit log needs a transaction")

var auditLikeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// ListAuditLog returns the platform audit log: the entries that belong to no
// garage, newest first. The audit_log policy shows them only while
// app.platform_audit is set, which is for this query alone.
func (r *Postgres) ListAuditLog(ctx context.Context, f domain.AuditFilter, page domain.Page) ([]*domain.AuditEntry, int, error) {
	if !platformdb.InTransaction(ctx) {
		return nil, 0, errAuditNoTransaction
	}

	q := r.q(ctx)

	if _, err := q.Exec(ctx, `SELECT set_config('app.platform_audit', 'on', true)`); err != nil {
		return nil, 0, err
	}

	entries, total, err := r.queryAuditLog(ctx, q, f, page)

	_, offErr := q.Exec(ctx, `SELECT set_config('app.platform_audit', '', true)`)

	if err = errors.Join(err, offErr); err != nil {
		return nil, 0, err
	}

	return entries, total, nil
}

func (r *Postgres) queryAuditLog(ctx context.Context, q platformdb.Querier, f domain.AuditFilter, page domain.Page) ([]*domain.AuditEntry, int, error) {
	const filter = `
		FROM audit_log a
		WHERE a.garage_id IS NULL
			AND ($1 = '' OR a.action LIKE $1 || '%')
			AND ($2 = '' OR a.entity_type = $2)
			AND ($3 = '' OR a.actor_user_id = NULLIF($3, '')::uuid)
			AND ($4::timestamptz IS NULL OR a.created_at >= $4)
			AND ($5::timestamptz IS NULL OR a.created_at < $5)`

	action := auditLikeEscaper.Replace(f.Action)

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*)::int`+filter, action, f.EntityType, f.ActorUserID, f.From, f.To).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := q.Query(ctx, `
		SELECT a.id, COALESCE(a.actor_user_id::text, ''), a.action, a.entity_type, a.entity_id,
			COALESCE(a.before::text, ''), COALESCE(a.after::text, ''), COALESCE(a.ip, ''), a.created_at`+filter+`
		ORDER BY a.created_at DESC, a.id LIMIT $6 OFFSET $7`,
		action, f.EntityType, f.ActorUserID, f.From, f.To, page.PageSize, page.Offset())
	if err != nil {
		return nil, 0, err
	}

	entries, err := collect(rows, func(row pgx.Row) (*domain.AuditEntry, error) {
		var e domain.AuditEntry

		err := row.Scan(&e.ID, &e.ActorUserID, &e.Action, &e.EntityType, &e.EntityID, &e.BeforeJSON, &e.AfterJSON, &e.IP, &e.CreatedAt)

		return &e, err
	})
	if err != nil {
		return nil, 0, err
	}

	return entries, total, nil
}
