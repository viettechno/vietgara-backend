//go:build integration

package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	customerDomain "github.com/viettechno/vietgara-backend/internal/customer/domain"
	customerRepository "github.com/viettechno/vietgara-backend/internal/customer/repository"
	"github.com/viettechno/vietgara-backend/internal/datarequest/domain"
	"github.com/viettechno/vietgara-backend/internal/datarequest/repository"
	"github.com/viettechno/vietgara-backend/internal/datarequest/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

type world struct {
	t         *testing.T
	pg        *pgtest.Postgres
	pool      *platformdb.Pool
	txm       *platformdb.TxManager
	requests  *repository.PostgresRepository
	customers *customerRepository.PostgresCustomerRepository
	owner     string
	garage    string
	other     string
	customer  string
	vehicle   string
}

func newWorld(t *testing.T) *world {
	t.Helper()

	pg := pgtest.StartMigrated(t)
	w := &world{
		t: t, pg: pg, owner: uuid.NewString(), garage: uuid.NewString(), other: uuid.NewString(), customer: uuid.NewString(), vehicle: uuid.NewString(),
	}

	w.exec(`INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@x.test', 'Owner')`, w.owner)
	w.exec(`INSERT INTO garages (id, user_id, name) VALUES ($1, $3, 'Gara A'), ($2, $3, 'Gara B')`, w.garage, w.other, w.owner)

	w.pool = pg.AppPool(t)
	w.txm = platformdb.NewTxManager(w.pool)
	w.requests = repository.NewPostgresRepository(w.pool)
	w.customers = customerRepository.NewPostgresCustomerRepository(w.pool)

	return w
}

func (w *world) exec(query string, args ...any) {
	w.t.Helper()

	_, err := w.pg.Admin.ExecContext(w.t.Context(), query, args...)
	require.NoError(w.t, err)
}

func (w *world) in(garage string, fn func(ctx context.Context)) {
	w.t.Helper()

	require.NoError(w.t, w.txm.WithTransaction(w.t.Context(), platformdb.Scope{UserID: w.owner, GarageID: garage}, func(ctx context.Context) error {
		fn(ctx)

		return nil
	}))
}

// seed adds a customer with an e-mail, a phone, a vehicle, a tax profile, a link, a notification log row and a receivable.
func (w *world) seed(garage, customer, email string) (vehicle string) {
	w.t.Helper()

	vehicle = uuid.NewString()

	w.exec(`INSERT INTO customers (id, garage_id, full_name, phone, email, address) VALUES ($1, $2, 'An Nguyen', $4, $3, '1 Le Loi')`,
		customer, garage, email, "09"+customer[:8])
	w.exec(`INSERT INTO vehicles (id, garage_id, customer_id, license_plate, vin, engine_number) VALUES ($1, $2, $3, $4, 'VIN123', 'ENG1')`,
		vehicle, garage, customer, "51A-"+customer[:5])
	w.exec(`INSERT INTO customer_tax_profiles (garage_id, customer_id, kind, legal_name, tax_id, address, invoice_email, is_default)
		VALUES ($1, $2, 'company', 'Cty An', '0301234567', '1 Le Loi', 'ketoan@an.test', true)`, garage, customer)
	w.exec(`INSERT INTO customer_user_links (garage_id, customer_id, user_id, status, channel, linked_at) VALUES ($1, $2, $3, 'linked', 'otp', now())`,
		garage, customer, w.owner)
	w.exec(`INSERT INTO notification_log (id, channel, template, recipient, status, garage_id, customer_id, type)
		VALUES ($1, 'email', 'payment_received', $2, 'sent', $3, $4, 'payment_received')`, uuid.NewString(), email, garage, customer)

	return vehicle
}

