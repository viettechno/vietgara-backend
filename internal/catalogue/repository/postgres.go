// Package repository implements the persistence layer of the Catalogue
// module on top of the shared Postgres pool. Every query filters on
// garage_id explicitly on top of the Row-Level Security policy and skips
// soft-deleted rows. See: Database Design Section 5.
package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

const pgUniqueViolation = "23505"

const (
	unitColumns = `u.id, u.garage_id, u.name, u.abbreviation, u.is_active, u.created_by, u.updated_by, u.created_at, u.updated_at`

	categoryColumns = `c.id, c.garage_id, c.parent_id, c.name, c.is_active, c.created_by, c.updated_by, c.created_at, c.updated_at`

	partColumns = `i.id, i.garage_id, i.sku, i.name, i.base_unit_id, bu.name, bu.abbreviation, i.sale_price,
		i.category_id, COALESCE(pc.name, ''), i.brand, i.is_active, i.reorder_threshold::text,
		i.created_by, i.updated_by, i.created_at, i.updated_at`

	partFrom = ` FROM parts i
		JOIN units bu ON bu.garage_id = i.garage_id AND bu.id = i.base_unit_id
		LEFT JOIN part_categories pc ON pc.garage_id = i.garage_id AND pc.id = i.category_id`

	// partSearch filters live parts of garage $1. $2 is an ILIKE pattern over
	// SKU, name and brand ("" disables it), $3 restricts to categories (an
	// empty array disables it), $4 the exact brand ("" disables it), $5 also
	// lists deactivated parts, and $6 is an exact barcode ("" disables it).
	partSearch = partFrom + `
		WHERE i.garage_id = $1 AND i.deleted_at IS NULL AND ($5::bool OR i.is_active)
		AND ($2::text = '' OR i.sku ILIKE $2 OR i.name ILIKE $2 OR i.brand ILIKE $2
			OR EXISTS (SELECT 1 FROM part_barcodes b
			           WHERE b.garage_id = i.garage_id AND b.part_id = i.id AND lower(b.barcode) = lower($6)))
		AND (cardinality($3::uuid[]) = 0 OR i.category_id = ANY($3::uuid[]))
		AND ($4::text = '' OR lower(i.brand) = lower($4))`
)

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func containsPattern(query string) string {
	if query == "" {
		return ""
	}

	return "%" + likeEscaper.Replace(query) + "%"
}

// PostgresRepository implements usecase.Repository over the shared pgx pool.
type PostgresRepository struct {
	pool *platformdb.Pool
}

// NewPostgresRepository returns a catalogue repository backed by pool.
func NewPostgresRepository(pool *platformdb.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) q(ctx context.Context) platformdb.Querier {
	return platformdb.QuerierFromContext(ctx, r.pool)
}

// translate maps unique violations to the domain errors.
func translate(err error) error {
	var pgErr *pgconn.PgError

	if !errors.As(err, &pgErr) || pgErr.Code != pgUniqueViolation {
		return err
	}

	switch pgErr.ConstraintName {
	case "idx_parts_garage_sku_unique":
		return domain.ErrSKUTaken
	case "idx_units_garage_name_unique":
		return domain.ErrUnitNameTaken
	case "idx_part_categories_sibling_name_unique":
		return domain.ErrCategoryNameTaken
	case "idx_part_barcodes_garage_barcode_unique":
		return domain.ErrBarcodeAlreadyUsed
	}

	return err
}

func validID(id string) bool { return uuid.Validate(id) == nil }

func nullable(id string) *string {
	if id == "" {
		return nil
	}

	return &id
}

// trimNumeric renders a NUMERIC text ("240.000") without trailing zeros.
func trimNumeric(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}

	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

// ---------------------------------------------------------------------------
// Units
// ---------------------------------------------------------------------------

func scanUnit(row pgx.Row) (*domain.Unit, error) {
	var u domain.Unit

	err := row.Scan(&u.ID, &u.GarageID, &u.Name, &u.Abbreviation, &u.IsActive, &u.CreatedBy, &u.UpdatedBy, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}

	return &u, nil
}

