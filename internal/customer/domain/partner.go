package domain

import "time"

// PartnerType classifies a partner of the garage.
type PartnerType string

// Valid PartnerType values.
const (
	PartnerTypeSupplier PartnerType = "supplier"
	PartnerTypeInsurer  PartnerType = "insurer"
)

// IsValid reports whether t is one of the defined PartnerType values.
func (t PartnerType) IsValid() bool {
	switch t {
	case PartnerTypeSupplier, PartnerTypeInsurer:
		return true
	}

	return false
}

// PartnerContact is how the garage reaches a partner; persisted as the
// partners.contact_info JSON document.
type PartnerContact struct {
	ContactPerson *string `json:"contact_person,omitempty"`
	Phone         *string `json:"phone,omitempty"`
	Email         *string `json:"email,omitempty"`
	Address       *string `json:"address,omitempty"`
}

// Partner is a parts supplier or insurance company the garage works with
// (FR-CUS-04). See: Database Design Section 3.3.
type Partner struct {
	ID        string
	GarageID  string
	Type      PartnerType
	Name      string
	TaxCode   *string
	Contact   PartnerContact
	Note      *string
	CreatedBy *string
	UpdatedBy *string
	CreatedAt time.Time
	UpdatedAt time.Time
}
