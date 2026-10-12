package domain

import (
	"slices"
	"strings"
	"time"
)

// Model year bounds; the lower one is the first production automobile and
// matches the CHECK constraint on vehicles.year.
const (
	MinVehicleYear = 1886

	minPlateKeyLen = 4
	maxPlateKeyLen = 12
	maxPlateLen    = 20
)

// Vehicle is a vehicle owned by one customer of a garage (FR-CUS-02).
// See: Database Design Section 3.2.
type Vehicle struct {
	ID           string
	GarageID     string
	CustomerID   string
	LicensePlate string
	Make         *string
	Model        *string
	Year         *int
	VIN          *string
	EngineNumber *string
	// IsActive is false once the vehicle was deactivated instead of
	// deleted because documents refer to it (FR-CAT-07).
	IsActive bool
	// CustomerName is derived when reading and never persisted.
	CustomerName string
	// Editable is false while a live document refers to the vehicle, which
	// locks its identity fields (FR-CUS-02); derived when reading and never
	// persisted.
	Editable  bool
	CreatedBy *string
	UpdatedBy *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NormalizeLicensePlate upper-cases a plate and collapses inner whitespace,
// keeping separators such as "-" and "." for display ("51F-123.45"). The
// plate must hold 4 to 12 letters or digits once separators are removed.
func NormalizeLicensePlate(plate string) (string, error) {
	p := strings.ToUpper(strings.Join(strings.Fields(plate), " "))
	if p == "" {
		return "", ErrLicensePlateRequired
	}

	key := LicensePlateKey(p)
	if len(key) < minPlateKeyLen || len(key) > maxPlateKeyLen || len(p) > maxPlateLen {
		return "", ErrInvalidLicensePlate
	}

	return p, nil
}

// LicensePlateKey reduces a plate to its upper-case ASCII letters and
// digits. It mirrors the vehicles.license_plate_key generated column, so
// "51f-123.45" and "51F 12345" share the key "51F12345".
func LicensePlateKey(plate string) string {
	var b strings.Builder

	for _, r := range strings.ToUpper(plate) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}

	return b.String()
}

// ValidateYear checks a model year is between MinVehicleYear and next year.
func ValidateYear(year int, now time.Time) error {
	if year < MinVehicleYear || year > now.Year()+1 {
		return ErrInvalidYear
	}

	return nil
}

// IdentityField names a vehicle field that identifies it (FR-CUS-02): locked
// while a live document refers to the vehicle, changed by a correction
// (FR-CUS-09). Colour, notes and odometer are never locked.
type IdentityField string

// The identity fields.
const (
	FieldLicensePlate IdentityField = "license_plate"
	FieldVIN          IdentityField = "vin"
	FieldEngineNumber IdentityField = "engine_number"
	FieldMake         IdentityField = "make"
	FieldModel        IdentityField = "model"
	FieldYear         IdentityField = "year"
)

// IdentityFields lists every identity field.
var IdentityFields = []IdentityField{FieldLicensePlate, FieldVIN, FieldEngineNumber, FieldMake, FieldModel, FieldYear}

// IsValid reports whether f is an identity field.
func (f IdentityField) IsValid() bool {
	return slices.Contains(IdentityFields, f)
}

// LockedFields returns the identity fields that cannot be edited now: all
// of them while the vehicle is not editable, none otherwise.
func (v *Vehicle) LockedFields() []IdentityField {
	if v.Editable {
		return nil
	}

	return IdentityFields
}
