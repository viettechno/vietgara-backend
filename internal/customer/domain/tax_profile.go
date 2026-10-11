package domain

import (
	"errors"
	"regexp"
	"time"
)

// TaxProfileKind separates a customer's personal tax profile from company
// ones (FR-CUS-07).
type TaxProfileKind string

// Valid TaxProfileKind values.
const (
	TaxProfilePersonal TaxProfileKind = "personal"
	TaxProfileCompany  TaxProfileKind = "company"
)

// IsValid reports whether k is one of the defined kinds.
func (k TaxProfileKind) IsValid() bool { return k == TaxProfilePersonal || k == TaxProfileCompany }

// TaxProfile is a legal identity a customer can be billed under
// (FR-CUS-07). See: Database Design Section 3.1.
type TaxProfile struct {
	ID         string
	GarageID   string
	CustomerID string
	Kind       TaxProfileKind
	LegalName  string
	TaxID      *string
	Address    *string
	// InvoiceEmail is where an e-invoice is sent.
	InvoiceEmail *string
	// IsDefault marks the profile a document is billed to when none is
	// chosen; at most one per customer.
	IsDefault bool
	IsActive  bool
	CreatedBy *string
	UpdatedBy *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MaxTaxProfileEmailLen mirrors the invoice_email column.
const MaxTaxProfileEmailLen = 255

var taxIDPattern = regexp.MustCompile(`^[0-9]{10}([0-9]{3})?$`)

// ValidTaxID reports whether id is 10 digits, or 13 with the branch suffix.
func ValidTaxID(id string) bool { return taxIDPattern.MatchString(id) }

// InvoiceReady reports whether an invoice can be issued to the profile: a
// tax id and an address are on file (the legal name always is).
func (p *TaxProfile) InvoiceReady() bool {
	return p.LegalName != "" && p.TaxID != nil && *p.TaxID != "" && p.Address != nil && *p.Address != ""
}

// Sentinel errors for the customer's tax profiles.
var (
	ErrTaxProfileNotFound   = errors.New("tax profile not found")
	ErrLegalNameRequired    = errors.New("legal name is required")
	ErrInvalidTaxProfile    = errors.New("tax profile kind must be personal or company")
	ErrInvalidTaxID         = errors.New("tax id must be 10 digits, or 13 with the branch suffix")
	ErrPersonalProfileTaken = errors.New("this customer already has a personal tax profile")
)
