package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

const countColumns = `c.id, c.garage_id, c.number, c.name, c.notes, c.status::text, c.completed_at, c.completed_by, c.cancelled_at,
	c.created_by, c.updated_by, c.created_at, c.updated_at`

func scanCountHeader(row pgx.Row, extra ...any) (*domain.StockCount, error) {
	var (
		c      domain.StockCount
		status string
	)

	dest := append([]any{
		&c.ID, &c.GarageID, &c.Number, &c.Name, &c.Notes, &status, &c.CompletedAt, &c.CompletedBy,
		&c.CancelledAt, &c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt,
	}, extra...)

	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrStockCountNotFound
		}

		return nil, err
	}

	c.Status = domain.StockCountStatus(status)

	return &c, nil
}

// CreateStockCount inserts the count header and its lines.
func (r *PostgresRepository) CreateStockCount(ctx context.Context, c *domain.StockCount) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		INSERT INTO stock_counts (id, garage_id, number, name, notes, status, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6::stock_count_status, $7, $8)
		RETURNING created_at, updated_at`,
		c.ID, c.GarageID, c.Number, c.Name, c.Notes, string(c.Status), c.CreatedBy, c.UpdatedBy,
	).Scan(&c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return err
	}

	return r.InsertStockCountLines(ctx, c.GarageID, c.ID, c.UpdatedBy, c.Lines)
}

// InsertStockCountLines adds lines to a count.
func (r *PostgresRepository) InsertStockCountLines(ctx context.Context, garageID, countID string, actor *string, lines []domain.StockCountLine) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	for _, l := range lines {
		_, err := q.Exec(ctx, `
			INSERT INTO stock_count_lines (id, garage_id, stock_count_id, position, part_id, unit_id, entered_counted_quantity,
			                               counted_quantity, system_quantity, average_cost, counted_at, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8::numeric, $9::numeric, $10::numeric, $11, $12, $12)`,
			l.ID, garageID, countID, l.Position, l.PartID, l.UnitID, l.EnteredCounted, l.Counted, l.SystemQuantity, l.AverageCost,
			l.CountedAt, actor)
		if err != nil {
			return err
		}
	}

	return nil
}

// FindStockCount returns the count with its lines in order. A line's average
// cost is the stored one, else the part's live average.
func (r *PostgresRepository) FindStockCount(ctx context.Context, garageID, countID string, forUpdate bool) (*domain.StockCount, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	lock := ""
	if forUpdate {
		lock = forUpdateClause
	}

	c, err := scanCountHeader(q.QueryRow(ctx,
		`SELECT `+countColumns+` FROM stock_counts c WHERE c.garage_id = $1 AND c.id = $2`+lock, garageID, countID))
	if err != nil {
		return nil, err
	}

	rows, err := q.Query(ctx, `
		SELECT l.id, l.position, l.part_id, l.unit_id, trim_scale(l.entered_counted_quantity)::text,
		       trim_scale(l.counted_quantity)::text, trim_scale(l.system_quantity)::text,
		       trim_scale(COALESCE(l.average_cost, b.average_cost, 0))::text, l.counted_at
		FROM stock_count_lines l
		LEFT JOIN stock_balance b ON b.garage_id = l.garage_id AND b.part_id = l.part_id
		WHERE l.garage_id = $1 AND l.stock_count_id = $2 ORDER BY l.position, l.id`, garageID, countID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var l domain.StockCountLine
		if err := rows.Scan(&l.ID, &l.Position, &l.PartID, &l.UnitID, &l.EnteredCounted, &l.Counted, &l.SystemQuantity,
			&l.AverageCost, &l.CountedAt); err != nil {
			return nil, err
		}

		c.Lines = append(c.Lines, l)
	}

	return c, rows.Err()
}

// SaveStockCount persists the header of the count, not its lines.
func (r *PostgresRepository) SaveStockCount(ctx context.Context, c *domain.StockCount) error {
	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		UPDATE stock_counts SET name = $3, notes = $4, status = $5::stock_count_status, completed_at = $6, completed_by = $7,
			cancelled_at = $8, updated_by = $9
		WHERE garage_id = $1 AND id = $2
		RETURNING updated_at`,
		c.GarageID, c.ID, c.Name, c.Notes, string(c.Status), c.CompletedAt, c.CompletedBy, c.CancelledAt, c.UpdatedBy,
	).Scan(&c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrStockCountNotFound
	}

	return err
}