func TestAnonymize_ErasesPersonalDataAndKeepsTheRest(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	vehicle := w.seed(w.garage, w.customer, "an@x.test")
	bystander := uuid.NewString()
	w.seed(w.garage, bystander, "other@x.test")

	var counts customerDomain.AnonymizedCounts

	w.in(w.garage, func(ctx context.Context) {
		var err error

		counts, err = w.customers.Anonymize(ctx, w.garage, w.customer, w.owner)
		require.NoError(t, err)

		// Again: it is gone as far as this operation is concerned.
		_, err = w.customers.Anonymize(ctx, w.garage, w.customer, w.owner)
		require.ErrorIs(t, err, customerDomain.ErrCustomerNotFound)
	})

	assert.Equal(t, customerDomain.AnonymizedCounts{Customers: 1, Vehicles: 1, TaxProfiles: 1}, counts)

	var (
		name                       string
		phone, mail, address       *string
		active                     bool
		anonymizedAt               *time.Time
		plate                      string
		vin, engine                *string
		legal                      string
		taxID, invoiceEmail, taddr *string
		linkStatus                 string
	)

	require.NoError(t, w.pg.Admin.QueryRowContext(t.Context(), `SELECT full_name, phone, email, address, is_active, anonymized_at FROM customers WHERE id = $1`, w.customer).
		Scan(&name, &phone, &mail, &address, &active, &anonymizedAt))
	assert.Equal(t, customerDomain.AnonymizedName, name)
	assert.Nil(t, phone)
	assert.Nil(t, mail)
	assert.Nil(t, address)
	assert.False(t, active)
	assert.NotNil(t, anonymizedAt)

	require.NoError(t, w.pg.Admin.QueryRowContext(t.Context(), `SELECT license_plate, vin, engine_number FROM vehicles WHERE id = $1`, vehicle).Scan(&plate, &vin, &engine))
	assert.Equal(t, customerDomain.AnonymizedPlatePrefix, plate[:len(customerDomain.AnonymizedPlatePrefix)])
	assert.Nil(t, vin)
	assert.Nil(t, engine)

	require.NoError(t, w.pg.Admin.QueryRowContext(t.Context(), `SELECT legal_name, tax_id, address, invoice_email FROM customer_tax_profiles WHERE customer_id = $1`, w.customer).
		Scan(&legal, &taxID, &taddr, &invoiceEmail))
	assert.Equal(t, customerDomain.AnonymizedName, legal)
	assert.Nil(t, taxID)
	assert.Nil(t, taddr)
	assert.Nil(t, invoiceEmail)

	require.NoError(t, w.pg.Admin.QueryRowContext(t.Context(), `SELECT status::text FROM customer_user_links WHERE customer_id = $1`, w.customer).Scan(&linkStatus))
	assert.Equal(t, "unlinked", linkStatus)

	// The bystander is untouched.
	require.NoError(t, w.pg.Admin.QueryRowContext(t.Context(), `SELECT full_name FROM customers WHERE id = $1`, bystander).Scan(&name))
	assert.Equal(t, "An Nguyen", name)

	// The anonymized customer no longer appears under its old e-mail.
	w.in(w.garage, func(ctx context.Context) {
		refs, err := w.customers.FindByEmailAcrossGarages(ctx, "an@x.test")
		require.NoError(t, err)
		assert.Empty(t, refs)

		refs, err = w.customers.FindByEmailAcrossGarages(ctx, "other@x.test")
		require.NoError(t, err)
		assert.Len(t, refs, 1)

		c, err := w.customers.FindByID(ctx, w.garage, w.customer)
		require.NoError(t, err)
		assert.True(t, c.IsAnonymized())

		err = w.customers.SetActive(ctx, w.garage, w.customer, true, w.owner)
		require.ErrorIs(t, err, customerDomain.ErrCustomerNotFound, "an anonymized customer cannot be reactivated")
	})
}

func TestRequests_AreStoredPerGarage(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.seed(w.garage, w.customer, "an@x.test")

	id := uuid.NewString()

	w.in(w.garage, func(ctx context.Context) {
		r := &domain.Request{
			ID: id, GarageID: w.garage, CustomerID: w.customer, Type: domain.TypeCorrect, Status: domain.StatusPending, Source: domain.SourceStaff,
			Details: "my phone", RequestedBy: &w.owner, RequestedAt: time.Now().UTC(),
		}
		require.NoError(t, w.requests.Create(ctx, r))

		got, err := w.requests.FindForUpdate(ctx, w.garage, id)
		require.NoError(t, err)
		assert.Equal(t, "my phone", got.Details)

		now := time.Now().UTC()
		got.Status, got.Outcome, got.CompletedAt, got.CompletedBy = domain.StatusCompleted, "Changed phone to 0912345678", &now, &w.owner
		require.NoError(t, w.requests.Save(ctx, got))

		items, total, err := w.requests.List(ctx, w.garage, usecase.Filter{CustomerID: w.customer, Status: domain.StatusCompleted, Limit: 10})
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.Equal(t, "Changed phone to 0912345678", items[0].Outcome)

		require.NoError(t, w.requests.ClearDetails(ctx, w.garage, w.customer))

		again, err := w.requests.Find(ctx, w.garage, id)
		require.NoError(t, err)
		assert.Empty(t, again.Details, "anonymizing clears what the person asked to correct")
		assert.Equal(t, "Cleared when the customer was anonymized", again.Outcome, "a correction's outcome may repeat what was corrected")

		numbers, err := w.requests.Numbers(ctx, w.garage, w.customer)
		require.NoError(t, err)
		assert.Equal(t, usecase.DocumentNumbers{Quotes: []string{}, RepairOrders: []string{}, Settlements: []string{}, PaymentVouchers: []string{}}, numbers)
	})

	// Another garage sees none of it.
	w.in(w.other, func(ctx context.Context) {
		_, err := w.requests.Find(ctx, w.other, id)
		require.ErrorIs(t, err, domain.ErrRequestNotFound)

		_, total, err := w.requests.List(ctx, w.other, usecase.Filter{Limit: 10})
		require.NoError(t, err)
		assert.Zero(t, total)
	})
}

