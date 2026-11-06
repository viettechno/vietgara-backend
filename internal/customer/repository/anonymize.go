package repository

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// Anonymize erases the personal data of a customer in one go (FR-CUS-05,
// PD-06): the name, phone, e-mail and address of the customer, the plate, VIN
// and engine number of every vehicle (also deleted ones), the personal fields
// of every tax profile, and the link to a user. The consent log stays. It
// returns what it changed, or domain.ErrCustomerNotFound for a customer that
// is not live or was anonymized already.
func (r *PostgresCustomerRepository) Anonymize(ctx context.Context, garageID, customerID, actorID string) (domain.AnonymizedCounts, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	var counts domain.AnonymizedCounts

	if !isUUID(customerID) {
		return counts, domain.ErrCustomerNotFound
	}

	tag, err := q.Exec(ctx, `
		UPDATE customers SET full_name = $3, phone = NULL, email = NULL, address = NULL, email_opt_out_at = NULL,
			is_active = false, anonymized_at = now(), updated_by = NULLIF($4, '')::uuid
		WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL AND anonymized_at IS NULL`,
		garageID, customerID, domain.AnonymizedName, actorID)
	if err != nil {
		return counts, err
	}

	if counts.Customers = int(tag.RowsAffected()); counts.Customers == 0 {
		return counts, domain.ErrCustomerNotFound
	}

	tag, err = q.Exec(ctx, `
		UPDATE vehicles SET license_plate = $3 || upper(substr(replace(id::text, '-', ''), 1, 8)), vin = NULL, engine_number = NULL,
			is_active = false, updated_by = NULLIF($4, '')::uuid
		WHERE garage_id = $1 AND customer_id = $2`,
		garageID, customerID, domain.AnonymizedPlatePrefix, actorID)
	if err != nil {
		return counts, err
	}

	counts.Vehicles = int(tag.RowsAffected())

	tag, err = q.Exec(ctx, `
		UPDATE customer_tax_profiles SET legal_name = $3, tax_id = NULL, address = NULL, invoice_email = NULL,
			is_default = false, is_active = false, updated_by = NULLIF($4, '')::uuid
		WHERE garage_id = $1 AND customer_id = $2`,
		garageID, customerID, domain.AnonymizedName, actorID)
	if err != nil {
		return counts, err
	}

	counts.TaxProfiles = int(tag.RowsAffected())

	_, err = q.Exec(ctx, `
		UPDATE customer_user_links SET status = 'unlinked', ended_at = now(), updated_by = NULLIF($3, '')::uuid
		WHERE garage_id = $1 AND customer_id = $2 AND status IN ('requested', 'linked')`,
		garageID, customerID, actorID)

	return counts, err
}