// CreateUnit inserts u and scans back the timestamps.
func (r *PostgresRepository) CreateUnit(ctx context.Context, u *domain.Unit) error {
	row := r.q(ctx).QueryRow(ctx, `
		INSERT INTO units (id, garage_id, name, abbreviation, is_active, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at`,
		u.ID, u.GarageID, u.Name, u.Abbreviation, u.IsActive, u.CreatedBy, u.UpdatedBy)

	return translate(row.Scan(&u.CreatedAt, &u.UpdatedAt))
}

// FindUnit returns the live unit, or domain.ErrUnitNotFound.
func (r *PostgresRepository) FindUnit(ctx context.Context, garageID, unitID string) (*domain.Unit, error) {
	if !validID(unitID) {
		return nil, domain.ErrUnitNotFound
	}

	u, err := scanUnit(r.q(ctx).QueryRow(ctx,
		`SELECT `+unitColumns+` FROM units u WHERE u.garage_id = $1 AND u.id = $2 AND u.deleted_at IS NULL`,
		garageID, unitID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrUnitNotFound
	}

	return u, err
}

// UpdateUnit persists the editable fields of u.
func (r *PostgresRepository) UpdateUnit(ctx context.Context, u *domain.Unit) error {
	err := r.q(ctx).QueryRow(ctx, `
		UPDATE units SET name = $3, abbreviation = $4, is_active = $5, updated_by = $6
		WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL
		RETURNING updated_at`,
		u.GarageID, u.ID, u.Name, u.Abbreviation, u.IsActive, u.UpdatedBy).Scan(&u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrUnitNotFound
	}

	return translate(err)
}

// ListUnits returns one page of live units ordered by name.
func (r *PostgresRepository) ListUnits(ctx context.Context, garageID string, f usecase.UnitFilter) ([]*domain.Unit, int, error) {
	const where = ` FROM units u WHERE u.garage_id = $1 AND u.deleted_at IS NULL AND ($2::bool OR u.is_active)`

	var total int
	if err := r.q(ctx).QueryRow(ctx, `SELECT count(*)`+where, garageID, f.IncludeInactive).Scan(&total); err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := r.q(ctx).Query(ctx,
		`SELECT `+unitColumns+where+` ORDER BY lower(u.name), u.id LIMIT $3 OFFSET $4`,
		garageID, f.IncludeInactive, f.Limit, f.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]*domain.Unit, 0, f.Limit)

	for rows.Next() {
		u, err := scanUnit(rows)
		if err != nil {
			return nil, 0, err
		}

		out = append(out, u)
	}

	return out, total, rows.Err()
}

// SoftDeleteUnit marks the unit deleted.
func (r *PostgresRepository) SoftDeleteUnit(ctx context.Context, garageID, unitID, actorID string) error {
	return r.touch(ctx, `UPDATE units SET deleted_at = now(), updated_by = $3 WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL`,
		garageID, unitID, actorID, domain.ErrUnitNotFound)
}

// SetUnitActive deactivates or reactivates the unit.
func (r *PostgresRepository) SetUnitActive(ctx context.Context, garageID, unitID string, active bool, actorID string) error {
	return r.setActive(ctx, "units", garageID, unitID, active, actorID, domain.ErrUnitNotFound)
}

