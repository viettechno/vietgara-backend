// Package domain contains the pure business types of the File module: the
// registry of every uploaded file and the rules of each kind of file
// (Release 5.0). See: Coding_Standards.docx Section 2.1.
package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
)

// Kind says what a file is for. Every kind has its own rules (Rules).
type Kind string

// Valid Kind values. Kinds added later (repair photo, customer document,
// claim document, export) are private.
const (
	// KindLogo is a garage logo (public).
	KindLogo Kind = "logo"
	// KindPaymentEvidence is the screenshot of a subscription transfer; it
	// belongs to the owner user (private).
	KindPaymentEvidence Kind = "payment_evidence"
	// KindPlatformQR is the platform's bank QR code; it belongs to the
	// platform admin who uploaded it (private, served by signed URL).
	KindPlatformQR Kind = "platform_qr"
	// KindPartImage is one of the one to three images of a part (public);
	// it belongs to the part (EntityPart) and is attached through the
	// Catalogue's AddPartImage.
	KindPartImage Kind = "part_image"
)

// Entity types of a file owned by a user rather than a garage.
const (
	EntityUser     = "user"
	EntityPlatform = "platform"
)

// EntityGarage is the entity type of a file that belongs to the garage
// itself (its logo); its entity id is the garage's id.
const EntityGarage = "garage"

// EntityPart is the entity type of a part image; its entity id is the part's.
const EntityPart = "part"

// Rule is what one kind of file allows.
type Rule struct {
	// Public files are served by URL; private ones only through signed
	// URLs.
	Public bool
	// ContentTypes maps an accepted content type to the file extension.
	ContentTypes map[string]string
	// MaxBytes is the largest accepted file.
	MaxBytes int
	// ImageOnly additionally checks the content sniffs as its content type.
	ImageOnly bool
	// EntityType is what the file belongs to: EntityGarage (the entity id
	// is the garage's own) or another type an EntityChecker vouches for.
	EntityType string
	// Write is the permission uploading or deleting needs; Read the one
	// serving needs (empty: any active member).
	Write, Read access.Permission
	// AttachedByModule marks a kind the owning module uploads itself (the
	// garage logo is set through UploadGarageLogo), so UploadFile refuses
	// it instead of leaving an unattached file.
	AttachedByModule bool
}

// Rules are the rules of every kind.
var Rules = map[Kind]Rule{
	KindLogo: {
		Public:           true,
		ContentTypes:     imageTypes(),
		MaxBytes:         1 << 20,
		ImageOnly:        true,
		EntityType:       EntityGarage,
		Write:            access.GarageWrite,
		AttachedByModule: true,
	},
	KindPartImage: {
		Public:       true,
		ContentTypes: imageTypes(),
		MaxBytes:     2 << 20,
		ImageOnly:    true,
		EntityType:   EntityPart,
		Write:        access.CatalogueWrite,
	},
	KindPaymentEvidence: {
		ContentTypes:     withPDF(imageTypes()),
		MaxBytes:         5 << 20,
		EntityType:       EntityUser,
		AttachedByModule: true,
	},
	KindPlatformQR: {
		ContentTypes:     imageTypes(),
		MaxBytes:         1 << 20,
		ImageOnly:        true,
		EntityType:       EntityPlatform,
		AttachedByModule: true,
	},
}

// imageTypes are the accepted image content types with their extensions.
func imageTypes() map[string]string {
	return map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp"}
}

// withPDF adds PDF to a set of accepted content types.
func withPDF(types map[string]string) map[string]string {
	types["application/pdf"] = "pdf"

	return types
}

// IsValid reports whether k is a defined Kind.
func (k Kind) IsValid() bool {
	_, ok := Rules[k]

	return ok
}

// File is one registered object. See: Database Design (files).
type File struct {
	ID string
	// GarageID owns a garage's file; UserID a user's own (exactly one).
	GarageID    string
	UserID      string
	Kind        Kind
	EntityType  string
	EntityID    string
	Key         string
	ContentType string
	Size        int64
	SHA256      string
	IsActive    bool
	CreatedBy   *string
	CreatedAt   time.Time
}

// ObjectKey returns the storage key of a garage's file:
// garages/{garage_id}/{kind}/{entity_id}/{uuid}.{ext}.
func ObjectKey(garageID string, kind Kind, entityID, fileID, ext string) string {
	return fmt.Sprintf("garages/%s/%s/%s/%s.%s", garageID, kind, entityID, fileID, ext)
}

// UserObjectKey returns the storage key of a user's own file:
// users/{user_id}/{kind}/{uuid}.{ext}.
func UserObjectKey(userID string, kind Kind, fileID, ext string) string {
	return fmt.Sprintf("users/%s/%s/%s.%s", userID, kind, fileID, ext)
}

// Sentinel errors for business-rule violations in the File module.
var (
	ErrFileNotFound  = errors.New("file not found")
	ErrInvalidKind   = errors.New("file kind is not supported")
	ErrInvalidFile   = errors.New("the file is empty, too large or of an unsupported type")
	ErrInvalidEntity = errors.New("the file's entity does not belong to this garage")
	ErrUseModule     = errors.New("this kind of file is uploaded through the resource it belongs to")
)
