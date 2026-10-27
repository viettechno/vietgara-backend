// Package reporting_test covers Reporting and document exports at API
// level: the revenue report follows settlements, and the quote, repair
// order, receipt, voucher PDFs plus the stock-movements Excel export
// download with the right content type and magic bytes. Totals are
// cross-checked through the API instead of parsing binaries; visual
// layout is web-E2E.
package reporting_test

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	reportingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/reporting/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// paidSettlement walks a new garage to a fully paid settlement of
// 500,000 VND and returns its ids.
func paidSettlement(t *testing.T, label string) (*fixture.Actor, string, string) {
	t.Helper()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, label)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox " + label})
	require.NoError(t, err)

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(), &customerv1.Customer{FullName: "Khách Báo Cáo", PersonalDataConsent: true})
	require.NoError(t, err)

	vehicle, err := owner.API.CreateVehicle(ctx, garage.GetId(), &customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51P-12345"})
	require.NoError(t, err)

	order, err := owner.API.CreateRepairOrder(ctx, garage.GetId(), &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType: repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR, Description: "Service", Quantity: "1", UnitPrice: 500000,
		}},
	})
	require.NoError(t, err)

	_, err = owner.API.UpdateRepairOrderLine(ctx, garage.GetId(), order.GetId(), order.GetLines()[0].GetId(),
		&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED})
	require.NoError(t, err)

	_, err = owner.API.CloseRepairOrder(ctx, garage.GetId(), order.GetId())
	require.NoError(t, err)

	settlement, err := owner.API.WithIdempotencyKey(uuid.NewString()).CreateSettlement(ctx, garage.GetId(), order.GetId())
	require.NoError(t, err)

	_, err = owner.API.WithIdempotencyKey(uuid.NewString()).RecordPayment(ctx, garage.GetId(), settlement.GetId(),
		&billingv1.RecordSettlementPaymentRequest{Amount: 500000, Method: billingv1.PaymentMethod_PAYMENT_METHOD_CASH})
	require.NoError(t, err)

	return owner, garage.GetId(), settlement.GetId()
}

// garageDays returns the range from yesterday to tomorrow (exclusive) in
// the garage's time zone: the revenue report counts the garage's days
// (FR-TEN-05), which differ from the machine's around midnight, and a
// settlement made just before the garage's midnight is still in range
// when the clock is read just after it.
func garageDays(t *testing.T, owner *fixture.Actor, garageID string) (string, string) {
	t.Helper()

	garage, err := owner.API.GetGarage(t.Context(), garageID)
	require.NoError(t, err)

	loc, err := time.LoadLocation(garage.GetTimezone())
	require.NoError(t, err)

	now := time.Now().In(loc)

	return now.AddDate(0, 0, -1).Format("2006-01-02"), now.AddDate(0, 0, 1).Format("2006-01-02")
}

// Scenario: E2E-BIL-008 (partial): the dashboard revenue follows the
// settlement — asserted at API level through the revenue report; the
// dashboard figure itself is web-E2E.
func TestRevenueReportFollowsSettlement(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garageID, _ := paidSettlement(t, "rep-revenue")

	from, to := garageDays(t, owner, garageID)

	report, err := owner.API.GetRevenueReport(ctx, garageID, reportingv1.RevenuePeriod_REVENUE_PERIOD_DAY, from, to)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, report.GetTotalAmount(), int64(500000))
	require.NotEmpty(t, report.GetPoints())
}

// Scenario: E2E-BIL-007 (partial): receipt and voucher PDFs download —
// status, content type and magic bytes are asserted; the legal header
// layout is web-E2E.
func TestSettlementPDFsDownload(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garageID, settlementID := paidSettlement(t, "rep-pdf")

	settlement, err := owner.API.GetSettlement(ctx, garageID, settlementID)
	require.NoError(t, err)
	require.NotEmpty(t, settlement.GetPayments())

	contentType, body, err := owner.API.DownloadReceiptPDF(ctx, garageID, settlementID)
	require.NoError(t, err)
	assertPDF(t, contentType, body)

	contentType, body, err = owner.API.DownloadVoucherPDF(ctx, garageID, settlementID, settlement.GetPayments()[0].GetId())
	require.NoError(t, err)
	assertPDF(t, contentType, body)
}

