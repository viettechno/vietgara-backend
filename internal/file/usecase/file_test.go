package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/file/domain"
	"github.com/viettechno/vietgara-backend/internal/file/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/storage"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

const (
	garageID = "11111111-1111-1111-1111-111111111111"
	fileID   = "22222222-2222-2222-2222-222222222222"
	actorID  = "33333333-3333-3333-3333-333333333333"
)

var (
	errBoom = errors.New("boom")
	png     = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
)

type env struct {
	uc      *usecase.UseCase
	files   *mocks.MockFileRepository
	storage *mocks.MockFileStorage
	usage   *mocks.MockFileUsageChecker
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{
		files: mocks.NewMockFileRepository(t), storage: mocks.NewMockFileStorage(t), usage: mocks.NewMockFileUsageChecker(t),
	}
	e.uc = usecase.New(e.files, e.storage, e.usage)

	return e
}

// as returns a request context of a garage member holding permissions
// (the owner when none is given and owner is true).
func as(owner bool, permissions ...access.Permission) context.Context {
	return access.WithAccess(context.Background(), &access.Access{
		GarageID: garageID, UserID: actorID, OwnerUserID: actorID, Owner: owner, Active: true, Permissions: permissions,
	})
}

func assertAppError(t *testing.T, err error, want apperror.Code) {
	t.Helper()

	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected *apperror.AppError, got %v", err)
	assert.Equal(t, want, appErr.Code)
}

func storedLogo() *domain.File {
	return &domain.File{
		ID: fileID, GarageID: garageID, Kind: domain.KindLogo, EntityType: "garage", EntityID: garageID,
		Key: "garages/" + garageID + "/logo/" + garageID + "/" + fileID + ".png", ContentType: "image/png", IsActive: true,
	}
}

func TestUseCase_StoreLogo(t *testing.T) {
	t.Parallel()

	t.Run("registers and stores a logo", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)

		var stored domain.File

		e.storage.EXPECT().Put(mock.Anything, mock.Anything, "image/png", png).Return("https://cdn/x", nil).Once()
		e.files.EXPECT().Create(mock.Anything, mock.Anything).
			Run(func(_ context.Context, f *domain.File) { stored = *f }).Return(nil).Once()

		key, err := e.uc.StoreLogo(t.Context(), garageID, actorID, png, "image/png")
		require.NoError(t, err)
		assert.Equal(t, stored.Key, key)
		assert.Regexp(t, `^garages/`+garageID+`/logo/`+garageID+`/[0-9a-f-]{36}\.png$`, key)
		assert.Equal(t, "image/png", stored.ContentType)
		assert.EqualValues(t, len(png), stored.Size)
		assert.Len(t, stored.SHA256, 64)
		assert.Equal(t, actorID, *stored.CreatedBy)
	})

	t.Run("refuses what is not a logo", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			content     []byte
			contentType string
		}{
			"unsupported type": {png, "image/gif"},
			"empty":            {nil, "image/png"},
			"not an image":     {[]byte("not an image"), "image/png"},
			"too large":        {append(append([]byte{}, png...), make([]byte, 1<<20)...), "image/png"},
		} {
			e := newEnv(t)

			_, err := e.uc.StoreLogo(t.Context(), garageID, actorID, tc.content, tc.contentType)
			require.ErrorIs(t, err, domain.ErrInvalidFile, name)
			assertAppError(t, err, apperror.CodeInvalidArgument)
		}
	})

	t.Run("removes the object when registering fails", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.storage.EXPECT().Put(mock.Anything, mock.Anything, "image/png", png).Return("u", nil).Once()
		e.files.EXPECT().Create(mock.Anything, mock.Anything).Return(errBoom).Once()
		e.storage.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil).Once()

		_, err := e.uc.StoreLogo(t.Context(), garageID, actorID, png, "image/png")
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("a storage failure is internal", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.storage.EXPECT().Put(mock.Anything, mock.Anything, "image/png", png).Return("", errBoom).Once()

		_, err := e.uc.StoreLogo(t.Context(), garageID, actorID, png, "image/png")
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestUseCase_Upload(t *testing.T) {
	t.Parallel()

	req := usecase.UploadRequest{
		GarageID: garageID, ActorID: actorID, Kind: domain.KindLogo, EntityType: "garage", EntityID: garageID,
		Content: png, ContentType: "image/png",
	}

	t.Run("a logo is attached by the garage, not uploaded directly", func(t *testing.T) {
		t.Parallel()

		_, err := newEnv(t).uc.Upload(as(true), req)
		require.ErrorIs(t, err, domain.ErrUseModule)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("needs the kind's permission", func(t *testing.T) {
		t.Parallel()

		_, err := newEnv(t).uc.Upload(as(false, access.QuoteRead), req)
		assertAppError(t, err, apperror.CodePermissionDenied)

		_, err = newEnv(t).uc.Upload(t.Context(), req)
		assertAppError(t, err, apperror.CodePermissionDenied)
	})

	t.Run("unknown kind", func(t *testing.T) {
		t.Parallel()

		bad := req
		bad.Kind = "repair_photo"

		_, err := newEnv(t).uc.Upload(as(true), bad)
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})
}

func TestUseCase_URL(t *testing.T) {
	t.Parallel()

	t.Run("a public file is served by its URL", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.files.EXPECT().FindByID(mock.Anything, garageID, fileID).Return(storedLogo(), nil).Once()
		e.storage.EXPECT().PublicURL(storedLogo().Key).Return("https://cdn/logo.png").Once()

		url, expires, err := e.uc.URL(as(false), garageID, fileID)
		require.NoError(t, err, "any member may read a logo")
		assert.Equal(t, "https://cdn/logo.png", url)
		assert.True(t, expires.IsZero())
	})

	t.Run("a missing file", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.files.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrFileNotFound).Once()

		_, _, err := e.uc.URL(as(true), garageID, "missing")
		assertAppError(t, err, apperror.CodeNotFound)
	})
}

