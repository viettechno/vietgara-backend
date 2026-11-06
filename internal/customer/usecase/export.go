package usecase

import (
	"context"
	"time"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
)

// maxExportedVehicles bounds the vehicles of one customer a view lists.
const maxExportedVehicles = 1000

// WithTaxProfiles lets Export list the customer's tax profiles.
func WithTaxProfiles(p TaxProfileRepository) Option {
	return func(o *options) {
		o.profiles = p
	}
}

// DataExport is what the garage holds about one customer: the answer to a view
// request (FR-CUS-05). It is built when asked for and never stored.
type DataExport struct {
	Customer    ExportedCustomer     `json:"customer"`
	Vehicles    []ExportedVehicle    `json:"vehicles"`
	TaxProfiles []ExportedTaxProfile `json:"taxProfiles"`
	ConsentLog  []ExportedConsent    `json:"consentLog"`
}

// ExportedCustomer is the customer of a DataExport.
type ExportedCustomer struct {
	ID        string    `json:"id"`
	FullName  string    `json:"fullName"`
	Phone     string    `json:"phone,omitempty"`
	Email     string    `json:"email,omitempty"`
	Address   string    `json:"address,omitempty"`
	Locale    string    `json:"locale"`
	CreatedAt time.Time `json:"createdAt"`
}

// ExportedVehicle is a vehicle of a DataExport.
type ExportedVehicle struct {
	ID           string `json:"id"`
	LicensePlate string `json:"licensePlate"`
	Make         string `json:"make,omitempty"`
	Model        string `json:"model,omitempty"`
	Year         int    `json:"year,omitempty"`
	VIN          string `json:"vin,omitempty"`
	EngineNumber string `json:"engineNumber,omitempty"`
}

// ExportedTaxProfile is a tax profile of a DataExport.
type ExportedTaxProfile struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	LegalName    string `json:"legalName"`
	TaxID        string `json:"taxId,omitempty"`
	Address      string `json:"address,omitempty"`
	InvoiceEmail string `json:"invoiceEmail,omitempty"`
}

// ExportedConsent is one decision of the consent log of a DataExport.
type ExportedConsent struct {
	Purpose    string    `json:"purpose,omitempty"`
	Granted    bool      `json:"granted"`
	RecordedAt time.Time `json:"recordedAt"`
}

// Export gathers what the garage holds about the customer.
func (uc *CustomerUseCase) Export(ctx context.Context, garageID, customerID string) (*DataExport, error) {
	const op = "export customer data"

	c, err := uc.customers.FindByID(ctx, garageID, customerID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if c.IsAnonymized() {
		return nil, toAppError(op, domain.ErrCustomerAnonymized)
	}

	out := &DataExport{
		Customer: ExportedCustomer{
			ID: c.ID, FullName: c.FullName, Phone: deref(c.Phone), Email: deref(c.Email), Address: deref(c.Address),
			Locale: c.Locale, CreatedAt: c.CreatedAt,
		},
		Vehicles: []ExportedVehicle{}, TaxProfiles: []ExportedTaxProfile{}, ConsentLog: []ExportedConsent{},
	}

	for _, r := range c.ConsentLog {
		out.ConsentLog = append(out.ConsentLog, ExportedConsent{Purpose: r.Purpose, Granted: r.Granted, RecordedAt: r.RecordedAt})
	}

	vehicles, _, err := uc.vehicles.List(ctx, garageID, VehicleFilter{
		ListFilter: ListFilter{Limit: maxExportedVehicles, IncludeInactive: true}, CustomerID: customerID,
	})
	if err != nil {
		return nil, toAppError(op, err)
	}

	for _, v := range vehicles {
		year := 0
		if v.Year != nil {
			year = *v.Year
		}

		out.Vehicles = append(out.Vehicles, ExportedVehicle{
			ID: v.ID, LicensePlate: v.LicensePlate, Make: deref(v.Make), Model: deref(v.Model), Year: year,
			VIN: deref(v.VIN), EngineNumber: deref(v.EngineNumber),
		})
	}

	if uc.opts.profiles == nil {
		return out, nil
	}

	profiles, err := uc.opts.profiles.List(ctx, garageID, customerID, true)
	if err != nil {
		return nil, toAppError(op, err)
	}

	for _, p := range profiles {
		out.TaxProfiles = append(out.TaxProfiles, ExportedTaxProfile{
			ID: p.ID, Kind: string(p.Kind), LegalName: p.LegalName, TaxID: deref(p.TaxID), Address: deref(p.Address), InvoiceEmail: deref(p.InvoiceEmail),
		})
	}

	return out, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}
