// Package domain contains the pure business types of the Catalogue module
// (HLD module #18): the part, its units of measure and packaging, the
// category tree and the part images, plus the conversion of a quantity to a
// part's base unit. Catalogue knows nothing about stock; see
// Coding_Standards.docx Section 2.1 and Database Design Section 5.
package domain

import (
	"errors"
	"math/big"
	"regexp"
	"strings"
	"time"
)

// Limits shared by the module's use cases.
const (
	MaxSKULen          = 50
	MaxNameLen         = 255
	MaxBrandLen        = 100
	MaxUnitNameLen     = 50
	MaxUnitAbbrevLen   = 20
	MaxCategoryNameLen = 255
	// MaxCategoryDepth is the depth of the category tree (FR-CAT-03).
	MaxCategoryDepth = 3
	// MaxPartImages bounds FR-CAT-04: one to three images per part.
	MaxPartImages = 3
	// MaxPartBarcodes bounds FR-INV-13: a part may carry up to twenty codes.
	MaxPartBarcodes = 20
	// MaxBarcodeLen is the length of a barcode after trimming.
	MaxBarcodeLen = 64
	// QuantityScale is the number of decimals a stored quantity keeps
	// (NUMERIC(14,3)).
	QuantityScale = 3
)

// Unit is one unit of measure of a garage (FR-CAT-01).
type Unit struct {
	ID           string
	GarageID     string
	Name         string
	Abbreviation string
	IsActive     bool
	CreatedBy    *string
	UpdatedBy    *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Category is one node of the part category tree (FR-CAT-03).
type Category struct {
	ID        string
	GarageID  string
	ParentID  *string
	Name      string
	IsActive  bool
	CreatedBy *string
	UpdatedBy *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PackagingUnit is a non-base unit of a part with its factor to the base
// unit (FR-CAT-02).
type PackagingUnit struct {
	UnitID string
	// Factor is base units per one of this unit, a decimal string above zero.
	Factor string
	// SalePrice overrides the part's sale price times Factor; nil derives it.
	SalePrice *int64
	// Output only.
	UnitName         string
	UnitAbbreviation string
	// EffectivePrice is the override or sale price times factor (VND).
	EffectivePrice int64
}

// Image is one image of a part, a file of kind part_image.
type Image struct {
	FileID   string
	Position int
	URL      string
}

// PartBarcode is one scannable code of a part (FR-INV-13). A code is unique
// across the garage, case-insensitively, and is stored as typed after
// trimming. UnitID is nil for the base unit and otherwise a packaging unit of
// the same part (a case barcode).
type PartBarcode struct {
	Barcode  string
	UnitID   *string
	UnitName string
	// UnitAbbreviation is the packaging unit's abbreviation; empty for the
	// base unit.
	UnitAbbreviation string
}

// Part is one part code of a garage (FR-CAT-01..04, FR-CAT-09).
type Part struct {
	ID                   string
	GarageID             string
	SKU                  string
	Name                 string
	BaseUnitID           string
	BaseUnitName         string
	BaseUnitAbbreviation string
	SalePrice            int64
	CategoryID           *string
	CategoryName         string
	Brand                string
	IsActive             bool
	// ReorderThreshold is a decimal string in the base unit.
	ReorderThreshold string
	PackagingUnits   []PackagingUnit
	// Barcodes are the codes that identify the part (FR-INV-13).
	Barcodes  []PartBarcode
	Images    []Image
	CreatedBy *string
	UpdatedBy *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Sentinel errors for business-rule violations in the Catalogue module.
var (
	ErrPartNotFound     = errors.New("part not found")
	ErrUnitNotFound     = errors.New("unit not found")
	ErrCategoryNotFound = errors.New("category not found")
	ErrImageNotFound    = errors.New("the part has no such image")

	ErrSKURequired          = errors.New("SKU is required")
	ErrNameRequired         = errors.New("name is required")
	ErrBaseUnitRequired     = errors.New("a base unit is required")
	ErrFieldTooLong         = errors.New("field is too long")
	ErrNegativePrice        = errors.New("sale price must not be negative")
	ErrInvalidThreshold     = errors.New("reorder threshold must be a number of at most 3 decimals and not negative")
	ErrUnitNameRequired     = errors.New("unit name is required")
	ErrUnitAbbrevRequired   = errors.New("unit abbreviation is required")
	ErrCategoryNameRequired = errors.New("category name is required")
	ErrCategoryDepth        = errors.New("categories are at most three levels deep")
	ErrCategoryCycle        = errors.New("a category cannot be moved under itself")
	ErrInvalidFactor        = errors.New("a factor must be a number above zero")
	ErrBaseUnitPackaged     = errors.New("the base unit cannot also be a packaging unit")
	ErrDuplicateUnit        = errors.New("a unit can appear only once in the packaging table")
	ErrInactiveUnit         = errors.New("the unit is not active")
	ErrInactiveCategory     = errors.New("the category is not active")
	ErrTooManyImages        = errors.New("a part has at most three images")
	ErrNotPartImage         = errors.New("the file is not an image of this garage's parts")
	ErrUnitNotForPart       = errors.New("the unit is not a packaging unit of this part")
	ErrQuantityPrecision    = errors.New("the converted quantity needs more than 3 decimals")
	ErrInvalidQuantity      = errors.New("quantity must be a positive number")
	ErrBaseUnitLocked       = errors.New("the base unit cannot change once the part has stock, packaging or documents")
	ErrCategoryHasChildren  = errors.New("the category still has sub-categories")
	ErrSKUTaken             = errors.New("another part already uses this SKU")
	ErrUnitNameTaken        = errors.New("another unit already has this name")
	ErrCategoryNameTaken    = errors.New("another category at this level already has this name")
	ErrPartInactive         = errors.New("the part is not active")
	ErrImageAlreadyAttached = errors.New("the image is already attached to a part")

	ErrBarcodeNotFound         = errors.New("no part has this barcode")
	ErrBarcodeInvalid          = errors.New("a barcode must be 1 to 64 characters: letters, digits, space or - . / + $ %")
	ErrBarcodeAlreadyUsed      = errors.New("another part of the garage already uses this barcode")
	ErrPackagingUnitHasBarcode = errors.New("a packaging unit with a barcode cannot be removed")
)

// CheckLength returns ErrFieldTooLong when value has more than maxLen
// characters.
func CheckLength(value string, maxLen int) error {
	if len([]rune(value)) > maxLen {
		return ErrFieldTooLong
	}

	return nil
}

// barcodePattern accepts the characters scanners produce: letters, digits,
// space and the punctuation - . / + $ % (FR-INV-13).
var barcodePattern = regexp.MustCompile(`^[A-Za-z0-9 \-./+$%]+$`)

// NormalizeBarcode trims a typed or scanned code and checks it: 1 to 64
// characters from barcodePattern. The code keeps its case; matching is
// case-insensitive.
func NormalizeBarcode(s string) (string, error) {
	barcode := strings.TrimSpace(s)
	if barcode == "" || len([]rune(barcode)) > MaxBarcodeLen || !barcodePattern.MatchString(barcode) {
		return "", ErrBarcodeInvalid
	}

	return barcode, nil
}

var decimalPattern = regexp.MustCompile(`^\d+(\.\d+)?$`)

// ParseDecimal parses a decimal string (digits with an optional fraction)
// into an exact rational. It rejects fractions ("1/2"), exponents, signs,
// blanks inside and a bare "." or "5.", so a quantity can never be misread.
func ParseDecimal(s string) (*big.Rat, bool) {
	s = strings.TrimSpace(s)
	if !decimalPattern.MatchString(s) {
		return nil, false
	}

	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, false
	}

	return r, true
}

// FormatDecimal renders r with up to QuantityScale decimals and no trailing
// zeros ("240", "8.75"). ok is false when r needs more decimals.
func FormatDecimal(r *big.Rat) (string, bool) {
	scaled := new(big.Rat).Mul(r, big.NewRat(1000, 1))
	if !scaled.IsInt() {
		return "", false
	}

	out := r.FloatString(QuantityScale)
	if strings.Contains(out, ".") {
		out = strings.TrimRight(strings.TrimRight(out, "0"), ".")
	}

	return out, true
}

// ConvertToBase returns entered * factor as a decimal string in the base
// unit: 10 cases of factor 24 are "240". entered must be above zero and the
// result must fit three decimals; otherwise it is refused, never rounded.
func ConvertToBase(entered, factor string) (string, error) {
	qty, ok := ParseDecimal(entered)
	if !ok || qty.Sign() <= 0 {
		return "", ErrInvalidQuantity
	}

	f, ok := ParseDecimal(factor)
	if !ok || f.Sign() <= 0 {
		return "", ErrInvalidFactor
	}

	out, ok := FormatDecimal(new(big.Rat).Mul(qty, f))
	if !ok {
		return "", ErrQuantityPrecision
	}

	return out, nil
}

// NormalizeFactor validates a factor and renders it with at most six
// decimals (NUMERIC(18,6)).
func NormalizeFactor(s string) (string, error) {
	r, ok := ParseDecimal(s)
	if !ok || r.Sign() <= 0 {
		return "", ErrInvalidFactor
	}

	scaled := new(big.Rat).Mul(r, big.NewRat(1_000_000, 1))
	if !scaled.IsInt() {
		return "", ErrInvalidFactor
	}

	out := r.FloatString(6)
	if strings.Contains(out, ".") {
		out = strings.TrimRight(strings.TrimRight(out, "0"), ".")
	}

	return out, nil
}

// NormalizeThreshold validates a reorder threshold: not negative, at most
// three decimals. An empty string means zero.
func NormalizeThreshold(s string) (string, error) {
	if strings.TrimSpace(s) == "" {
		return "0", nil
	}

	r, ok := ParseDecimal(s)
	if !ok {
		return "", ErrInvalidThreshold
	}

	out, ok := FormatDecimal(r)
	if !ok {
		return "", ErrInvalidThreshold
	}

	return out, nil
}

// PerBaseUnit is the price of one base unit when a packaging unit costs price
// and holds factor base units, rounded to whole dong. A quote or order line
// prices its base-unit quantity, so a pack price becomes this.
func PerBaseUnit(price int64, factor string) int64 {
	f, ok := ParseDecimal(factor)
	if !ok || f.Sign() == 0 {
		return 0
	}

	v := new(big.Rat).Quo(big.NewRat(price, 1), f)
	v.Add(v, big.NewRat(1, 2))

	return new(big.Int).Quo(v.Num(), v.Denom()).Int64()
}

// DerivedPrice is the price of one packaging unit when it has no override:
// sale price times factor, rounded to whole dong.
func DerivedPrice(salePrice int64, factor string) int64 {
	f, ok := ParseDecimal(factor)
	if !ok {
		return 0
	}

	v := new(big.Rat).Mul(big.NewRat(salePrice, 1), f)
	// Round half up to whole dong.
	v.Add(v, big.NewRat(1, 2))
	n := new(big.Int).Quo(v.Num(), v.Denom())

	return n.Int64()
}