// UnitUsedByParts reports whether a live part has the unit as base or
// packaging unit.
func (r *PostgresRepository) UnitUsedByParts(ctx context.Context, garageID, unitID string) (bool, error) {
	var used bool

	err := r.q(ctx).QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM parts i WHERE i.garage_id = $1 AND i.deleted_at IS NULL AND i.base_unit_id = $2)
		    OR EXISTS (SELECT 1 FROM part_units iu JOIN parts i ON i.garage_id = iu.garage_id AND i.id = iu.part_id
		               WHERE iu.garage_id = $1 AND iu.unit_id = $2 AND i.deleted_at IS NULL)`,
		garageID, unitID).Scan(&used)

	return used, err
}

// ---------------------------------------------------------------------------
// Categories
// ---------------------------------------------------------------------------

func scanCategory(row pgx.Row) (*domain.Category, error) {
	var c domain.Category

	err := row.Scan(&c.ID, &c.GarageID, &c.ParentID, &c.Name, &c.IsActive, &c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}

	return &c, nil
}

// CreateCategory inserts c and scans back the timestamps.
func (r *PostgresRepository) CreateCategory(ctx context.Context, c *domain.Category) error {
	row := r.q(ctx).QueryRow(ctx, `
		INSERT INTO part_categories (id, garage_id, parent_id, name, is_active, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at`,
		c.ID, c.GarageID, c.ParentID, c.Name, c.IsActive, c.CreatedBy, c.UpdatedBy)

	return translate(row.Scan(&c.CreatedAt, &c.UpdatedAt))
}

// FindCategory returns the live category, or domain.ErrCategoryNotFound.
func (r *PostgresRepository) FindCategory(ctx context.Context, garageID, categoryID string) (*domain.Category, error) {
	if !validID(categoryID) {
		return nil, domain.ErrCategoryNotFound
	}

	c, err := scanCategory(r.q(ctx).QueryRow(ctx,
		`SELECT `+categoryColumns+` FROM part_categories c WHERE c.garage_id = $1 AND c.id = $2 AND c.deleted_at IS NULL`,
		garageID, categoryID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrCategoryNotFound
	}

	return c, err
}

// UpdateCategory persists name and parent of c.
func (r *PostgresRepository) UpdateCategory(ctx context.Context, c *domain.Category) error {
	err := r.q(ctx).QueryRow(ctx, `
		UPDATE part_categories SET name = $3, parent_id = $4, is_active = $5, updated_by = $6
		WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL
		RETURNING updated_at`,
		c.GarageID, c.ID, c.Name, c.ParentID, c.IsActive, c.UpdatedBy).Scan(&c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrCategoryNotFound
	}

	return translate(err)
}

// ListCategories returns the whole tree as a flat list ordered by name.
func (r *PostgresRepository) ListCategories(ctx context.Context, garageID string, includeInactive bool) ([]*domain.Category, error) {
	rows, err := r.q(ctx).Query(ctx, `
		SELECT `+categoryColumns+` FROM part_categories c
		WHERE c.garage_id = $1 AND c.deleted_at IS NULL AND ($2::bool OR c.is_active)
		ORDER BY lower(c.name), c.id`, garageID, includeInactive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.Category

	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, c)
	}

	return out, rows.Err()
}

// SoftDeleteCategory marks the category deleted.
func (r *PostgresRepository) SoftDeleteCategory(ctx context.Context, garageID, categoryID, actorID string) error {
	return r.touch(ctx, `UPDATE part_categories SET deleted_at = now(), updated_by = $3 WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL`,
		garageID, categoryID, actorID, domain.ErrCategoryNotFound)
}

// SetCategoryActive deactivates or reactivates the category.
func (r *PostgresRepository) SetCategoryActive(ctx context.Context, garageID, categoryID string, active bool, actorID string) error {
	return r.setActive(ctx, "part_categories", garageID, categoryID, active, actorID, domain.ErrCategoryNotFound)
}

// CategoryHasParts reports whether a live part is in the category.
func (r *PostgresRepository) CategoryHasParts(ctx context.Context, garageID, categoryID string) (bool, error) {
	var has bool

	err := r.q(ctx).QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM parts WHERE garage_id = $1 AND category_id = $2 AND deleted_at IS NULL)`,
		garageID, categoryID).Scan(&has)

	return has, err
}

// ---------------------------------------------------------------------------
// Parts
// ---------------------------------------------------------------------------

func scanPart(row pgx.Row) (*domain.Part, error) {
	var p domain.Part

	err := row.Scan(&p.ID, &p.GarageID, &p.SKU, &p.Name, &p.BaseUnitID, &p.BaseUnitName, &p.BaseUnitAbbreviation, &p.SalePrice,
		&p.CategoryID, &p.CategoryName, &p.Brand, &p.IsActive, &p.ReorderThreshold,
		&p.CreatedBy, &p.UpdatedBy, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}

	p.ReorderThreshold = trimNumeric(p.ReorderThreshold)

	return &p, nil
}

