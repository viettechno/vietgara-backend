package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
)

type GarageRepository interface {
	FindByID(ctx context.Context, id string) (*domain.Garage, error)
	ListByAccountID(ctx context.Context, accountID string) ([]*domain.Garage, error)
	Create(ctx context.Context, g *domain.Garage) error
	Update(ctx context.Context, g *domain.Garage) error
}

type PostgresGarageRepository struct {
	pool *platformdb.Pool
}

func NewPostgresGarageRepository(pool *platformdb.Pool) *PostgresGarageRepository {
	return &PostgresGarageRepository{pool: pool}
}

const garageColumns = `id, account_id, name, tax_code, address, logo_url, timezone, currency, status, created_at, updated_at`

func scanGarage(row pgx.Row) (*domain.Garage, error) {
	var g domain.Garage
	var status string
	err := row.Scan(&g.ID, &g.AccountID, &g.Name, &g.TaxCode, &g.Address, &g.LogoURL, &g.Timezone, &g.Currency, &status, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrGarageNotFound
		}
		return nil, err
	}
	g.Status = domain.GarageStatus(status)
	return &g, nil
}

// FindByID intentionally does NOT rely on the RLS session variable being
// set beforehand -- listing/creating garages is inherently a
// cross-tenant operation scoped by account_id, not garage_id. RLS on the
// garages table still applies (see migrations), so this query only
// returns rows where id = the currently-set app.current_garage_id UNLESS
// called outside a garage-scoped transaction (e.g. right after creating
// the garage, within the same transaction that set the variable to the
// new garage's own id -- see usecase.CreateGarage).
func (r *PostgresGarageRepository) FindByID(ctx context.Context, id string) (*domain.Garage, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	row := q.QueryRow(ctx, `SELECT `+garageColumns+` FROM garages WHERE id = $1`, id)
	return scanGarage(row)
}

func (r *PostgresGarageRepository) ListByAccountID(ctx context.Context, accountID string) ([]*domain.Garage, error) {
	// NOTE: this is the one read path in the codebase that must bypass the
	// per-garage RLS policy by design, since "list every garage I own" is
	// inherently a cross-tenant query for the account itself. In
	// production this uses a Postgres role with BYPASSRLS restricted to
	// this exact query, or a SECURITY DEFINER function -- flagged here so
	// it isn't copied as a pattern for any other repository method.
	q := platformdb.QuerierFromContext(ctx, r.pool)
	rows, err := q.Query(ctx, `SELECT `+garageColumns+` FROM garages WHERE account_id = $1 ORDER BY created_at ASC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*domain.Garage
	for rows.Next() {
		g, err := scanGarage(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	return result, rows.Err()
}

func (r *PostgresGarageRepository) Create(ctx context.Context, g *domain.Garage) error {
	// g.ID is generated call-site side (see usecase.CreateGarage), so it is
	// inserted explicitly -- the callers rely on FindByID(g.ID) and the
	// garage-scoped RLS WITH CHECK (id = app.current_garage_id) both working
	// with the caller-chosen id.
	q := platformdb.QuerierFromContext(ctx, r.pool)
	row := q.QueryRow(ctx, `
		INSERT INTO garages (id, account_id, name, tax_code, address, logo_url, timezone, currency, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at, updated_at`,
		g.ID, g.AccountID, g.Name, g.TaxCode, g.Address, g.LogoURL, g.Timezone, g.Currency, string(g.Status),
	)
	return row.Scan(&g.ID, &g.CreatedAt, &g.UpdatedAt)
}

func (r *PostgresGarageRepository) Update(ctx context.Context, g *domain.Garage) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE garages SET name=$1, tax_code=$2, address=$3, logo_url=$4, timezone=$5, currency=$6, status=$7, updated_at=now()
		WHERE id = $8`,
		g.Name, g.TaxCode, g.Address, g.LogoURL, g.Timezone, g.Currency, string(g.Status), g.ID,
	)
	return err
}