func TestUseCase_Delete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		inUse       bool
		dryRun      bool
		findErr     error
		wantOutcome lifecycle.Outcome
		wantCode    apperror.Code
		wantDelete  bool
		wantDeact   bool
		member      bool
	}{
		{name: "deletes an unreferenced file and its object", wantOutcome: lifecycle.Deleted, wantDelete: true},
		{name: "deactivates a file a business row refers to", inUse: true, wantOutcome: lifecycle.Deactivated, wantDeact: true},
		{name: "a dry run changes nothing", inUse: true, dryRun: true, wantOutcome: lifecycle.Deactivated},
		{name: "unknown file", findErr: domain.ErrFileNotFound, wantCode: apperror.CodeNotFound},
		{name: "needs the kind's permission", member: true, wantCode: apperror.CodePermissionDenied},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := newEnv(t)
			f := storedLogo()

			if tt.findErr != nil {
				e.files.EXPECT().FindByID(mock.Anything, garageID, fileID).Return(nil, tt.findErr).Once()
			} else {
				e.files.EXPECT().FindByID(mock.Anything, garageID, fileID).Return(f, nil).Once()
			}

			if tt.findErr == nil && !tt.member {
				e.usage.EXPECT().FileInUse(mock.Anything, garageID, f).Return(tt.inUse, nil).Once()
			}

			if tt.wantDelete {
				e.files.EXPECT().SoftDelete(mock.Anything, garageID, fileID, actorID).Return(nil).Once()
				e.storage.EXPECT().Delete(mock.Anything, f.Key).Return(nil).Once()
			}

			if tt.wantDeact {
				e.files.EXPECT().SetActive(mock.Anything, garageID, fileID, false, actorID).Return(nil).Once()
			}

			ctx := as(true)
			if tt.member {
				ctx = as(false, access.QuoteRead)
			}

			outcome, err := e.uc.Delete(ctx, usecase.DeleteRequest{GarageID: garageID, FileID: fileID, ActorID: actorID, DryRun: tt.dryRun})
			if tt.wantCode != "" {
				assertAppError(t, err, tt.wantCode)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantOutcome, outcome)
		})
	}

	t.Run("a failing reference check is internal", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		f := storedLogo()
		e.files.EXPECT().FindByID(mock.Anything, garageID, fileID).Return(f, nil).Once()
		e.usage.EXPECT().FileInUse(mock.Anything, garageID, f).Return(false, errBoom).Once()

		_, err := e.uc.Delete(as(true), usecase.DeleteRequest{GarageID: garageID, FileID: fileID})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestUseCase_RemoveByKey(t *testing.T) {
	t.Parallel()

	t.Run("a registered file is soft-deleted and its object removed", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		f := storedLogo()
		e.files.EXPECT().FindByKey(mock.Anything, garageID, f.Key).Return(f, nil).Once()
		e.files.EXPECT().SoftDelete(mock.Anything, garageID, fileID, actorID).Return(nil).Once()
		e.storage.EXPECT().Delete(mock.Anything, f.Key).Return(nil).Once()

		require.NoError(t, e.uc.RemoveByKey(t.Context(), garageID, actorID, f.Key))
	})

	t.Run("an unregistered key only has its object removed", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.files.EXPECT().FindByKey(mock.Anything, garageID, "garages/old.png").Return(nil, domain.ErrFileNotFound).Once()
		e.storage.EXPECT().Delete(mock.Anything, "garages/old.png").Return(errBoom).Once()

		require.NoError(t, e.uc.RemoveByKey(t.Context(), garageID, actorID, "garages/old.png"), "a failed object removal is only logged")
	})

	t.Run("a registry failure is returned", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.files.EXPECT().FindByKey(mock.Anything, garageID, "k").Return(nil, errBoom).Once()

		assertAppError(t, e.uc.RemoveByKey(t.Context(), garageID, actorID, "k"), apperror.CodeInternal)
	})

	t.Run("a failing soft delete is returned", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		f := storedLogo()
		e.files.EXPECT().FindByKey(mock.Anything, garageID, f.Key).Return(f, nil).Once()
		e.files.EXPECT().SoftDelete(mock.Anything, garageID, fileID, actorID).Return(errBoom).Once()

		assertAppError(t, e.uc.RemoveByKey(t.Context(), garageID, actorID, f.Key), apperror.CodeInternal)
	})
}