// CreatePart inserts p and scans back the timestamps.
func (r *PostgresRepository) CreatePart(ctx context.Context, p *domain.Part) error {
	row := r.q(ctx).QueryRow(ctx, `
		INSERT INTO parts (id, garage_id, sku, name, base_unit_id, sale_price, category_id, brand, is_active,
		                   reorder_threshold, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::numeric, $11, $12)
		RETURNING created_at, updated_at`,
		p.ID, p.GarageID, p.SKU, p.Name, p.BaseUnitID, p.SalePrice, p.CategoryID, p.Brand, p.IsActive,
		p.ReorderThreshold, p.CreatedBy, p.UpdatedBy)

	return translate(row.Scan(&p.CreatedAt, &p.UpdatedAt))
}

// CreateParts inserts parts with their packaging units and barcodes, one
// statement per table. A SKU or barcode already taken returns the matching
// domain error and the caller's transaction is abandoned.
func (r *PostgresRepository) CreateParts(
	ctx context.Context, garageID, actorID string, parts []*domain.Part,
	packaging []usecase.PackagingRow, barcodes []usecase.BarcodeRow,
) error {
	if len(parts) == 0 {
		return nil
	}

	q := r.q(ctx)
	ids, skus, names := make([]string, len(parts)), make([]string, len(parts)), make([]string, len(parts))
	units, categories, brands := make([]string, len(parts)), make([]*string, len(parts)), make([]string, len(parts))

	for i, p := range parts {
		ids[i], skus[i], names[i], units[i], categories[i], brands[i] = p.ID, p.SKU, p.Name, p.BaseUnitID, p.CategoryID, p.Brand
	}

	if _, err := q.Exec(ctx, `
		INSERT INTO parts (id, garage_id, sku, name, base_unit_id, category_id, brand, created_by, updated_by)
		SELECT t.id, $1, t.sku, t.name, t.base_unit_id, t.category_id, t.brand, $2, $2
		FROM unnest($3::uuid[], $4::text[], $5::text[], $6::uuid[], $7::uuid[], $8::text[])
		     AS t(id, sku, name, base_unit_id, category_id, brand)`,
		garageID, nullable(actorID), ids, skus, names, units, categories, brands); err != nil {
		return translate(err)
	}

	if len(packaging) > 0 {
		partIDs, unitIDs, factors := make([]string, len(packaging)), make([]string, len(packaging)), make([]string, len(packaging))
		prices := make([]*int64, len(packaging))

		for i, row := range packaging {
			partIDs[i], unitIDs[i], factors[i], prices[i] = row.PartID, row.Unit.UnitID, row.Unit.Factor, row.Unit.SalePrice
		}

		if _, err := q.Exec(ctx, `
			INSERT INTO part_units (garage_id, part_id, unit_id, factor, sale_price, created_by)
			SELECT $1, t.part_id, t.unit_id, t.factor::numeric, t.sale_price, $2
			FROM unnest($3::uuid[], $4::uuid[], $5::text[], $6::bigint[]) AS t(part_id, unit_id, factor, sale_price)`,
			garageID, nullable(actorID), partIDs, unitIDs, factors, prices); err != nil {
			return err
		}
	}

	if len(barcodes) > 0 {
		partIDs, codes, unitIDs := make([]string, len(barcodes)), make([]string, len(barcodes)), make([]*string, len(barcodes))

		for i, row := range barcodes {
			partIDs[i], codes[i], unitIDs[i] = row.PartID, row.Barcode.Barcode, row.Barcode.UnitID
		}

		if _, err := q.Exec(ctx, `
			INSERT INTO part_barcodes (garage_id, part_id, unit_id, barcode, created_by)
			SELECT $1, t.part_id, t.unit_id, t.barcode, $2
			FROM unnest($3::uuid[], $4::uuid[], $5::text[]) AS t(part_id, unit_id, barcode)`,
			garageID, nullable(actorID), partIDs, unitIDs, codes); err != nil {
			return translate(err)
		}
	}

	return nil
}

