package repository_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/repository"
)

func TestPostgresCustomerRepository_Anonymize(t *testing.T) {
	t.Parallel()

	conn, pool := newPool(t)
	repo := repository.NewPostgresCustomerRepository(pool)

	// A customer with 2 vehicles and 1 tax profile.
	conn.EXPECT().Exec(mock.Anything, sqlContaining("UPDATE customers SET full_name"), []any{garageID, customerID, domain.AnonymizedName, actorID}).
		Return(commandTag("1"), nil).Once()
	conn.EXPECT().Exec(mock.Anything, sqlContaining("UPDATE vehicles SET license_plate"), []any{garageID, customerID, domain.AnonymizedPlatePrefix, actorID}).
		Return(commandTag("2"), nil).Once()
	conn.EXPECT().Exec(mock.Anything, sqlContaining("UPDATE customer_tax_profiles SET legal_name"), []any{garageID, customerID, domain.AnonymizedName, actorID}).
		Return(commandTag("1"), nil).Once()
	conn.EXPECT().Exec(mock.Anything, sqlContaining("UPDATE customer_user_links SET status"), []any{garageID, customerID, actorID}).
		Return(commandTag("1"), nil).Once()

	counts, err := repo.Anonymize(t.Context(), garageID, customerID, actorID)
	require.NoError(t, err)
	assert.Equal(t, domain.AnonymizedCounts{Customers: 1, Vehicles: 2, TaxProfiles: 1}, counts)

	// Not live, or anonymized already: nothing else is touched.
	conn.EXPECT().Exec(mock.Anything, sqlContaining("UPDATE customers SET full_name"), mock.Anything).Return(commandTag("0"), nil).Once()

	_, err = repo.Anonymize(t.Context(), garageID, customerID, actorID)
	require.ErrorIs(t, err, domain.ErrCustomerNotFound)

	_, err = repo.Anonymize(t.Context(), garageID, "bad", actorID)
	require.ErrorIs(t, err, domain.ErrCustomerNotFound)
}

func TestPostgresCustomerRepository_Anonymize_Failures(t *testing.T) {
	t.Parallel()

	for _, step := range []string{"UPDATE customers SET full_name", "UPDATE vehicles SET license_plate", "UPDATE customer_tax_profiles SET legal_name", "UPDATE customer_user_links SET status"} {
		conn, pool := newPool(t)
		repo := repository.NewPostgresCustomerRepository(pool)

		all := []string{"UPDATE customers SET full_name", "UPDATE vehicles SET license_plate", "UPDATE customer_tax_profiles SET legal_name", "UPDATE customer_user_links SET status"}
		for _, q := range all {
			if q == step {
				conn.EXPECT().Exec(mock.Anything, sqlContaining(q), mock.Anything).Return(commandTag("0"), errBoom).Once()

				break
			}

			conn.EXPECT().Exec(mock.Anything, sqlContaining(q), mock.Anything).Return(commandTag("1"), nil).Once()
		}

		_, err := repo.Anonymize(t.Context(), garageID, customerID, actorID)
		require.ErrorIs(t, err, errBoom, step)
	}
}