// SaveStockCountLine persists the counted, system and cost fields of a line.
func (r *PostgresRepository) SaveStockCountLine(ctx context.Context, garageID, countID string, l *domain.StockCountLine) error {
	tag, err := platformdb.QuerierFromContext(ctx, r.pool).Exec(ctx, `
		UPDATE stock_count_lines SET unit_id = $4, entered_counted_quantity = $5::numeric, counted_quantity = $6::numeric,
			system_quantity = $7::numeric, average_cost = $8::numeric, counted_at = $9, position = $10
		WHERE garage_id = $1 AND stock_count_id = $2 AND id = $3`,
		garageID, countID, l.ID, l.UnitID, l.EnteredCounted, l.Counted, l.SystemQuantity, l.AverageCost, l.CountedAt, l.Position)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrStockCountLineNotFound
	}

	return nil
}

// DeleteStockCountLine removes a line of the count.
func (r *PostgresRepository) DeleteStockCountLine(ctx context.Context, garageID, countID, lineID string) error {
	tag, err := platformdb.QuerierFromContext(ctx, r.pool).Exec(ctx,
		`DELETE FROM stock_count_lines WHERE garage_id = $1 AND stock_count_id = $2 AND id = $3`, garageID, countID, lineID)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrStockCountLineNotFound
	}

	return nil
}

// ClearStockCountLines removes every line of the count.
func (r *PostgresRepository) ClearStockCountLines(ctx context.Context, garageID, countID string) error {
	_, err := platformdb.QuerierFromContext(ctx, r.pool).Exec(ctx,
		`DELETE FROM stock_count_lines WHERE garage_id = $1 AND stock_count_id = $2`, garageID, countID)

	return err
}

// ListStockCounts returns one page of counts, newest first, with the number of
// lines per result and the value of the difference (stored cost, else the
// live average).
func (r *PostgresRepository) ListStockCounts(ctx context.Context, garageID string, filter usecase.StockCountFilter) ([]*domain.StockCount, int, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	where := ` FROM stock_counts c WHERE c.garage_id = $1
		AND ($2::text = '' OR strpos(lower(c.number || ' ' || c.name || ' ' || c.notes), lower($2)) > 0)
		AND ($3::text = '' OR c.status = NULLIF($3, '')::stock_count_status)`
	args := []any{garageID, filter.Query, string(filter.Status)}

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := q.Query(ctx, `SELECT `+countColumns+`,
		(SELECT count(*) FROM stock_count_lines l WHERE l.garage_id = c.garage_id AND l.stock_count_id = c.id
			AND l.counted_quantity IS NULL),
		(SELECT count(*) FROM stock_count_lines l WHERE l.garage_id = c.garage_id AND l.stock_count_id = c.id
			AND l.counted_quantity = l.system_quantity),
		(SELECT count(*) FROM stock_count_lines l WHERE l.garage_id = c.garage_id AND l.stock_count_id = c.id
			AND l.counted_quantity > l.system_quantity),
		(SELECT count(*) FROM stock_count_lines l WHERE l.garage_id = c.garage_id AND l.stock_count_id = c.id
			AND l.counted_quantity < l.system_quantity),
		COALESCE((SELECT sum(round((l.counted_quantity - l.system_quantity) * COALESCE(l.average_cost, b.average_cost, 0)))
			FROM stock_count_lines l
			LEFT JOIN stock_balance b ON b.garage_id = l.garage_id AND b.part_id = l.part_id
			WHERE l.garage_id = c.garage_id AND l.stock_count_id = c.id AND l.counted_quantity IS NOT NULL), 0)::bigint`+where+`
		ORDER BY c.created_at DESC, c.id LIMIT $4 OFFSET $5`, append(args, filter.Limit, filter.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]*domain.StockCount, 0, filter.Limit)

	for rows.Next() {
		var t domain.Tally

		var value int64

		c, err := scanCountHeader(rows, &t.Uncounted, &t.Matched, &t.Over, &t.Short, &value)
		if err != nil {
			return nil, 0, err
		}

		c.Tally, c.TotalDifferenceValue = t, value
		items = append(items, c)
	}

	return items, total, rows.Err()
}
