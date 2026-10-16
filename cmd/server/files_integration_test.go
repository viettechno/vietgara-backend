//go:build integration

package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	catalogueRepository "github.com/viettechno/vietgara-backend/internal/catalogue/repository"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	fileUsecase "github.com/viettechno/vietgara-backend/internal/file/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
	"github.com/viettechno/vietgara-backend/internal/platform/storage"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

// TestGarageLogoFileLifecycle runs the file registry against Postgres and
// the local store: a logo is registered under the key layout, a logo the
// garage still uses is deactivated instead of deleted, an unused one is
// deleted with its object, and another garage cannot see the file.
func TestGarageLogoFileLifecycle(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	userID, garageID, otherGarageID := uuid.NewString(), uuid.NewString(), uuid.NewString()

	exec := func(query string, args ...any) {
		t.Helper()

		_, err := pg.Admin.ExecContext(t.Context(), query, args...)
		require.NoError(t, err)
	}

	exec(`INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner')`, userID)
	exec(`INSERT INTO garages (id, user_id, name) VALUES ($1, $3, 'A'), ($2, $3, 'B')`, garageID, otherGarageID, userID)

	dir := t.TempDir()
	store, err := storage.NewLocal(dir, "", nil)
	require.NoError(t, err)

	pool := pg.AppPool(t)
	txm := dbpkg.NewTxManager(pool)
	garages := tenantRepo.NewPostgresGarageRepository(pool)
	files := buildFiles(pool, store, garages, catalogueRepository.NewPostgresRepository(pool), &fileEntities{})
	logos := tenantLogos{files: files.uc, store: store}

	owner := func(ctx context.Context) context.Context {
		return access.WithAccess(ctx, &access.Access{GarageID: garageID, UserID: userID, OwnerUserID: userID, Owner: true, Active: true})
	}

	inGarage := func(id string, fn func(ctx context.Context)) {
		t.Helper()

		scope := dbpkg.Scope{GarageID: id, UserID: userID}

		require.NoError(t, txm.WithTransaction(t.Context(), scope, func(ctx context.Context) error {
			fn(owner(ctx))

			return nil
		}))
	}

	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)

	var key, fileID string

	inGarage(garageID, func(ctx context.Context) {
		var err error

		key, err = logos.StoreLogo(ctx, garageID, userID, png, "image/png")
		require.NoError(t, err)
		assert.Regexp(t, `^garages/`+garageID+`/logo/`+garageID+`/[0-9a-f-]{36}\.png$`, key)
		assert.FileExists(t, filepath.Join(dir, filepath.FromSlash(key)))

		_, _, err = files.uc.URL(ctx, garageID, "00000000-0000-0000-0000-000000000000")
		require.ErrorContains(t, err, "file not found")

		_, err = logos.StoreLogo(ctx, garageID, userID, []byte("not an image"), "image/png")
		require.ErrorContains(t, err, "logo", "an invalid logo is the tenant's error")
	})

	// The file has a registry row, owned by the garage and audited.
	require.NoError(t, pg.Admin.QueryRowContext(t.Context(),
		`SELECT id FROM files WHERE key = $1 AND garage_id = $2 AND created_by = $3 AND is_active`, key, garageID, userID).Scan(&fileID))

	inGarage(garageID, func(ctx context.Context) {
		g, err := garages.FindByID(ctx, garageID)
		require.NoError(t, err)

		g.LogoKey = &key
		require.NoError(t, garages.Update(ctx, g))

		url, expires, err := files.uc.URL(ctx, garageID, fileID)
		require.NoError(t, err)
		assert.Equal(t, "/api/v1/files/"+key, url)
		assert.True(t, expires.IsZero())

		outcome, err := files.uc.Delete(ctx, fileUsecase.DeleteRequest{GarageID: garageID, FileID: fileID, ActorID: userID, DryRun: true})
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deactivated, outcome, "the garage still uses it")

		outcome, err = files.uc.Delete(ctx, fileUsecase.DeleteRequest{GarageID: garageID, FileID: fileID, ActorID: userID})
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deactivated, outcome)
	})

	var active bool

	require.NoError(t, pg.Admin.QueryRowContext(t.Context(), `SELECT is_active FROM files WHERE id = $1`, fileID).Scan(&active))
	assert.False(t, active)
	assert.FileExists(t, filepath.Join(dir, filepath.FromSlash(key)), "an object in use stays")

	// Another garage cannot see the file.
	inGarage(otherGarageID, func(ctx context.Context) {
		_, _, err := files.uc.URL(ctx, otherGarageID, fileID)
		require.ErrorContains(t, err, "file not found")
	})

	// Once the garage drops the logo, deleting removes the row and object.
	inGarage(garageID, func(ctx context.Context) {
		g, err := garages.FindByID(ctx, garageID)
		require.NoError(t, err)

		g.LogoKey = nil
		require.NoError(t, garages.Update(ctx, g))

		outcome, err := files.uc.Delete(ctx, fileUsecase.DeleteRequest{GarageID: garageID, FileID: fileID, ActorID: userID})
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deleted, outcome)
	})

	assert.NoFileExists(t, filepath.Join(dir, filepath.FromSlash(key)))

	var deleted bool

	require.NoError(t, pg.Admin.QueryRowContext(t.Context(), `SELECT deleted_at IS NOT NULL FROM files WHERE id = $1`, fileID).Scan(&deleted))
	assert.True(t, deleted)
}