func TestConfirmations_WorkOnceAndExpire(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := t.Context()
	now := time.Now().UTC()

	confirmation := func(hash, email string, expires, created time.Time) *domain.Confirmation {
		return &domain.Confirmation{
			ID: uuid.NewString(), Email: email, Type: domain.TypeDelete, Details: "d", Locale: "vi", TokenHash: hash, SourceHash: "src",
			ExpiresAt: expires, CreatedAt: created,
		}
	}

	require.NoError(t, w.requests.CreateConfirmation(ctx, confirmation("live", "An@X.test", now.Add(time.Hour), now)))
	require.NoError(t, w.requests.CreateConfirmation(ctx, confirmation("stale", "an@x.test", now.Add(-time.Hour), now.Add(-25*time.Hour))))
	require.NoError(t, w.requests.CreateConfirmation(ctx, confirmation("old", "old@x.test", now.Add(-time.Hour), now.Add(-8*24*time.Hour))))

	n, err := w.requests.CountByEmail(ctx, "AN@x.test", now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, n, "the count ignores case and counts only the last hour")

	n, err = w.requests.CountBySource(ctx, "src", now.Add(-48*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	got, err := w.requests.Consume(ctx, "live", now)
	require.NoError(t, err)
	assert.Equal(t, "An@X.test", got.Email)
	assert.Equal(t, domain.TypeDelete, got.Type)

	_, err = w.requests.Consume(ctx, "live", now)
	require.ErrorIs(t, err, domain.ErrTokenInvalid, "a link works once")

	_, err = w.requests.Consume(ctx, "stale", now)
	require.ErrorIs(t, err, domain.ErrTokenInvalid, "an expired link does not")

	_, err = w.requests.Consume(ctx, "unknown", now)
	require.ErrorIs(t, err, domain.ErrTokenInvalid)

	purged, err := w.requests.PurgeBefore(ctx, now.Add(-7*24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, purged, "only the one older than 7 days; the other was consumed or is recent")
}

func TestFindByEmail_ReachesEveryGarage(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	other := uuid.NewString()

	w.seed(w.garage, w.customer, "Shared@X.test")
	w.seed(w.other, other, "shared@x.test")

	refs, err := w.customers.FindByEmailAcrossGarages(t.Context(), "SHARED@x.test")
	require.NoError(t, err)
	require.Len(t, refs, 2)
	assert.ElementsMatch(t, []string{w.garage, w.other}, []string{refs[0].GarageID, refs[1].GarageID})
}

func TestAnonymizedCustomerKeepsItsVehiclesAndProfilesLocked(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	vehicle := w.seed(w.garage, w.customer, "an@x.test")
	vehicles := customerRepository.NewPostgresVehicleRepository(w.pool)
	profiles := customerRepository.NewPostgresTaxProfileRepository(w.pool)

	var profileID string

	w.in(w.garage, func(ctx context.Context) {
		list, err := profiles.List(ctx, w.garage, w.customer, true)
		require.NoError(t, err)
		require.Len(t, list, 1)

		profileID = list[0].ID

		// Before: a vehicle and a profile can be changed.
		v, err := vehicles.FindByID(ctx, w.garage, vehicle)
		require.NoError(t, err)

		v.VIN = new("NEWVIN")
		require.NoError(t, vehicles.Update(ctx, v))

		_, err = w.customers.Anonymize(ctx, w.garage, w.customer, w.owner)
		require.NoError(t, err)
	})

	w.in(w.garage, func(ctx context.Context) {
		v, err := vehicles.FindByID(ctx, w.garage, vehicle)
		require.NoError(t, err)

		v.VIN, v.LicensePlate = new("REAL-VIN"), "51A-999.99"
		require.ErrorIs(t, vehicles.Update(ctx, v), customerDomain.ErrVehicleNotFound, "the erasure cannot be written back")
		require.ErrorIs(t, vehicles.SetActive(ctx, w.garage, vehicle, true, w.owner), customerDomain.ErrVehicleNotFound)

		p, err := profiles.FindByID(ctx, w.garage, w.customer, profileID)
		require.NoError(t, err)

		p.TaxID = new("0301234567")
		require.ErrorIs(t, profiles.Update(ctx, p), customerDomain.ErrTaxProfileNotFound)
		require.ErrorIs(t, profiles.SetActive(ctx, w.garage, w.customer, profileID, true, w.owner), customerDomain.ErrTaxProfileNotFound)
	})

	var vin *string

	require.NoError(t, w.pg.Admin.QueryRowContext(t.Context(), `SELECT vin FROM vehicles WHERE id = $1`, vehicle).Scan(&vin))
	assert.Nil(t, vin)
}
