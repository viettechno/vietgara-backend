package repository

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// PartSuppliers returns the usual suppliers of a part, the default first.
func (r *PostgresRepository) PartSuppliers(ctx context.Context, garageID, partID string) ([]domain.PartSupplier, error) {
	rows, err := platformdb.QuerierFromContext(ctx, r.pool).Query(ctx, `
		SELECT supplier_id, supplier_sku, trim_scale(last_unit_cost)::text, is_default
		FROM part_suppliers WHERE garage_id = $1 AND part_id = $2
		ORDER BY is_default DESC, created_at, supplier_id`, garageID, partID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []domain.PartSupplier{}

	for rows.Next() {
		var s domain.PartSupplier
		if err := rows.Scan(&s.SupplierID, &s.SupplierSKU, &s.LastUnitCost, &s.IsDefault); err != nil {
			return nil, err
		}

		list = append(list, s)
	}

	return list, rows.Err()
}

// ReplacePartSuppliers replaces the part's list as a whole, keeping the last
// cost of suppliers that stay on it.
func (r *PostgresRepository) ReplacePartSuppliers(ctx context.Context, garageID, partID string, list []domain.PartSupplier) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	kept, err := r.PartSuppliers(ctx, garageID, partID)
	if err != nil {
		return err
	}

	lastCost := make(map[string]*string, len(kept))
	for _, s := range kept {
		lastCost[s.SupplierID] = s.LastUnitCost
	}

	if _, err := q.Exec(ctx, `DELETE FROM part_suppliers WHERE garage_id = $1 AND part_id = $2`, garageID, partID); err != nil {
		return err
	}

	for _, s := range list {
		if _, err := q.Exec(ctx, `
			INSERT INTO part_suppliers (garage_id, part_id, supplier_id, supplier_sku, last_unit_cost, is_default)
			VALUES ($1, $2, $3, $4, $5::numeric, $6)`,
			garageID, partID, s.SupplierID, s.SupplierSKU, lastCost[s.SupplierID], s.IsDefault); err != nil {
			return err
		}
	}

	return nil
}

// DefaultSuppliers returns the default supplier id per part.
func (r *PostgresRepository) DefaultSuppliers(ctx context.Context, garageID string, partIDs []string) (map[string]string, error) {
	if partIDs == nil {
		partIDs = []string{}
	}

	rows, err := platformdb.QuerierFromContext(ctx, r.pool).Query(ctx, `
		SELECT part_id, supplier_id FROM part_suppliers
		WHERE garage_id = $1 AND is_default AND part_id = ANY($2::uuid[])`, garageID, partIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}

	for rows.Next() {
		var partID, supplierID string
		if err := rows.Scan(&partID, &supplierID); err != nil {
			return nil, err
		}

		out[partID] = supplierID
	}

	return out, rows.Err()
}

// RecordLastCost sets the last cost paid for the part and supplier, adding
// the supplier to the part's list when needed.
func (r *PostgresRepository) RecordLastCost(ctx context.Context, garageID, partID, supplierID, unitCost string) error {
	_, err := platformdb.QuerierFromContext(ctx, r.pool).Exec(ctx, `
		INSERT INTO part_suppliers (garage_id, part_id, supplier_id, last_unit_cost)
		VALUES ($1, $2, $3, $4::numeric)
		ON CONFLICT (garage_id, part_id, supplier_id) DO UPDATE SET last_unit_cost = $4::numeric`,
		garageID, partID, supplierID, unitCost)

	return err
}

// SupplierReferenced reports whether documents still refer to the supplier,
// for the Supplier module's delete-or-deactivate rule (FR-CAT-07).
func (r *PostgresRepository) SupplierReferenced(ctx context.Context, garageID, supplierID string) (bool, error) {
	var exists bool

	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM goods_receipt_lines WHERE garage_id = $1 AND supplier_id = $2)
		    OR EXISTS (SELECT 1 FROM goods_receipts WHERE garage_id = $1 AND default_supplier_id = $2)
		    OR EXISTS (SELECT 1 FROM part_suppliers WHERE garage_id = $1 AND supplier_id = $2)
		    OR EXISTS (SELECT 1 FROM stock_transactions WHERE garage_id = $1 AND supplier_id = $2)
		    OR EXISTS (SELECT 1 FROM supplier_returns WHERE garage_id = $1 AND supplier_id = $2)`,
		garageID, supplierID).Scan(&exists)

	return exists, err
}
