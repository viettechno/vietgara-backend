package system_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	supplierv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/supplier/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// TestSearchIgnoresDiacritics: garage staff often type without the marks, so
// "nguyen van anh" finds "Nguyễn Văn Ánh" and "dai phat" finds "Đại Phát" in
// every list that has a search box; the marks typed the full way and the case
// still match, and a different name still does not.
//
// Scenario: E2E-SYS-012: a search typed without Vietnamese diacritics finds
// customers, vehicles, suppliers, parts and jobs written with them.
func TestSearchIgnoresDiacritics(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	business := fixture.NewBusiness(t, "diacritics")
	api := business.Owner.API
	garageID := business.GarageID

	customer, err := api.CreateCustomer(ctx, garageID, &customerv1.Customer{FullName: "Nguyễn Văn Ánh", PersonalDataConsent: true})
	require.NoError(t, err)

	vehicle, err := api.CreateVehicle(ctx, garageID, &customerv1.Vehicle{
		CustomerId: customer.GetId(), LicensePlate: "51A-77777", Make: new("Hyundai"), Model: new("Thể thao Đặc biệt"),
	})
	require.NoError(t, err)

	supplier, err := api.CreateSupplier(ctx, garageID, &supplierv1.Supplier{Name: "Công ty Phụ tùng Đại Phát"})
	require.NoError(t, err)

	unit := fixture.NewUnit(t, api, garageID, "Lít", "l")
	part := fixture.NewPart(t, api, garageID, "SKU-DIAC-1", "Dầu nhớt tổng hợp", unit)

	job, err := api.CreateJob(ctx, garageID, &cataloguev1.Job{Name: "Thay dầu động cơ", Category: "Bảo dưỡng"})
	require.NoError(t, err)

	searches := []struct {
		what   string
		wantID string
		find   func(query string) ([]string, error)
	}{
		{"customer", customer.GetId(), func(q string) ([]string, error) {
			found, err := api.SearchCustomers(ctx, garageID, q)
			return ids(found, (*customerv1.Customer).GetId), err
		}},
		{"vehicle", vehicle.GetId(), func(q string) ([]string, error) {
			found, err := api.SearchVehicles(ctx, garageID, q)
			return ids(found, (*customerv1.Vehicle).GetId), err
		}},
		{"supplier", supplier.GetId(), func(q string) ([]string, error) {
			found, err := api.SearchSuppliers(ctx, garageID, q)
			return ids(found, (*supplierv1.Supplier).GetId), err
		}},
		{"part", part.GetId(), func(q string) ([]string, error) {
			found, err := api.SearchParts(ctx, garageID, q)
			return ids(found, (*cataloguev1.Part).GetId), err
		}},
		{"job", job.GetId(), func(q string) ([]string, error) {
			found, err := api.ListJobs(ctx, garageID, url.Values{"query": {q}})
			return ids(found, (*cataloguev1.Job).GetId), err
		}},
	}

	// The queries differ per kind: without marks, with them, in capitals.
	queries := map[string][]string{
		// The third is "Nguyễn" typed with combining marks, as a Mac keyboard can.
		"customer": {"nguyen van anh", "Nguyễn Văn Ánh", "NGUYEN VAN", "Nguye\u0302\u0303n"},
		"vehicle":  {"the thao dac biet", "Thể thao Đặc biệt", "THE THAO"},
		"supplier": {"dai phat", "Đại Phát", "PHU TUNG"},
		"part":     {"dau nhot", "Dầu nhớt", "TONG HOP"},
		"job":      {"thay dau dong co", "Bảo dưỡng", "bao duong"},
	}

	for _, s := range searches {
		for _, query := range queries[s.what] {
			got, err := s.find(query)
			require.NoError(t, err, "%s: %q", s.what, query)
			assert.Contains(t, got, s.wantID, "%s: %q finds it", s.what, query)
		}

		// A different name is still a different name.
		got, err := s.find("zzz khong co")
		require.NoError(t, err)
		assert.Empty(t, got, "%s: an unrelated search finds nothing", s.what)
	}
}

// ids maps a list to the id of each item.
func ids[T any](items []T, id func(T) string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, id(item))
	}

	return out
}
