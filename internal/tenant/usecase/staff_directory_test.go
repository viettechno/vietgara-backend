package usecase_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/tenanttest"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

var errBoom = errors.New("boom")

type limits = tenanttest.Limits

const (
	owner = tenanttest.Owner
	staff = tenanttest.Staff
)

type env struct {
	*tenanttest.Env
}

func newEnv(t *testing.T, lim limits) *env {
	t.Helper()

	return &env{tenanttest.NewEnv(lim)}
}

// as signs in userID and authorizes it for garageID.
func (e *env) as(t *testing.T, userID, garageID string) context.Context {
	t.Helper()

	claims := &auth.Claims{}
	claims.Subject = userID
	ctx := middleware.WithClaims(t.Context(), claims)

	if garageID != "" {
		user, err := e.Store.GarageAccess(ctx, garageID, userID)
		require.NoError(t, err)

		ctx = access.WithAccess(ctx, user)
	}

	return ctx
}

// garage creates a garage for the owner.
func (e *env) garage(t *testing.T) string {
	t.Helper()

	name := "Gara Minh"
	g, err := e.UC.CreateGarage(e.as(t, owner, ""), owner, usecase.GarageFields{Name: &name})
	require.NoError(t, err)

	return g.Garage.ID
}

func requireReason(t *testing.T, err error, reason string, _ ...string) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, reason, apperror.ReasonOf(err), err)
}

//go:fix inline

func groupNamed(t *testing.T, e *env, garageID, name string) *domain.StaffGroup {
	t.Helper()

	groups, err := e.UC.ListGroups(t.Context(), garageID)
	require.NoError(t, err)

	i := slices.IndexFunc(groups, func(g *domain.StaffGroup) bool { return g.Name == name })
	require.GreaterOrEqual(t, i, 0, name)

	return groups[i]
}

var _ = time.Now
