//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	customerDomain "github.com/viettechno/vietgara-backend/internal/customer/domain"
	customerRepository "github.com/viettechno/vietgara-backend/internal/customer/repository"
	customerUsecase "github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

type fixedDirectory struct{ userID string }

func (d fixedDirectory) FindVerifiedByEmail(context.Context, string) (string, bool, error) {
	return d.userID, true, nil
}

type acceptingCodes struct{}

func (acceptingCodes) Send(context.Context, string, string, string) error { return nil }

func (acceptingCodes) Check(context.Context, string, string) error { return nil }

type namedGarage struct{}

func (namedGarage) Name(context.Context, string) (string, error) { return "Gara A", nil }

// TestCustomerUserLinkPersistence runs the link flow through the real
// schema: the garage sees the link state of its own customers only, the
// user sees the links addressed to them across garages and nobody else's,
// and a customer has at most one current link.
//
//nolint:funlen // one scenario through the real schema, read top to bottom
func TestCustomerUserLinkPersistence(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	staffID, binhID, otherID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	garageID, otherGarageID := uuid.NewString(), uuid.NewString()
	customerID, otherCustomerID := uuid.NewString(), uuid.NewString()

	exec := func(query string, args ...any) {
		t.Helper()

		_, err := pg.Admin.ExecContext(t.Context(), query, args...)
		require.NoError(t, err)
	}

	for _, u := range [][2]string{{staffID, "staff@example.com"}, {binhID, "binh@example.com"}, {otherID, "other@example.com"}} {
		exec(`INSERT INTO users (id, email, full_name) VALUES ($1, $2, 'U')`, u[0], u[1])
	}

	exec(`INSERT INTO garages (id, user_id, name) VALUES ($1, $3, 'A'), ($2, $3, 'B')`, garageID, otherGarageID, staffID)
	exec(`INSERT INTO customers (id, garage_id, full_name, email) VALUES ($1, $2, 'Binh', 'binh@example.com')`, customerID, garageID)
	exec(`INSERT INTO customers (id, garage_id, full_name, email) VALUES ($1, $2, 'Binh', 'binh@example.com')`, otherCustomerID, otherGarageID)

	pool := pg.AppPool(t)
	txm := dbpkg.NewTxManager(pool)
	customers := customerRepository.NewPostgresCustomerRepository(pool)
	links := customerRepository.NewPostgresLinkRepository(pool)

	uc := customerUsecase.NewLinkUseCase(customerUsecase.LinkDeps{
		Links: links, Customers: customers, Users: fixedDirectory{userID: binhID}, Codes: acceptingCodes{},
		Garages: namedGarage{}, Audit: audit.NewLogger(pool),
	})

	as := func(scope dbpkg.Scope, fn func(ctx context.Context) error) error {
		return txm.WithTransaction(t.Context(), scope, fn)
	}
	staff := dbpkg.Scope{GarageID: garageID, UserID: staffID}

	var linkID string

	require.NoError(t, as(staff, func(ctx context.Context) error {
		link, err := uc.Request(ctx, garageID, customerID, staffID)
		require.NoError(t, err)

		linkID = link.ID

		c, err := customers.FindByID(ctx, garageID, customerID)
		require.NoError(t, err)
		assert.Equal(t, customerDomain.LinkRequested, c.LinkStatus)

		return nil
	}))

	// The user sees the request without any garage scope, someone else does not.
	require.NoError(t, as(dbpkg.UserScope(binhID), func(ctx context.Context) error {
		mine, err := uc.ListForUser(ctx, binhID)
		require.NoError(t, err)
		require.Len(t, mine, 1)
		assert.Equal(t, linkID, mine[0].ID)
		assert.Equal(t, garageID, mine[0].GarageID)

		return nil
	}))
	require.NoError(t, as(dbpkg.UserScope(otherID), func(ctx context.Context) error {
		mine, err := uc.ListForUser(ctx, otherID)
		require.NoError(t, err)
		assert.Empty(t, mine)

		_, err = links.FindForUser(ctx, otherID, linkID)
		require.ErrorIs(t, err, customerDomain.ErrLinkNotFound)

		return nil
	}))

	// Another garage's staff never sees the link.
	require.NoError(t, as(dbpkg.Scope{GarageID: otherGarageID, UserID: staffID}, func(ctx context.Context) error {
		_, err := links.FindCurrent(ctx, garageID, customerID)
		require.ErrorIs(t, err, customerDomain.ErrLinkNotFound)

		return nil
	}))

	// A second current link for the customer is refused by the index.
	err := as(staff, func(ctx context.Context) error {
		return links.Create(ctx, &customerDomain.UserLink{
			ID: uuid.NewString(), GarageID: garageID, CustomerID: customerID, UserID: otherID,
			Status: customerDomain.LinkRequested,
		})
	})
	require.ErrorIs(t, err, customerDomain.ErrAlreadyLinked)

	// The user accepts in the garage's scope; the garage then sees linked.
	require.NoError(t, as(dbpkg.Scope{GarageID: garageID, UserID: binhID}, func(ctx context.Context) error {
		link, err := uc.Accept(ctx, binhID, linkID)
		require.NoError(t, err)
		assert.Equal(t, customerDomain.ChannelApp, link.Channel)

		return nil
	}))
	require.NoError(t, as(staff, func(ctx context.Context) error {
		c, err := customers.FindByID(ctx, garageID, customerID)
		require.NoError(t, err)
		assert.Equal(t, customerDomain.LinkLinked, c.LinkStatus)

		return nil
	}))

	// The verified e-mail links the other garage's record on sign-in, once.
	require.NoError(t, as(dbpkg.Scope{GarageID: otherGarageID, UserID: binhID}, func(ctx context.Context) error {
		require.NoError(t, uc.AutoLink(ctx, binhID, otherGarageID, otherCustomerID))
		require.NoError(t, uc.AutoLink(ctx, binhID, otherGarageID, otherCustomerID))

		return nil
	}))
	require.NoError(t, as(dbpkg.UserScope(binhID), func(ctx context.Context) error {
		mine, err := uc.ListForUser(ctx, binhID)
		require.NoError(t, err)
		require.Len(t, mine, 2)

		return nil
	}))

	// Unlinked by the user: never linked again by the e-mail match.
	require.NoError(t, as(dbpkg.Scope{GarageID: garageID, UserID: binhID}, func(ctx context.Context) error {
		_, err := uc.UnlinkByUser(ctx, binhID, linkID)
		require.NoError(t, err)
		require.NoError(t, uc.AutoLink(ctx, binhID, garageID, customerID))

		return nil
	}))
	require.NoError(t, as(staff, func(ctx context.Context) error {
		c, err := customers.FindByID(ctx, garageID, customerID)
		require.NoError(t, err)
		assert.Equal(t, customerDomain.LinkUnlinked, c.LinkStatus)

		_, err = links.FindCurrent(ctx, garageID, customerID)
		require.ErrorIs(t, err, customerDomain.ErrLinkNotFound)

		return nil
	}))

	// The garage may ask again after an unlink.
	require.NoError(t, as(staff, func(ctx context.Context) error {
		again, err := uc.Request(ctx, garageID, customerID, staffID)
		require.NoError(t, err)
		assert.Equal(t, customerDomain.LinkRequested, again.Status)

		return nil
	}))
}
