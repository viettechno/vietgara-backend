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
)

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
	// EntityType is what the file belongs to; its id must be the garage's.
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
		ContentTypes:     map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp"},
		MaxBytes:         1 << 20,
		ImageOnly:        true,
		EntityType:       "garage",
		Write:            access.GarageWrite,
		AttachedByModule: true,
	},
}

// IsValid reports whether k is a defined Kind.
func (k Kind) IsValid() bool {
	_, ok := Rules[k]

	return ok
}

// File is one registered object. See: Database Design (files).
type File struct {
	ID          string
	GarageID    string
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

// Sentinel errors for business-rule violations in the File module.
var (
	ErrFileNotFound  = errors.New("file not found")
	ErrInvalidKind   = errors.New("file kind is not supported")
	ErrInvalidFile   = errors.New("the file is empty, too large or of an unsupported type")
	ErrInvalidEntity = errors.New("the file's entity does not belong to this garage")
	ErrUseModule     = errors.New("this kind of file is uploaded through the resource it belongs to")
)