func TestUseCase_PublicURL(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.storage.EXPECT().PublicURL("k").Return("https://cdn/k").Once()

	assert.Equal(t, "https://cdn/k", e.uc.PublicURL("k"))
}

// TestUseCase_FutureKinds runs the generic paths with kinds the registry
// does not have yet (a private one, a public one the caller uploads
// directly). It registers them for its own duration, so it is not parallel
// and the parallel tests only start after it.
func TestUseCase_FutureKinds(t *testing.T) { //nolint:tparallel // mutates the shared rule table
	domain.Rules["export"] = domain.Rule{
		Public: false, ContentTypes: map[string]string{"text/csv": "csv"}, MaxBytes: 64, EntityType: "garage",
		Write: access.ReportRead, Read: access.ReportRead,
	}
	domain.Rules["sign"] = domain.Rule{
		Public: true, ContentTypes: map[string]string{"image/png": "png"}, MaxBytes: 1 << 20, ImageOnly: true, EntityType: "garage",
	}

	t.Cleanup(func() {
		delete(domain.Rules, "export")
		delete(domain.Rules, "sign")
	})

	t.Run("a private file is stored without a URL and served signed", func(t *testing.T) {
		e := newEnv(t)
		e.storage.EXPECT().PutPrivate(mock.Anything, mock.Anything, "text/csv", []byte("a,b")).Return(nil).Once()
		e.files.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()

		res, err := e.uc.Upload(as(false, access.ReportRead), usecase.UploadRequest{
			GarageID: garageID, ActorID: actorID, Kind: "export", EntityType: "garage", EntityID: garageID,
			Content: []byte("a,b"), ContentType: "text/csv",
		})
		require.NoError(t, err)
		assert.Empty(t, res.URL)
		assert.Regexp(t, `^garages/`+garageID+`/export/`+garageID+`/[0-9a-f-]{36}\.csv$`, res.File.Key)

		private := storedLogo()
		private.Kind = "export"

		e.files.EXPECT().FindByID(mock.Anything, garageID, fileID).Return(private, nil).Times(3)

		expires := time.Now().Add(usecase.SignedURLTTL)
		e.storage.EXPECT().SignedURL(mock.Anything, private.Key, usecase.SignedURLTTL).Return("https://signed", expires, nil).Once()

		url, got, err := e.uc.URL(as(false, access.ReportRead), garageID, fileID)
		require.NoError(t, err)
		assert.Equal(t, "https://signed", url)
		assert.Equal(t, expires, got)

		_, _, err = e.uc.URL(as(false, access.QuoteRead), garageID, fileID)
		assertAppError(t, err, apperror.CodePermissionDenied)

		e.storage.EXPECT().SignedURL(mock.Anything, private.Key, usecase.SignedURLTTL).
			Return("", time.Time{}, storage.ErrPrivateNotConfigured).Once()

		_, _, err = e.uc.URL(as(true), garageID, fileID)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("a file belongs to the garage's own entity", func(t *testing.T) {
		_, err := newEnv(t).uc.Upload(as(true), usecase.UploadRequest{
			GarageID: garageID, ActorID: actorID, Kind: "sign", EntityType: "garage", EntityID: "another-garage",
			Content: png, ContentType: "image/png",
		})
		require.ErrorIs(t, err, domain.ErrInvalidEntity)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		_, err = newEnv(t).uc.Upload(as(true), usecase.UploadRequest{
			GarageID: garageID, ActorID: actorID, Kind: "sign", EntityType: "part", EntityID: garageID,
			Content: png, ContentType: "image/png",
		})
		require.ErrorIs(t, err, domain.ErrInvalidEntity)
	})

	t.Run("a public file is stored with its URL", func(t *testing.T) {
		e := newEnv(t)
		e.storage.EXPECT().Put(mock.Anything, mock.Anything, "image/png", png).Return("https://cdn/f.png", nil).Once()
		e.files.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()

		res, err := e.uc.Upload(as(false), usecase.UploadRequest{
			GarageID: garageID, ActorID: actorID, Kind: "sign", EntityType: "garage", EntityID: garageID,
			Content: png, ContentType: "image/png",
		})
		require.NoError(t, err)
		assert.Equal(t, "https://cdn/f.png", res.URL)
	})
}
