package domain

import (
	"errors"
	"time"
)

// Limits of the job catalogue (FR-CAT-05).
const (
	MaxJobNameLen     = 255
	MaxJobCategoryLen = 100
	// MaxJobParts bounds the parts one job suggests.
	MaxJobParts = 30
)

// JobPart is one part a job suggests: a default amount in a unit of the part.
type JobPart struct {
	PartID string
	// UnitID is a packaging unit of the part; nil means the base unit.
	UnitID *string
	// Quantity is the amount typed in UnitID (or the base unit), a decimal
	// string above zero.
	Quantity string
	// NonBillable marks a hidden consumable (FR-CAT-08).
	NonBillable bool

	// Derived when reading, never persisted: the part's code and name, the
	// unit's abbreviation and the part's current price per unit.
	PartSKU          string
	PartName         string
	UnitAbbreviation string
	UnitPrice        int64
	// BasePrice is that price per base unit, which is what a quote or order
	// line (priced per base-unit quantity) starts at.
	BasePrice    int64
	PartIsActive bool
}

// Job is one type of work of a garage with its standard labor charge and the
// parts it usually needs (FR-CAT-05).
type Job struct {
	ID       string
	GarageID string
	Name     string
	// Category is a free label to group jobs in the picker.
	Category        string
	LaborPrice      int64
	DurationMinutes int
	Parts           []JobPart
	IsActive        bool
	CreatedBy       *string
	UpdatedBy       *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Sentinel errors for the job catalogue.
var (
	ErrJobNotFound       = errors.New("job not found")
	ErrJobNameRequired   = errors.New("job name is required")
	ErrJobNameTaken      = errors.New("another job already has this name")
	ErrInvalidLaborPrice = errors.New("labor price must not be negative")
	ErrInvalidDuration   = errors.New("duration must not be negative")
	ErrTooManyJobParts   = errors.New("a job suggests at most 30 parts")
	ErrDuplicateJobPart  = errors.New("a part can be suggested only once in a job")
	// ErrJobPartUnit is returned for a suggested unit that is not a packaging
	// unit of the suggested part.
	ErrJobPartUnit = errors.New("the unit is not a packaging unit of the suggested part")
	ErrJobInactive = errors.New("the job is not active")
	// ErrPackagingUnitUsedByJob is returned when a packaging unit that a job
	// suggests is about to be removed from its part.
	ErrPackagingUnitUsedByJob = errors.New("a packaging unit that a job uses cannot be removed")
)
