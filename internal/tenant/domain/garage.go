// Package domain contains pure business types for the Tenant/Garage
// Management module. See Coding_Standards.docx Section 2.1.
package domain

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// GarageStatus is the lifecycle state of a garage.
type GarageStatus string

// Valid GarageStatus values.
const (
	GarageStatusActive   GarageStatus = "active"
	GarageStatusInactive GarageStatus = "inactive"
)

// IsValid reports whether s is one of the defined GarageStatus values.
func (s GarageStatus) IsValid() bool {
	return s == GarageStatusActive || s == GarageStatusInactive
}

// QuoteApprovalMode selects how a garage's quotes are approved by the
// customer by default (FR-TEN-05, FR-QUO-02); a service advisor may still
// change it per quote (Quote & Pricing module).
type QuoteApprovalMode string

// Valid QuoteApprovalMode values.
const (
	// QuoteApprovalModeLink sends a public approval link, e-mailed to the
	// customer automatically when they have an e-mail on file.
	QuoteApprovalModeLink QuoteApprovalMode = "link"
	// QuoteApprovalModeManual sends no link: a staff member records the
	// customer's decision directly (e.g. approved face to face).
	QuoteApprovalModeManual QuoteApprovalMode = "manual"
)

// IsValid reports whether m is one of the defined QuoteApprovalMode values.
func (m QuoteApprovalMode) IsValid() bool {
	return m == QuoteApprovalModeLink || m == QuoteApprovalModeManual
}

// Defaults of a new garage (Database Design 2.2).
const (
	DefaultTimezone                     = "Asia/Ho_Chi_Minh"
	DefaultCurrency                     = "VND"
	DefaultQuoteNumberTemplate          = "BG-{YYYY}{MM}-{SEQ:4}"
	DefaultRepairOrderNumberTemplate    = "LSC-{YYYY}{MM}-{SEQ:4}"
	DefaultSettlementNumberTemplate     = "QT-{YYYY}{MM}-{SEQ:4}"
	DefaultPaymentVoucherNumberTemplate = "PT-{YYYY}{MM}-{SEQ:4}"
	MaxGarageNameLength                 = 255
	MaxTaxCodeLength                    = 20
	MaxTemplateLength                   = 50
	MaxLegalNameLength                  = 255
	MaxLegalAddressLength               = 500
	MaxBankFieldLength                  = 100
)

// DailyHours is the opening time of one weekday (1 = Monday ... 7 =
// Sunday), "HH:MM" in the garage's timezone (FR-TEN-05).
type DailyHours struct {
	Day    int    `json:"day"`
	Closed bool   `json:"closed"`
	Open   string `json:"open"`
	Close  string `json:"close"`
}

// DefaultOperatingHours returns Monday-Friday 08:00-17:30, Saturday 08:00-12:00,
// closed on Sunday.
func DefaultOperatingHours() []DailyHours {
	hours := make([]DailyHours, 0, 7)
	for day := 1; day <= 5; day++ {
		hours = append(hours, DailyHours{Day: day, Open: "08:00", Close: "17:30"})
	}

	return append(hours, DailyHours{Day: 6, Open: "08:00", Close: "12:00"}, DailyHours{Day: 7, Closed: true})
}

// BankDetails is the garage's bank account, printed on documents it issues
// (FR-TEN-06); persisted as the garages.bank_details JSON document.
type BankDetails struct {
	BankName      *string `json:"bank_name,omitempty"`
	AccountNumber *string `json:"account_number,omitempty"`
	AccountHolder *string `json:"account_holder,omitempty"`
}

// Garage is one tenant: an independent operating/data-isolation unit.
// A user may own many garages (one user -- many garages); "branch"
// is used synonymously with "garage" throughout VietGara.
// See: Database Design Section 2.2, ADR-005.
type Garage struct {
	ID          string
	OwnerUserID string
	Name        string
	TaxCode     *string
	Address     *string
	Phone       *string
	Email       *string
	// LegalName, LegalAddress, Representative and Bank are the legal identity
	// of the garage (FR-TEN-06), printed with TaxCode and the logo in the
	// header of every document. Documents keep a snapshot when issued.
	LegalName      *string
	LegalAddress   *string
	Representative *string
	Bank           BankDetails
	// LogoKey is the object key of the logo in the file registry; the URL
	// is derived from it when the garage is returned.
	LogoKey                   *string
	Timezone                  string
	Currency                  string
	Status                    GarageStatus
	OperatingHours            []DailyHours
	QuoteNumberTemplate       string
	RepairOrderNumberTemplate string
	SettlementNumberTemplate  string
	// PaymentVoucherNumberTemplate numbers payment vouchers (FR-BIL-05).
	PaymentVoucherNumberTemplate string
	DefaultQuoteApprovalMode     QuoteApprovalMode
	CreatedBy                    *string
	UpdatedBy                    *string
	CreatedAt                    time.Time
	UpdatedAt                    time.Time
}

// ValidName reports whether name is a usable garage name.
func ValidName(name string) bool {
	return name != "" && utf8.RuneCountInString(name) <= MaxGarageNameLength
}

// ValidTimezone reports whether tz is a known IANA timezone.
func ValidTimezone(tz string) bool {
	if tz == "" || len(tz) > 64 {
		return false
	}

	_, err := time.LoadLocation(tz)

	return err == nil && tz != "Local"
}

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// ValidCurrency reports whether c is an ISO 4217 code shape.
func ValidCurrency(c string) bool {
	return currencyPattern.MatchString(c)
}

var (
	hhmm             = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	templateToken    = regexp.MustCompile(`\{[^}]*\}`)
	allowedTemplates = map[string]bool{"{YYYY}": true, "{YY}": true, "{MM}": true, "{DD}": true}
	seqToken         = regexp.MustCompile(`^\{SEQ:[1-9]\}$`)
)

// ValidOperatingHours reports whether hours lists each weekday at most once
// with valid, ordered times.
func ValidOperatingHours(hours []DailyHours) bool {
	seen := map[int]bool{}

	for _, h := range hours {
		if h.Day < 1 || h.Day > 7 || seen[h.Day] {
			return false
		}

		seen[h.Day] = true

		if h.Closed {
			continue
		}

		if !hhmm.MatchString(h.Open) || !hhmm.MatchString(h.Close) || h.Open >= h.Close {
			return false
		}
	}

	return true
}

// ValidNumberingTemplate reports whether t uses only the known
// placeholders and exactly one {SEQ:n}.
func ValidNumberingTemplate(t string) bool {
	if t == "" || utf8.RuneCountInString(t) > MaxTemplateLength {
		return false
	}

	seq := 0

	for _, token := range templateToken.FindAllString(t, -1) {
		switch {
		case seqToken.MatchString(token):
			seq++
		case !allowedTemplates[token]:
			return false
		}
	}

	rest := templateToken.ReplaceAllString(t, "")

	return seq == 1 && !strings.ContainsAny(rest, "{}")
}
