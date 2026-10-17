package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

const issueColumns = `i.id, i.garage_id, i.number, i.repair_order_id, i.repair_order_number, i.status::text, i.posted_at, i.posted_by,
	i.created_by, i.updated_by, i.created_at, i.updated_at`

// CreateStockIssue inserts the issue and its lines unless the repair order
// already has one.
func (r *PostgresRepository) CreateStockIssue(ctx context.Context, issue *domain.StockIssue) (bool, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		INSERT INTO stock_issues (id, garage_id, number, repair_order_id, repair_order_number, status, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6::stock_issue_status, $7, $7)
		ON CONFLICT (garage_id, repair_order_id) DO NOTHING
		RETURNING created_at, updated_at`,
		issue.ID, issue.GarageID, issue.Number, issue.RepairOrderID, issue.RepairOrderNumber, string(issue.Status), issue.CreatedBy,
	).Scan(&issue.CreatedAt, &issue.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	for _, l := range issue.Lines {
		_, err := q.Exec(ctx, `
			INSERT INTO stock_issue_lines (id, garage_id, stock_issue_id, repair_order_line_id, part_id, unit_id,
			                               entered_quantity, quantity)
			VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8::numeric)`,
			l.ID, issue.GarageID, issue.ID, l.RepairOrderLineID, l.PartID, l.UnitID, l.EnteredQuantity, l.Quantity)
		if err != nil {
			return false, err
		}
	}

	return true, nil
}

// FindStockIssue returns the issue with its lines.
func (r *PostgresRepository) FindStockIssue(ctx context.Context, garageID, issueID string, forUpdate bool) (*domain.StockIssue, error) {
	return r.findIssue(ctx, garageID, "i.id", issueID, forUpdate)
}

// FindStockIssueByRepairOrder returns the repair order's issue with its lines.
func (r *PostgresRepository) FindStockIssueByRepairOrder(ctx context.Context, garageID, repairOrderID string, forUpdate bool) (*domain.StockIssue, error) {
	return r.findIssue(ctx, garageID, "i.repair_order_id", repairOrderID, forUpdate)
}

func (r *PostgresRepository) findIssue(ctx context.Context, garageID, column, value string, forUpdate bool) (*domain.StockIssue, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}

	var (
		issue  domain.StockIssue
		status string
	)

	err := q.QueryRow(ctx, `SELECT `+issueColumns+` FROM stock_issues i WHERE i.garage_id = $1 AND `+column+` = $2::uuid`+lock,
		garageID, value).Scan(&issue.ID, &issue.GarageID, &issue.Number, &issue.RepairOrderID, &issue.RepairOrderNumber, &status,
		&issue.PostedAt, &issue.PostedBy, &issue.CreatedBy, &issue.UpdatedBy, &issue.CreatedAt, &issue.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrStockIssueNotFound
		}

		return nil, err
	}

	issue.Status = domain.StockIssueStatus(status)

	rows, err := q.Query(ctx, `
		SELECT id, repair_order_line_id, part_id, unit_id, trim_scale(entered_quantity)::text, trim_scale(quantity)::text,
		       trim_scale(unit_cost)::text
		FROM stock_issue_lines WHERE garage_id = $1 AND stock_issue_id = $2 ORDER BY created_at, id`, garageID, issue.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var l domain.StockIssueLine
		if err := rows.Scan(&l.ID, &l.RepairOrderLineID, &l.PartID, &l.UnitID, &l.EnteredQuantity, &l.Quantity, &l.UnitCost); err != nil {
			return nil, err
		}

		issue.Lines = append(issue.Lines, l)
	}

	return &issue, rows.Err()
}

// SaveStockIssue persists the status, posting and line costs.
func (r *PostgresRepository) SaveStockIssue(ctx context.Context, issue *domain.StockIssue) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		UPDATE stock_issues SET status = $3::stock_issue_status, posted_at = $4, posted_by = $5, updated_by = $6
		WHERE garage_id = $1 AND id = $2
		RETURNING updated_at`,
		issue.GarageID, issue.ID, string(issue.Status), issue.PostedAt, issue.PostedBy, issue.UpdatedBy,
	).Scan(&issue.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrStockIssueNotFound
		}

		return err
	}

	for _, l := range issue.Lines {
		if _, err := q.Exec(ctx, `UPDATE stock_issue_lines SET unit_cost = $3::numeric WHERE garage_id = $1 AND id = $2`,
			issue.GarageID, l.ID, l.UnitCost); err != nil {
			return err
		}
	}

	return nil
}
