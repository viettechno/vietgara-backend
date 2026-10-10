//go:build integration

package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/repository"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

// integrationEnv holds a migrated Postgres with two garages and pools bound
// to a vietgara_app login, so the FORCE ROW LEVEL SECURITY policies apply.
type integrationEnv struct {
	txm       *dbpkg.TxManager
	customers *repository.PostgresCustomerRepository
	vehicles  *repository.PostgresVehicleRepository
	partners  *repository.PostgresPartnerRepository
	userID    string
	garageA   string
	garageB   string
}

func startIntegrationEnv(t *testing.T) *integrationEnv {
	t.Helper()

	pg := pgtest.StartMigrated(t)
	env := &integrationEnv{userID: uuid.NewString(), garageA: uuid.NewString(), garageB: uuid.NewString()}

	_, err := pg.Admin.ExecContext(t.Context(), `INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner')`, env.userID)
	require.NoError(t, err)

	_, err = pg.Admin.ExecContext(t.Context(), `INSERT INTO garages (id, user_id, name) VALUES ($1, $3, 'A'), ($2, $3, 'B')`,
		env.garageA, env.garageB, env.userID)
	require.NoError(t, err)

	pool := pg.AppPool(t)
	env.txm = dbpkg.NewTxManager(pool)
	env.customers = repository.NewPostgresCustomerRepository(pool)
	env.vehicles = repository.NewPostgresVehicleRepository(pool)
	env.partners = repository.NewPostgresPartnerRepository(pool)

	return env
}

// inGarage runs fn in a transaction scoped to garageID, the way the
// handlers do.
func (env *integrationEnv) inGarage(t *testing.T, garageID string, fn func(ctx context.Context)) {
	t.Helper()

	require.NoError(t, env.txm.WithTransaction(t.Context(), dbpkg.GarageScope(garageID), func(ctx context.Context) error {
		fn(ctx)

		return nil
	}))
}

func newCustomer(garageID, name, phone string) *domain.Customer {
	return &domain.Customer{
		ID: uuid.NewString(), GarageID: garageID, FullName: name, Phone: &phone,
		ConsentLog: []domain.ConsentRecord{{Granted: true, RecordedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}},
	}
}

func newVehicle(c *domain.Customer, plate string) *domain.Vehicle {
	return &domain.Vehicle{ID: uuid.NewString(), GarageID: c.GarageID, CustomerID: c.ID, LicensePlate: plate}
}

