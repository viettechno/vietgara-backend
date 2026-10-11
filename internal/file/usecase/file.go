package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/file/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

// SignedURLTTL is how long a private file's URL stays valid.
const SignedURLTTL = 5 * time.Minute

// UseCase implements the file registry.
type UseCase struct {
	files    Repository
	storage  Storage
	usage    UsageChecker
	entities EntityChecker
}

// New wires the use case.
func New(files Repository, storage Storage, usage UsageChecker, entities EntityChecker) *UseCase {
	return &UseCase{files: files, storage: storage, usage: usage, entities: entities}
}

// UploadRequest is the input for UseCase.Upload.
type UploadRequest struct {
	GarageID    string
	ActorID     string
	Kind        domain.Kind
	EntityType  string
	EntityID    string
	Content     []byte
	ContentType string
}

// Result is a stored file and, for a public kind, its URL.
type Result struct {
	File *domain.File
	URL  string
}

// Upload registers and stores a file of the garage. The caller needs the
// kind's write permission; kinds that the owning module attaches itself are
// refused here.
func (uc *UseCase) Upload(ctx context.Context, req UploadRequest) (*Result, error) {
	rule, ok := domain.Rules[req.Kind]
	if !ok {
		return nil, toAppError("upload file", domain.ErrInvalidKind)
	}

	if err := requirePermission(ctx, rule.Write); err != nil {
		return nil, err
	}

	if rule.AttachedByModule {
		return nil, toAppError("upload file", domain.ErrUseModule)
	}

	return uc.store(ctx, req, rule)
}

// StoreLogo registers and stores a garage's logo, for the Tenant module
// (which enforces garage.write and sets the logo on the garage). It returns
// the object key to keep on the garage.
func (uc *UseCase) StoreLogo(ctx context.Context, garageID, actorID string, content []byte, contentType string) (string, error) {
	result, err := uc.store(ctx, UploadRequest{
		GarageID: garageID, ActorID: actorID, Kind: domain.KindLogo, EntityType: "garage", EntityID: garageID,
		Content: content, ContentType: contentType,
	}, domain.Rules[domain.KindLogo])
	if err != nil {
		return "", err
	}

	return result.File.Key, nil
}

// RemoveByKey removes a file by its object key once the surrounding change
// commits: the registry row is soft-deleted and the object deleted. A key
// that is not registered (an old upload) only has its object deleted.
func (uc *UseCase) RemoveByKey(ctx context.Context, garageID, actorID, key string) error {
	f, err := uc.files.FindByKey(ctx, garageID, key)
	if err != nil && !errors.Is(err, domain.ErrFileNotFound) {
		return toAppError("remove file", err)
	}

	if f != nil {
		if err := uc.files.SoftDelete(ctx, garageID, f.ID, actorID); err != nil {
			return toAppError("remove file", err)
		}
	}

	platformdb.AfterCommit(ctx, func() {
		if err := uc.storage.Delete(context.WithoutCancel(ctx), key); err != nil {
			slog.Warn("file: removing the object failed", "key", key, "error", err)
		}
	})

	return nil
}

// PublicURL returns the URL of a public object key.
func (uc *UseCase) PublicURL(key string) string { return uc.storage.PublicURL(key) }

func (uc *UseCase) store(ctx context.Context, req UploadRequest, rule domain.Rule) (*Result, error) {
	contentType := strings.ToLower(strings.TrimSpace(req.ContentType))

	ext, ok := rule.ContentTypes[contentType]
	if !ok || len(req.Content) == 0 || len(req.Content) > rule.MaxBytes ||
		(rule.ImageOnly && http.DetectContentType(req.Content) != contentType) {
		return nil, toAppError("store file", domain.ErrInvalidFile)
	}

	if err := uc.checkEntity(ctx, req, rule); err != nil {
		return nil, err
	}

	sum := sha256.Sum256(req.Content)
	id := uuid.NewString()
	f := &domain.File{
		ID: id, GarageID: req.GarageID, Kind: req.Kind, EntityType: req.EntityType, EntityID: req.EntityID,
		Key: domain.ObjectKey(req.GarageID, req.Kind, req.EntityID, id, ext), ContentType: contentType,
		Size: int64(len(req.Content)), SHA256: hex.EncodeToString(sum[:]), IsActive: true, CreatedBy: actorRef(req.ActorID),
	}

	var (
		url string
		err error
	)

	if rule.Public {
		url, err = uc.storage.Put(ctx, f.Key, contentType, req.Content)
	} else {
		err = uc.storage.PutPrivate(ctx, f.Key, contentType, req.Content)
	}

	if err != nil {
		return nil, toAppError("store file", err)
	}

	if err := uc.files.Create(ctx, f); err != nil {
		_ = uc.storage.Delete(context.WithoutCancel(ctx), f.Key)

		return nil, toAppError("register file", err)
	}

	return &Result{File: f, URL: url}, nil
}