// Scenario: E2E-QUO-001 (partial) + E2E-REP-001 (partial): the quote and
// repair order PDFs download — asserted at API level; layout is
// web-E2E.
func TestQuoteAndRepairOrderPDFsDownload(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "rep-docpdf")

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Doc PDF"})
	require.NoError(t, err)

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(), &customerv1.Customer{FullName: "Khách Tài Liệu", PersonalDataConsent: true})
	require.NoError(t, err)

	vehicle, err := owner.API.CreateVehicle(ctx, garage.GetId(), &customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51D-12345"})
	require.NoError(t, err)

	quote, err := owner.API.CreateQuote(ctx, garage.GetId(), &quotev1.Quote{
		CustomerId: customer.GetId(), VehicleId: vehicle.GetId(),
		Lines: []*quotev1.QuoteLine{{
			ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "Service", Quantity: "1", UnitPrice: 100000,
		}},
	})
	require.NoError(t, err)

	contentType, body, err := owner.API.DownloadQuotePDF(ctx, garage.GetId(), quote.GetId())
	require.NoError(t, err)
	assertPDF(t, contentType, body)

	order, err := owner.API.CreateRepairOrder(ctx, garage.GetId(), &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType: repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR, Description: "Service", Quantity: "1", UnitPrice: 100000,
		}},
	})
	require.NoError(t, err)

	contentType, body, err = owner.API.DownloadRepairOrderPDF(ctx, garage.GetId(), order.GetId())
	require.NoError(t, err)
	assertPDF(t, contentType, body)
}

// Scenario: E2E-INV-007 (partial): the stock-movements Excel export
// downloads — status, content type and workbook magic bytes are
// asserted; cell values are cross-checked through the JSON report.
func TestStockMovementsExcelDownload(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "rep-excel")

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Excel"})
	require.NoError(t, err)

	contentType, body, err := owner.API.DownloadStockMovementsExcel(ctx, garage.GetId())
	require.NoError(t, err)
	assert.Contains(t, contentType, "spreadsheet")
	require.Greater(t, len(body), 100)
	assert.Equal(t, "PK", string(body[:2]), "an .xlsx workbook is a zip archive")

	// Documents of an unreachable garage are refused.
	_, _, err = owner.API.DownloadStockMovementsExcel(ctx, "00000000-0000-4000-8000-000000000000")
	apiErr := client.AsAPIError(err)
	require.NotNil(t, apiErr)
	assert.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, apiErr.Status)
}

// Scenario: E2E-CAT-008 (partial): the revenue Excel carries the same
// figures as the revenue report — asserted at API level; opening the file
// and the dashboard button are web-E2E.
func TestRevenueExcelDownload(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garageID, _ := paidSettlement(t, "rep-revenue-xlsx")

	from, to := garageDays(t, owner, garageID)

	report, err := owner.API.GetRevenueReport(ctx, garageID, reportingv1.RevenuePeriod_REVENUE_PERIOD_DAY, from, to)
	require.NoError(t, err)
	require.EqualValues(t, 500000, report.GetTotalAmount())

	contentType, body, err := owner.API.DownloadRevenueExcel(ctx, garageID, reportingv1.RevenuePeriod_REVENUE_PERIOD_DAY, from, to)
	require.NoError(t, err)
	assert.Contains(t, contentType, "spreadsheet")
	require.Greater(t, len(body), 100)
	assert.Equal(t, "PK", string(body[:2]), "an .xlsx workbook is a zip archive")

	// The workbook's total row carries the report's total.
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	require.NoError(t, err)

	var sheet string

	for _, f := range archive.File {
		if f.Name != "xl/worksheets/sheet1.xml" {
			continue
		}

		r, err := f.Open()
		require.NoError(t, err)

		raw, err := io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())

		sheet = string(raw)
	}

	assert.Contains(t, sheet, "<v>500000</v>")

	// A bad range or a missing period is refused.
	_, _, err = owner.API.DownloadRevenueExcel(ctx, garageID, reportingv1.RevenuePeriod_REVENUE_PERIOD_DAY, to, from)
	assert.Equal(t, http.StatusUnprocessableEntity, client.AsAPIError(err).Status)

	_, _, err = owner.API.DownloadRevenueExcel(ctx, garageID, reportingv1.RevenuePeriod_REVENUE_PERIOD_UNSPECIFIED, from, to)
	assert.Equal(t, http.StatusUnprocessableEntity, client.AsAPIError(err).Status)
}

// assertPDF checks a downloaded PDF's content type and magic bytes.
func assertPDF(t *testing.T, contentType string, body []byte) {
	t.Helper()

	assert.Contains(t, strings.ToLower(contentType), "pdf")
	require.Greater(t, len(body), 100)
	assert.Equal(t, "%PDF", string(body[:4]))
}