//nolint:funlen // one walk-through of the repositories against a shared, seeded database
func TestCustomerAndVehicleRepositories(t *testing.T) {
	env := startIntegrationEnv(t)
	alice := newCustomer(env.garageA, "Nguyễn Thị Alice", "0912345678")
	bob := newCustomer(env.garageA, "Tran Bob", "0987654321")

	env.inGarage(t, env.garageA, func(ctx context.Context) {
		require.NoError(t, env.customers.Create(ctx, alice))
		require.NoError(t, env.customers.Create(ctx, bob))
		assert.False(t, alice.CreatedAt.IsZero())

		carA := newVehicle(alice, "51F-123.45")
		require.NoError(t, env.vehicles.Create(ctx, carA))
		require.NoError(t, env.vehicles.Create(ctx, newVehicle(alice, "30A-999.99")))

		got, err := env.vehicles.FindByID(ctx, env.garageA, carA.ID)
		require.NoError(t, err)
		assert.Equal(t, "Nguyễn Thị Alice", got.CustomerName)
		assert.Equal(t, "51F-123.45", got.LicensePlate)

		found, err := env.customers.FindByID(ctx, env.garageA, alice.ID)
		require.NoError(t, err)
		assert.Equal(t, 2, found.VehicleCount)
		require.Len(t, found.ConsentLog, 1)
		assert.True(t, found.ConsentLog[0].Granted)
	})

	t.Run("duplicates are rejected with domain errors", func(t *testing.T) {
		err := env.txm.WithTransaction(t.Context(), dbpkg.GarageScope(env.garageA), func(ctx context.Context) error {
			return env.customers.Create(ctx, newCustomer(env.garageA, "Copy", "0912345678"))
		})
		require.ErrorIs(t, err, domain.ErrPhoneTaken)

		err = env.txm.WithTransaction(t.Context(), dbpkg.GarageScope(env.garageA), func(ctx context.Context) error {
			return env.vehicles.Create(ctx, newVehicle(bob, "51f 12345"))
		})
		require.ErrorIs(t, err, domain.ErrLicensePlateTaken)
	})

	t.Run("search matches name, phone digits and plates", func(t *testing.T) {
		env.inGarage(t, env.garageA, func(ctx context.Context) {
			for query, want := range map[string]string{
				"alice":        alice.ID,
				"0912 345":     alice.ID,
				"51F12":        alice.ID,
				"bob":          bob.ID,
				"100%_literal": "",
			} {
				list, total, err := env.customers.List(ctx, env.garageA, usecase.ListFilter{Query: query, Limit: 10})
				require.NoError(t, err)

				if want == "" {
					assert.Zero(t, total, query)

					continue
				}

				require.Equal(t, 1, total, query)
				assert.Equal(t, want, list[0].ID, query)
			}

			list, total, err := env.customers.List(ctx, env.garageA, usecase.ListFilter{Limit: 1, Offset: 1})
			require.NoError(t, err)
			assert.Equal(t, 2, total)
			require.Len(t, list, 1, "second page of one")

			vehicles, total, err := env.vehicles.List(ctx, env.garageA, usecase.VehicleFilter{
				ListFilter: usecase.ListFilter{Query: "30a.999", Limit: 10}, CustomerID: alice.ID,
			})
			require.NoError(t, err)
			require.Equal(t, 1, total)
			assert.Equal(t, "30A-999.99", vehicles[0].LicensePlate)
		})
	})

	t.Run("updates persist and transfer vehicle ownership", func(t *testing.T) {
		env.inGarage(t, env.garageA, func(ctx context.Context) {
			vehicles, _, err := env.vehicles.List(ctx, env.garageA, usecase.VehicleFilter{ListFilter: usecase.ListFilter{Query: "30A", Limit: 10}})
			require.NoError(t, err)
			require.Len(t, vehicles, 1)

			car := vehicles[0]
			car.CustomerID = bob.ID
			year := 2020
			car.Year = &year
			require.NoError(t, env.vehicles.Update(ctx, car))

			moved, err := env.vehicles.FindByID(ctx, env.garageA, car.ID)
			require.NoError(t, err)
			assert.Equal(t, "Tran Bob", moved.CustomerName)
			assert.Equal(t, 2020, *moved.Year)

			email := "bob@example.com"
			bob.Email = &email
			require.NoError(t, env.customers.Update(ctx, bob))

			got, err := env.customers.FindByID(ctx, env.garageA, bob.ID)
			require.NoError(t, err)
			assert.Equal(t, email, *got.Email)
			assert.Equal(t, 1, got.VehicleCount)
		})
	})

	t.Run("soft delete hides the rows and frees the phone number", func(t *testing.T) {
		env.inGarage(t, env.garageA, func(ctx context.Context) {
			require.NoError(t, env.customers.SoftDelete(ctx, env.garageA, alice.ID, env.userID))
			require.NoError(t, env.vehicles.SoftDeleteByCustomer(ctx, env.garageA, alice.ID, env.userID))

			_, err := env.customers.FindByID(ctx, env.garageA, alice.ID)
			require.ErrorIs(t, err, domain.ErrCustomerNotFound)
			require.ErrorIs(t, env.customers.SoftDelete(ctx, env.garageA, alice.ID, ""), domain.ErrCustomerNotFound)

			_, total, err := env.vehicles.List(ctx, env.garageA, usecase.VehicleFilter{ListFilter: usecase.ListFilter{Query: "51F", Limit: 10}})
			require.NoError(t, err)
			assert.Zero(t, total)

			require.NoError(t, env.customers.Create(ctx, newCustomer(env.garageA, "New owner of the number", "0912345678")))
			require.NoError(t, env.vehicles.Create(ctx, newVehicle(bob, "51F-123.45")))
		})
	})

	t.Run("row-level security isolates garages", func(t *testing.T) {
		env.inGarage(t, env.garageB, func(ctx context.Context) {
			_, err := env.customers.FindByID(ctx, env.garageA, bob.ID)
			require.ErrorIs(t, err, domain.ErrCustomerNotFound)

			_, total, err := env.customers.List(ctx, env.garageA, usecase.ListFilter{Limit: 10})
			require.NoError(t, err)
			assert.Zero(t, total)
		})

		err := env.txm.WithTransaction(t.Context(), dbpkg.GarageScope(env.garageB), func(ctx context.Context) error {
			return env.customers.Create(ctx, newCustomer(env.garageA, "Intruder", "0900000000"))
		})
		require.ErrorContains(t, err, "row-level security")

		// A garage B vehicle cannot be attached to a garage A customer.
		err = env.txm.WithTransaction(t.Context(), dbpkg.GarageScope(env.garageB), func(ctx context.Context) error {
			return env.vehicles.Create(ctx, &domain.Vehicle{ID: uuid.NewString(), GarageID: env.garageB, CustomerID: bob.ID, LicensePlate: "99Z-00000"})
		})
		require.ErrorIs(t, err, domain.ErrCustomerNotFound)
	})
}