// checkEntity confirms the entity the file is for is of the kind's entity
// type and belongs to the garage: the garage itself by id, any other entity
// through the owning module. An entity type nobody can vouch for is
// refused, so a new kind cannot skip the check.
func (uc *UseCase) checkEntity(ctx context.Context, req UploadRequest, rule domain.Rule) error {
	if req.EntityType != rule.EntityType || req.EntityID == "" {
		return toAppError("store file", domain.ErrInvalidEntity)
	}

	if rule.EntityType == domain.EntityGarage {
		if req.EntityID != req.GarageID {
			return toAppError("store file", domain.ErrInvalidEntity)
		}

		return nil
	}

	exists, err := uc.entities.EntityExists(ctx, req.GarageID, req.EntityType, req.EntityID)
	if err != nil {
		return toAppError("check file entity", err)
	}

	if !exists {
		return toAppError("store file", domain.ErrInvalidEntity)
	}

	return nil
}

// URL returns where a file is served from: the public URL, or a short-lived
// signed URL for a private file. The caller needs the kind's read
// permission (any member when the kind names none).
func (uc *UseCase) URL(ctx context.Context, garageID, fileID string) (string, time.Time, error) {
	f, err := uc.files.FindByID(ctx, garageID, fileID)
	if err != nil {
		return "", time.Time{}, toAppError("get file url", err)
	}

	rule := domain.Rules[f.Kind]

	if err := requirePermission(ctx, rule.Read); err != nil {
		return "", time.Time{}, err
	}

	if rule.Public {
		return uc.storage.PublicURL(f.Key), time.Time{}, nil
	}

	url, expires, err := uc.storage.SignedURL(ctx, f.Key, SignedURLTTL)
	if err != nil {
		return "", time.Time{}, toAppError("sign file url", err)
	}

	return url, expires, nil
}

// DeleteRequest is the input for UseCase.Delete.
type DeleteRequest struct {
	GarageID string
	FileID   string
	ActorID  string
	// DryRun answers the outcome without changing anything.
	DryRun bool
}

// Delete follows the delete-or-deactivate rule (FR-CAT-07): a file that a
// business row refers to is deactivated; an unreferenced one is
// soft-deleted and its object removed once the change commits. The caller
// needs the kind's write permission.
func (uc *UseCase) Delete(ctx context.Context, req DeleteRequest) (lifecycle.Outcome, error) {
	f, err := uc.files.FindByID(ctx, req.GarageID, req.FileID)
	if err != nil {
		return "", toAppError("delete file", err)
	}

	if err := requirePermission(ctx, domain.Rules[f.Kind].Write); err != nil {
		return "", err
	}

	outcome, err := lifecycle.Apply(ctx, req.DryRun, lifecycle.Rule{
		InUse: func(ctx context.Context) (bool, error) { return uc.usage.FileInUse(ctx, req.GarageID, f) },
		Delete: func(ctx context.Context) error {
			if err := uc.files.SoftDelete(ctx, req.GarageID, f.ID, req.ActorID); err != nil {
				return err
			}

			platformdb.AfterCommit(ctx, func() {
				if err := uc.storage.Delete(context.WithoutCancel(ctx), f.Key); err != nil {
					slog.Warn("file: removing the object failed", "key", f.Key, "error", err)
				}
			})

			return nil
		},
		Deactivate: func(ctx context.Context) error {
			return uc.files.SetActive(ctx, req.GarageID, f.ID, false, req.ActorID)
		},
	})
	if err != nil {
		return "", toAppError("delete file", err)
	}

	return outcome, nil
}

// requirePermission refuses the caller of the request's garage unless they
// hold p (any active member when p is empty).
func requirePermission(ctx context.Context, p access.Permission) error {
	caller, ok := access.FromContext(ctx)
	if !ok {
		return apperror.New(apperror.CodePermissionDenied, access.ErrNoMembership.Error())
	}

	if err := caller.Check(access.Rule{Permission: p}); err != nil {
		return apperror.Wrap(apperror.CodePermissionDenied, err.Error(), err)
	}

	return nil
}

func actorRef(userID string) *string {
	if userID == "" {
		return nil
	}

	return &userID
}
