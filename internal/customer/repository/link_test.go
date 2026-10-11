package repository_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/repository"
)

const userID = "66666666-6666-6666-6666-666666666666"

func linkValues(status string, channel *string) []any {
	return []any{"l-1", garageID, customerID, userID, status, channel, testTime, nil, nil}
}

func TestPostgresLinkRepository_Create(t *testing.T) {
	t.Parallel()

	link := func() *domain.UserLink {
		return &domain.UserLink{
			ID: "l-1", GarageID: garageID, CustomerID: customerID, UserID: userID, Status: domain.LinkRequested,
			RequestedAt: time.Time{},
		}
	}

	t.Run("inserts a requested link", func(t *testing.T) {
		t.Parallel()

		conn, pool := newPool(t)
		conn.EXPECT().QueryRow(mock.Anything, sqlContaining("INSERT INTO customer_user_links"), mock.Anything).
			Return(valuesRow(t, testTime)).Once()

		l := link()
		require.NoError(t, repository.NewPostgresLinkRepository(pool).Create(t.Context(), l))
		assert.Equal(t, testTime, l.RequestedAt)
	})

	t.Run("a second current link is refused", func(t *testing.T) {
		t.Parallel()

		conn, pool := newPool(t)
		conn.EXPECT().QueryRow(mock.Anything, sqlContaining("INSERT INTO customer_user_links"), mock.Anything).
			Return(errRow(t, uniqueViolation("idx_customer_user_links_current"))).Once()

		err := repository.NewPostgresLinkRepository(pool).Create(t.Context(), link())
		require.ErrorIs(t, err, domain.ErrAlreadyLinked)
	})
}

func TestPostgresLinkRepository_Finders(t *testing.T) {
	t.Parallel()

	t.Run("current link", func(t *testing.T) {
		t.Parallel()

		conn, pool := newPool(t)
		conn.EXPECT().QueryRow(mock.Anything, sqlContaining("status IN ('requested', 'linked')"), []any{garageID, customerID}).
			Return(valuesRow(t, linkValues("linked", new("otp"))...)).Once()
		conn.EXPECT().QueryRow(mock.Anything, mock.Anything, mock.Anything).Return(errRow(t, pgx.ErrNoRows)).Once()

		repo := repository.NewPostgresLinkRepository(pool)

		l, err := repo.FindCurrent(t.Context(), garageID, customerID)
		require.NoError(t, err)
		assert.Equal(t, domain.LinkLinked, l.Status)
		assert.Equal(t, domain.ChannelOTP, l.Channel)

		_, err = repo.FindCurrent(t.Context(), garageID, customerID)
		require.ErrorIs(t, err, domain.ErrLinkNotFound)

		_, err = repo.FindCurrent(t.Context(), garageID, "not-a-uuid")
		require.ErrorIs(t, err, domain.ErrLinkNotFound)
	})

	t.Run("a user's own link", func(t *testing.T) {
		t.Parallel()

		conn, pool := newPool(t)
		conn.EXPECT().QueryRow(mock.Anything, sqlContaining("WHERE user_id = $1 AND id = $2"), []any{userID, customerID}).
			Return(valuesRow(t, linkValues("requested", nil)...)).Once()

		repo := repository.NewPostgresLinkRepository(pool)

		l, err := repo.FindForUser(t.Context(), userID, customerID)
		require.NoError(t, err)
		assert.Equal(t, domain.LinkRequested, l.Status)
		assert.Empty(t, l.Channel)

		_, err = repo.FindForUser(t.Context(), userID, "not-a-uuid")
		require.ErrorIs(t, err, domain.ErrLinkNotFound)
	})

	t.Run("a user's open links", func(t *testing.T) {
		t.Parallel()

		conn, pool := newPool(t)
		conn.EXPECT().Query(mock.Anything, sqlContaining("ORDER BY requested_at DESC"), []any{userID}).
			Return(resultRows(t, linkValues("requested", nil), linkValues("linked", new("app"))), nil).Once()

		links, err := repository.NewPostgresLinkRepository(pool).ListOpenForUser(t.Context(), userID)
		require.NoError(t, err)
		require.Len(t, links, 2)
		assert.Equal(t, domain.ChannelApp, links[1].Channel)
	})

	t.Run("list failures", func(t *testing.T) {
		t.Parallel()

		conn, pool := newPool(t)
		conn.EXPECT().Query(mock.Anything, mock.Anything, mock.Anything).Return(nil, errBoom).Once()
		conn.EXPECT().Query(mock.Anything, mock.Anything, mock.Anything).Return(resultRows(t, []any{errBoom}), nil).Once()

		repo := repository.NewPostgresLinkRepository(pool)

		_, err := repo.ListOpenForUser(t.Context(), userID)
		require.ErrorIs(t, err, errBoom)

		_, err = repo.ListOpenForUser(t.Context(), userID)
		require.ErrorIs(t, err, errBoom)
	})
}

func TestPostgresLinkRepository_HasAnyAndUpdate(t *testing.T) {
	t.Parallel()

	conn, pool := newPool(t)
	repo := repository.NewPostgresLinkRepository(pool)

	conn.EXPECT().QueryRow(mock.Anything, sqlContaining("SELECT EXISTS"), []any{customerID, userID}).
		Return(valuesRow(t, true)).Once()

	had, err := repo.HasAny(t.Context(), customerID, userID)
	require.NoError(t, err)
	assert.True(t, had)

	conn.EXPECT().Exec(mock.Anything, sqlContaining("UPDATE customer_user_links"), mock.Anything).Return(commandTag("1"), nil).Once()
	conn.EXPECT().Exec(mock.Anything, sqlContaining("UPDATE customer_user_links"), mock.Anything).Return(commandTag("0"), nil).Once()

	l := &domain.UserLink{ID: "l-1", Status: domain.LinkUnlinked}
	require.NoError(t, repo.Update(t.Context(), l))
	require.ErrorIs(t, repo.Update(t.Context(), l), domain.ErrLinkNotFound)
}