// FindPart returns the part with its packaging units and image links.
func (r *PostgresRepository) FindPart(ctx context.Context, garageID, partID string) (*domain.Part, error) {
	if !validID(partID) {
		return nil, domain.ErrPartNotFound
	}

	p, err := scanPart(r.q(ctx).QueryRow(ctx,
		`SELECT `+partColumns+partFrom+` WHERE i.garage_id = $1 AND i.id = $2 AND i.deleted_at IS NULL`,
		garageID, partID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPartNotFound
	}

	if err != nil {
		return nil, err
	}

	if err := r.fillChildren(ctx, garageID, []*domain.Part{p}); err != nil {
		return nil, err
	}

	return p, nil
}

// FindPartsByIDs returns the live parts with these ids; unknown ones are
// skipped.
func (r *PostgresRepository) FindPartsByIDs(ctx context.Context, garageID string, ids []string) ([]*domain.Part, error) {
	valid := make([]string, 0, len(ids))

	for _, id := range ids {
		if validID(id) {
			valid = append(valid, id)
		}
	}

	if len(valid) == 0 {
		return nil, nil
	}

	rows, err := r.q(ctx).Query(ctx,
		`SELECT `+partColumns+partFrom+` WHERE i.garage_id = $1 AND i.id = ANY($2::uuid[]) AND i.deleted_at IS NULL`,
		garageID, valid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	parts, err := collectParts(rows)
	if err != nil {
		return nil, err
	}

	return parts, r.fillChildren(ctx, garageID, parts)
}

// FindPartsBySKUs returns the parts with these SKUs, matched
// case-insensitively, deleted ones excluded.
func (r *PostgresRepository) FindPartsBySKUs(ctx context.Context, garageID string, skus []string) ([]*domain.Part, error) {
	valid := make([]string, 0, len(skus))

	for _, sku := range skus {
		if sku = strings.TrimSpace(sku); sku != "" {
			valid = append(valid, strings.ToLower(sku))
		}
	}

	if len(valid) == 0 {
		return nil, nil
	}

	rows, err := r.q(ctx).Query(ctx,
		`SELECT `+partColumns+partFrom+` WHERE i.garage_id = $1 AND lower(i.sku) = ANY($2::text[]) AND i.deleted_at IS NULL`,
		garageID, valid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	parts, err := collectParts(rows)
	if err != nil {
		return nil, err
	}

	return parts, r.fillChildren(ctx, garageID, parts)
}

func collectParts(rows pgx.Rows) ([]*domain.Part, error) {
	var out []*domain.Part

	for rows.Next() {
		p, err := scanPart(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, p)
	}

	return out, rows.Err()
}

// UpdatePart persists the editable fields of p.
func (r *PostgresRepository) UpdatePart(ctx context.Context, p *domain.Part) error {
	err := r.q(ctx).QueryRow(ctx, `
		UPDATE parts SET sku = $3, name = $4, base_unit_id = $5, sale_price = $6, category_id = $7, brand = $8,
		                 is_active = $9, reorder_threshold = $10::numeric, updated_by = $11
		WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL
		RETURNING updated_at`,
		p.GarageID, p.ID, p.SKU, p.Name, p.BaseUnitID, p.SalePrice, p.CategoryID, p.Brand,
		p.IsActive, p.ReorderThreshold, p.UpdatedBy).Scan(&p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrPartNotFound
	}

	return translate(err)
}

// ListParts returns one page of live parts ordered by name.
func (r *PostgresRepository) ListParts(ctx context.Context, garageID string, f usecase.PartFilter) ([]*domain.Part, int, error) {
	categories := f.CategoryIDs
	if categories == nil {
		categories = []string{}
	}

	args := []any{garageID, containsPattern(f.Query), categories, f.Brand, f.IncludeInactive, strings.TrimSpace(f.Query)}

	var total int
	if err := r.q(ctx).QueryRow(ctx, `SELECT count(*)`+partSearch, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := r.q(ctx).Query(ctx,
		`SELECT `+partColumns+partSearch+` ORDER BY lower(i.name), i.id LIMIT $7 OFFSET $8`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	parts, err := collectParts(rows)
	if err != nil {
		return nil, 0, err
	}

	rows.Close()

	return parts, total, r.fillChildren(ctx, garageID, parts)
}

// SoftDeletePart marks the part deleted and frees its barcodes: the delete
// happens in the same transaction, so no other part can claim a code while
// the part still looks alive.
func (r *PostgresRepository) SoftDeletePart(ctx context.Context, garageID, partID, actorID string) error {
	if err := r.touch(ctx,
		`UPDATE parts SET deleted_at = now(), updated_by = $3 WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL`,
		garageID, partID, actorID, domain.ErrPartNotFound); err != nil {
		return err
	}

	_, err := r.q(ctx).Exec(ctx, `DELETE FROM part_barcodes WHERE garage_id = $1 AND part_id = $2`, garageID, partID)

	return err
}

// ReplaceBarcodes replaces the part's barcode table.
func (r *PostgresRepository) ReplaceBarcodes(
	ctx context.Context, garageID, partID, actorID string, barcodes []domain.PartBarcode,
) error {
	q := r.q(ctx)

	if _, err := q.Exec(ctx, `DELETE FROM part_barcodes WHERE garage_id = $1 AND part_id = $2`, garageID, partID); err != nil {
		return err
	}

	for _, b := range barcodes {
		if _, err := q.Exec(ctx, `
			INSERT INTO part_barcodes (garage_id, part_id, unit_id, barcode, created_by)
			VALUES ($1, $2, $3, $4, $5)`,
			garageID, partID, b.UnitID, b.Barcode, nullable(actorID)); err != nil {
			return translate(err)
		}
	}

	_, err := q.Exec(ctx, `UPDATE parts SET updated_by = $3 WHERE garage_id = $1 AND id = $2`, garageID, partID, nullable(actorID))

	return err
}

// FindBarcode returns the part and the barcode row matching code
// case-insensitively, or domain.ErrBarcodeNotFound.
func (r *PostgresRepository) FindBarcode(ctx context.Context, garageID, barcode string) (*domain.Part, *domain.PartBarcode, error) {
	var (
		partID string
		code   domain.PartBarcode
	)

	err := r.q(ctx).QueryRow(ctx, `
		SELECT b.part_id, b.barcode, b.unit_id, COALESCE(u.name, ''), COALESCE(u.abbreviation, '')
		FROM part_barcodes b
		JOIN parts i ON i.garage_id = b.garage_id AND i.id = b.part_id AND i.deleted_at IS NULL
		LEFT JOIN units u ON u.garage_id = b.garage_id AND u.id = b.unit_id
		WHERE b.garage_id = $1 AND lower(b.barcode) = lower($2)`,
		garageID, barcode).Scan(&partID, &code.Barcode, &code.UnitID, &code.UnitName, &code.UnitAbbreviation)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, domain.ErrBarcodeNotFound
	}

	if err != nil {
		return nil, nil, err
	}

	p, err := r.FindPart(ctx, garageID, partID)
	if err != nil {
		return nil, nil, err
	}

	return p, &code, nil
}

// BarcodesByCode returns the part id per lower-case code for the codes that
// exist in the garage; unknown codes are absent.
func (r *PostgresRepository) BarcodesByCode(ctx context.Context, garageID string, codes []string) (map[string]string, error) {
	out := make(map[string]string, len(codes))
	if len(codes) == 0 {
		return out, nil
	}

	lowered := make([]string, 0, len(codes))
	for _, code := range codes {
		lowered = append(lowered, strings.ToLower(strings.TrimSpace(code)))
	}

	rows, err := r.q(ctx).Query(ctx, `
		SELECT lower(b.barcode), b.part_id
		FROM part_barcodes b
		JOIN parts i ON i.garage_id = b.garage_id AND i.id = b.part_id AND i.deleted_at IS NULL
		WHERE b.garage_id = $1 AND lower(b.barcode) = ANY($2::text[])`,
		garageID, lowered)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var code, partID string
		if err := rows.Scan(&code, &partID); err != nil {
			return nil, err
		}

		out[code] = partID
	}

	return out, rows.Err()
}

// SetPartActive deactivates or reactivates the part.
func (r *PostgresRepository) SetPartActive(ctx context.Context, garageID, partID string, active bool, actorID string) error {
	return r.setActive(ctx, "parts", garageID, partID, active, actorID, domain.ErrPartNotFound)
}

// fillChildren loads the packaging units and image links of parts.
func (r *PostgresRepository) fillChildren(ctx context.Context, garageID string, parts []*domain.Part) error {
	if len(parts) == 0 {
		return nil
	}

	byID := make(map[string]*domain.Part, len(parts))
	ids := make([]string, 0, len(parts))

	for _, p := range parts {
		byID[p.ID] = p
		ids = append(ids, p.ID)
	}

	rows, err := r.q(ctx).Query(ctx, `
		SELECT iu.part_id, iu.unit_id, u.name, u.abbreviation, iu.factor::text, iu.sale_price
		FROM part_units iu JOIN units u ON u.garage_id = iu.garage_id AND u.id = iu.unit_id
		WHERE iu.garage_id = $1 AND iu.part_id = ANY($2::uuid[])
		ORDER BY iu.factor, lower(u.name)`, garageID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			partID string
			pu     domain.PackagingUnit
		)

		if err := rows.Scan(&partID, &pu.UnitID, &pu.UnitName, &pu.UnitAbbreviation, &pu.Factor, &pu.SalePrice); err != nil {
			return err
		}

		pu.Factor = trimNumeric(pu.Factor)

		p := byID[partID]

		if pu.SalePrice != nil {
			pu.EffectivePrice = *pu.SalePrice
		} else {
			pu.EffectivePrice = domain.DerivedPrice(p.SalePrice, pu.Factor)
		}

		p.PackagingUnits = append(p.PackagingUnits, pu)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	imgs, err := r.q(ctx).Query(ctx, `
		SELECT part_id, file_id, position FROM part_images
		WHERE garage_id = $1 AND part_id = ANY($2::uuid[]) ORDER BY position, created_at, file_id`, garageID, ids)
	if err != nil {
		return err
	}
	defer imgs.Close()

	for imgs.Next() {
		var (
			partID string
			img    domain.Image
		)

		if err := imgs.Scan(&partID, &img.FileID, &img.Position); err != nil {
			return err
		}

		byID[partID].Images = append(byID[partID].Images, img)
	}

	if err := imgs.Err(); err != nil {
		return err
	}

	return r.fillBarcodes(ctx, garageID, ids, byID)
}

// fillBarcodes attaches each part's barcodes, ordered by code.
func (r *PostgresRepository) fillBarcodes(ctx context.Context, garageID string, ids []string, byID map[string]*domain.Part) error {
	codes, err := r.q(ctx).Query(ctx, `
		SELECT b.part_id, b.barcode, b.unit_id, COALESCE(u.name, ''), COALESCE(u.abbreviation, '')
		FROM part_barcodes b
		LEFT JOIN units u ON u.garage_id = b.garage_id AND u.id = b.unit_id
		WHERE b.garage_id = $1 AND b.part_id = ANY($2::uuid[])
		ORDER BY lower(b.barcode), b.barcode`, garageID, ids)
	if err != nil {
		return err
	}
	defer codes.Close()

	for codes.Next() {
		var (
			partID string
			code   domain.PartBarcode
		)

		if err := codes.Scan(&partID, &code.Barcode, &code.UnitID, &code.UnitName, &code.UnitAbbreviation); err != nil {
			return err
		}

		byID[partID].Barcodes = append(byID[partID].Barcodes, code)
	}

	return codes.Err()
}

// ReplacePackaging replaces the part's packaging table.
func (r *PostgresRepository) ReplacePackaging(ctx context.Context, garageID, partID, actorID string, units []domain.PackagingUnit) error {
	q := r.q(ctx)

	// Only the units that left the table are deleted: a barcode may point at
	// a unit that stays, and the foreign key would refuse deleting it.
	kept := make([]string, 0, len(units))
	for _, u := range units {
		kept = append(kept, u.UnitID)
	}

	if _, err := q.Exec(ctx,
		`DELETE FROM part_units WHERE garage_id = $1 AND part_id = $2 AND NOT (unit_id = ANY($3::uuid[]))`,
		garageID, partID, kept); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgRestrictViolation && pgErr.ConstraintName == "job_parts_garage_part_unit_fkey" {
			return domain.ErrPackagingUnitUsedByJob
		}

		return err
	}

	for _, u := range units {
		if _, err := q.Exec(ctx, `
			INSERT INTO part_units (garage_id, part_id, unit_id, factor, sale_price, created_by)
			VALUES ($1, $2, $3, $4::numeric, $5, $6)
			ON CONFLICT (garage_id, part_id, unit_id) DO UPDATE SET factor = EXCLUDED.factor, sale_price = EXCLUDED.sale_price`,
			garageID, partID, u.UnitID, u.Factor, u.SalePrice, nullable(actorID)); err != nil {
			return err
		}
	}

	_, err := q.Exec(ctx, `UPDATE parts SET updated_by = $3 WHERE garage_id = $1 AND id = $2`, garageID, partID, nullable(actorID))

	return err
}

// ListImages returns the image links of a part in display order.
func (r *PostgresRepository) ListImages(ctx context.Context, garageID, partID string) ([]usecase.ImageRef, error) {
	rows, err := r.q(ctx).Query(ctx,
		`SELECT file_id, position FROM part_images WHERE garage_id = $1 AND part_id = $2 ORDER BY position, created_at, file_id`,
		garageID, partID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []usecase.ImageRef

	for rows.Next() {
		var ref usecase.ImageRef
		if err := rows.Scan(&ref.FileID, &ref.Position); err != nil {
			return nil, err
		}

		out = append(out, ref)
	}

	return out, rows.Err()
}

// AddImage links a file to the part after its existing images.
func (r *PostgresRepository) AddImage(ctx context.Context, garageID, partID, fileID, actorID string) error {
	_, err := r.q(ctx).Exec(ctx, `
		INSERT INTO part_images (garage_id, part_id, file_id, position, created_by)
		VALUES ($1, $2, $3,
			(SELECT COALESCE(max(position) + 1, 0) FROM part_images WHERE garage_id = $1 AND part_id = $2), $4)`,
		garageID, partID, fileID, nullable(actorID))

	return err
}

// RemoveImage unlinks a file from the part, or returns
// domain.ErrImageNotFound.
func (r *PostgresRepository) RemoveImage(ctx context.Context, garageID, partID, fileID string) error {
	if !validID(partID) || !validID(fileID) {
		return domain.ErrImageNotFound
	}

	tag, err := r.q(ctx).Exec(ctx,
		`DELETE FROM part_images WHERE garage_id = $1 AND part_id = $2 AND file_id = $3`, garageID, partID, fileID)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrImageNotFound
	}

	return nil
}

// FileAttached reports whether any part uses the file.
func (r *PostgresRepository) FileAttached(ctx context.Context, garageID, fileID string) (bool, error) {
	if !validID(fileID) {
		return false, nil
	}

	var used bool

	err := r.q(ctx).QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM part_images WHERE garage_id = $1 AND file_id = $2)`, garageID, fileID).Scan(&used)

	return used, err
}

// ---------------------------------------------------------------------------

func (r *PostgresRepository) touch(ctx context.Context, sql, garageID, id, actorID string, notFound error) error {
	if !validID(id) {
		return notFound
	}

	tag, err := r.q(ctx).Exec(ctx, sql, garageID, id, nullable(actorID))
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return notFound
	}

	return nil
}

func (r *PostgresRepository) setActive(ctx context.Context, table, garageID, id string, active bool, actorID string, notFound error) error {
	if !validID(id) {
		return notFound
	}

	tag, err := r.q(ctx).Exec(ctx,
		`UPDATE `+table+` SET is_active = $3, updated_by = $4 WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL`,
		garageID, id, active, nullable(actorID))
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return notFound
	}

	return nil
}