func TestPartnerRepository(t *testing.T) {
	env := startIntegrationEnv(t)
	person, phone := "Ms. Lan", "0281234567"
	supplier := &domain.Partner{
		ID: uuid.NewString(), GarageID: env.garageA, Type: domain.PartnerTypeSupplier, Name: "Phu Tung Sai Gon",
		Contact: domain.PartnerContact{ContactPerson: &person, Phone: &phone},
	}

	env.inGarage(t, env.garageA, func(ctx context.Context) {
		require.NoError(t, env.partners.Create(ctx, supplier))
		require.NoError(t, env.partners.Create(ctx, &domain.Partner{
			ID: uuid.NewString(), GarageID: env.garageA, Type: domain.PartnerTypeInsurer, Name: "Bao Viet",
		}))
		// The same name is allowed under another type.
		require.NoError(t, env.partners.Create(ctx, &domain.Partner{
			ID: uuid.NewString(), GarageID: env.garageA, Type: domain.PartnerTypeInsurer, Name: "Phu Tung Sai Gon",
		}))

		got, err := env.partners.FindByID(ctx, env.garageA, supplier.ID)
		require.NoError(t, err)
		assert.Equal(t, domain.PartnerTypeSupplier, got.Type)
		assert.Equal(t, person, *got.Contact.ContactPerson)

		list, total, err := env.partners.List(ctx, env.garageA, usecase.PartnerFilter{
			ListFilter: usecase.ListFilter{Limit: 10}, Type: domain.PartnerTypeInsurer,
		})
		require.NoError(t, err)
		require.Equal(t, 2, total)
		assert.Equal(t, "Bao Viet", list[0].Name, "ordered by name")

		for _, query := range []string{"lan", "028 123", "sai gon"} {
			_, total, err := env.partners.List(ctx, env.garageA, usecase.PartnerFilter{
				ListFilter: usecase.ListFilter{Query: query, Limit: 10}, Type: domain.PartnerTypeSupplier,
			})
			require.NoError(t, err)
			assert.Equal(t, 1, total, query)
		}

		note := "Pays within 30 days"
		got.Note = &note
		require.NoError(t, env.partners.Update(ctx, got))

		got, err = env.partners.FindByID(ctx, env.garageA, supplier.ID)
		require.NoError(t, err)
		assert.Equal(t, note, *got.Note)
	})

	err := env.txm.WithTransaction(t.Context(), dbpkg.GarageScope(env.garageA), func(ctx context.Context) error {
		return env.partners.Create(ctx, &domain.Partner{
			ID: uuid.NewString(), GarageID: env.garageA, Type: domain.PartnerTypeSupplier, Name: "phu tung SAI GON",
		})
	})
	require.ErrorIs(t, err, domain.ErrPartnerNameTaken)

	env.inGarage(t, env.garageA, func(ctx context.Context) {
		require.NoError(t, env.partners.SoftDelete(ctx, env.garageA, supplier.ID, env.userID))
		_, err := env.partners.FindByID(ctx, env.garageA, supplier.ID)
		require.ErrorIs(t, err, domain.ErrPartnerNotFound)
	})

	env.inGarage(t, env.garageB, func(ctx context.Context) {
		_, total, err := env.partners.List(ctx, env.garageA, usecase.PartnerFilter{ListFilter: usecase.ListFilter{Limit: 10}})
		require.NoError(t, err)
		assert.Zero(t, total)
	})
}

// The Customer Portal's lookup (Release 2.4, migration 000020) finds a
// person's customer records in every garage by e-mail, case-insensitively,
// although the caller belongs to none of those garages and RLS still hides
// the rows themselves.
func TestFindByEmailAcrossGarages(t *testing.T) {
	env := startIntegrationEnv(t)

	email := func(c *domain.Customer, address string) *domain.Customer {
		c.Email = &address

		return c
	}

	inA := email(newCustomer(env.garageA, "Binh in A", "0911111111"), "Binh@Example.com")
	inB := email(newCustomer(env.garageB, "Binh in B", "0922222222"), "binh@example.com")
	removed := email(newCustomer(env.garageB, "Old Binh", "0933333333"), "binh@example.com")
	other := email(newCustomer(env.garageA, "Someone else", "0944444444"), "other@example.com")

	env.inGarage(t, env.garageA, func(ctx context.Context) {
		require.NoError(t, env.customers.Create(ctx, inA))
		require.NoError(t, env.customers.Create(ctx, other))
	})
	env.inGarage(t, env.garageB, func(ctx context.Context) {
		require.NoError(t, env.customers.Create(ctx, inB))
		require.NoError(t, env.customers.Create(ctx, removed))
		require.NoError(t, env.customers.SoftDelete(ctx, env.garageB, removed.ID, env.userID))
	})

	// No garage scope at all: only the SECURITY DEFINER function sees rows.
	err := env.txm.WithTransaction(t.Context(), dbpkg.UserScope(uuid.NewString()), func(ctx context.Context) error {
		refs, err := env.customers.FindByEmailAcrossGarages(ctx, "BINH@example.com")
		require.NoError(t, err)
		assert.ElementsMatch(t, []repository.CustomerRef{
			{ID: inA.ID, GarageID: env.garageA}, {ID: inB.ID, GarageID: env.garageB},
		}, refs)

		_, err = env.customers.FindByID(ctx, env.garageA, inA.ID)
		require.ErrorIs(t, err, domain.ErrCustomerNotFound, "RLS still hides the record itself")

		none, err := env.customers.FindByEmailAcrossGarages(ctx, "nobody@example.com")
		require.NoError(t, err)
		assert.Empty(t, none)

		return nil
	})
	require.NoError(t, err)
}
