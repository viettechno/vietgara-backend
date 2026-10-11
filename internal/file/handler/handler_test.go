package handler_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	filev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/file/v1"

	"github.com/viettechno/vietgara-backend/internal/file/domain"
	"github.com/viettechno/vietgara-backend/internal/file/handler"
	"github.com/viettechno/vietgara-backend/internal/file/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
)

const (
	garageID = "11111111-1111-4111-8111-111111111111"
	fileID   = "22222222-2222-4222-8222-222222222222"
	userID   = "33333333-3333-4333-8333-333333333333"
)

var anyArg = mock.Anything

type env struct {
	srv     *handler.Server
	files   *mocks.MockFileRepository
	storage *mocks.MockFileStorage
	usage   *mocks.MockFileUsageChecker
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{
		files: mocks.NewMockFileRepository(t), storage: mocks.NewMockFileStorage(t), usage: mocks.NewMockFileUsageChecker(t),
	}
	e.srv = handler.NewServer(usecase.New(e.files, e.storage, e.usage))

	return e
}

// signedIn is a request of the garage's owner.
func signedIn(t *testing.T) context.Context {
	t.Helper()

	claims := &auth.Claims{}
	claims.Subject = userID

	ctx := middleware.WithClaims(t.Context(), claims)

	return access.WithAccess(ctx, &access.Access{GarageID: garageID, UserID: userID, OwnerUserID: userID, Owner: true, Active: true})
}

func reason(err error) string {
	for _, d := range status.Convert(err).Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info.GetReason()
		}
	}

	return ""
}

func logo() *domain.File {
	return &domain.File{
		ID: fileID, GarageID: garageID, Kind: domain.KindLogo, EntityType: "garage", EntityID: garageID,
		Key: "garages/g/logo/g/f.png", ContentType: "image/png", IsActive: true,
	}
}

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 3)

	for _, method := range []string{
		filev1.FileService_UploadFile_FullMethodName,
		filev1.FileService_GetFileUrl_FullMethodName,
		filev1.FileService_DeleteFile_FullMethodName,
	} {
		assert.Empty(t, p[method].Garage.Permission, "any member; the kind's permission is checked by the use case")
	}
}

func TestUploadFile(t *testing.T) {
	t.Parallel()

	e := newEnv(t)

	// A logo is attached through UploadGarageLogo, not here.
	_, err := e.srv.UploadFile(signedIn(t), &filev1.UploadFileRequest{
		GarageId: garageID, Kind: filev1.FileKind_FILE_KIND_LOGO, EntityType: "garage", EntityId: garageID,
		Content: []byte("x"), ContentType: "image/png",
	})
	assert.Equal(t, "FAILED_PRECONDITION", reason(err))

	_, err = e.srv.UploadFile(signedIn(t), &filev1.UploadFileRequest{GarageId: garageID})
	assert.Equal(t, "VALIDATION_ERROR", reason(err), "unspecified kind")
}

func TestGetFileUrl(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.files.EXPECT().FindByID(anyArg, garageID, fileID).Return(logo(), nil).Once()
	e.storage.EXPECT().PublicURL(logo().Key).Return("https://cdn/logo.png").Once()

	resp, err := e.srv.GetFileUrl(signedIn(t), &filev1.GetFileUrlRequest{GarageId: garageID, FileId: fileID})
	require.NoError(t, err)
	assert.Equal(t, "https://cdn/logo.png", resp.GetUrl())
	assert.Nil(t, resp.GetExpiresAt(), "a public URL does not expire")

	e.files.EXPECT().FindByID(anyArg, garageID, "missing").Return(nil, domain.ErrFileNotFound).Once()

	_, err = e.srv.GetFileUrl(signedIn(t), &filev1.GetFileUrlRequest{GarageId: garageID, FileId: "missing"})
	assert.Equal(t, "NOT_FOUND", reason(err))
}

func TestGetFileUrlSigned(t *testing.T) {
	// Private kinds do not exist yet, so register one for this test; it is
	// not parallel so no other test reads the rules meanwhile.
	domain.Rules["export"] = domain.Rule{Public: false}

	t.Cleanup(func() { delete(domain.Rules, "export") })

	e := newEnv(t)
	private := logo()
	private.Kind = "export"
	expires := time.Date(2026, 10, 12, 8, 5, 0, 0, time.UTC)

	e.files.EXPECT().FindByID(anyArg, garageID, fileID).Return(private, nil).Once()
	e.storage.EXPECT().SignedURL(anyArg, private.Key, usecase.SignedURLTTL).Return("https://signed", expires, nil).Once()

	resp, err := e.srv.GetFileUrl(signedIn(t), &filev1.GetFileUrlRequest{GarageId: garageID, FileId: fileID})
	require.NoError(t, err)
	assert.Equal(t, "https://signed", resp.GetUrl())
	assert.True(t, resp.GetExpiresAt().AsTime().Equal(expires))
}

func TestDeleteFile(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := logo()
	e.files.EXPECT().FindByID(anyArg, garageID, fileID).Return(f, nil).Twice()
	e.usage.EXPECT().FileInUse(anyArg, garageID, f).Return(true, nil).Twice()

	dry, err := e.srv.DeleteFile(signedIn(t), &filev1.DeleteFileRequest{GarageId: garageID, FileId: fileID, DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, dry.GetOutcome())

	e.files.EXPECT().SetActive(anyArg, garageID, fileID, false, userID).Return(nil).Once()

	got, err := e.srv.DeleteFile(signedIn(t), &filev1.DeleteFileRequest{GarageId: garageID, FileId: fileID})
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, got.GetOutcome())

	e.files.EXPECT().FindByID(anyArg, garageID, "missing").Return(nil, domain.ErrFileNotFound).Once()

	_, err = e.srv.DeleteFile(signedIn(t), &filev1.DeleteFileRequest{GarageId: garageID, FileId: "missing"})
	assert.Equal(t, "NOT_FOUND", reason(err))
}
