package render_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/render"
)

func TestAmountInWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		amount int64
		want   string
	}{
		{0, "Không đồng chẵn"},
		{1, "Một đồng chẵn"},
		{5, "Năm đồng chẵn"},
		{10, "Mười đồng chẵn"},
		{15, "Mười lăm đồng chẵn"},
		{21, "Hai mươi mốt đồng chẵn"},
		{25, "Hai mươi lăm đồng chẵn"},
		{105, "Một trăm lẻ năm đồng chẵn"},
		{110, "Một trăm mười đồng chẵn"},
		{1_000, "Một nghìn đồng chẵn"},
		{1_005, "Một nghìn không trăm lẻ năm đồng chẵn"},
		{500_000, "Năm trăm nghìn đồng chẵn"},
		{1_234_567, "Một triệu hai trăm ba mươi bốn nghìn năm trăm sáu mươi bảy đồng chẵn"},
		{1_000_000, "Một triệu đồng chẵn"},
		{2_000_300, "Hai triệu ba trăm đồng chẵn"},
		{3_000_000_000, "Ba tỷ đồng chẵn"},
		{-1_000, "Một nghìn đồng chẵn"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, render.AmountInWords(tt.amount), "%d", tt.amount)
	}
}

func TestPaymentVoucherPDF(t *testing.T) {
	t.Parallel()

	data := printmodel.PaymentVoucher{
		Garage: printmodel.GarageHeader{Name: "Công ty TNHH Gara Ánh Dương", Address: "12 Nguyễn Huệ, Quận 1"},
		Number: "PT-202610-0001", Date: "12/10/2026", PayerName: "Nguyễn Thị Bích Ngọc", Reason: "Thanh toán quyết toán QT-1",
		Amount: 1_500_000, Method: "Tiền mặt", SettlementNumber: "QT-1", CollectorName: "Lê Thu Ngân",
	}

	pdf, err := a4(data)
	require.NoError(t, err)
	assert.Equal(t, "%PDF", string(pdf[:4]))

	data.Voided, data.VoidReason = true, "nhập nhầm"
	voided, err := a4(data)
	require.NoError(t, err)
	assert.NotEqual(t, len(pdf), len(voided), "a voided voucher is marked")
}

func TestPaymentOutVoucherPDF(t *testing.T) {
	t.Parallel()

	data := printmodel.PaymentOutVoucher{
		Garage: printmodel.GarageHeader{Name: "Công ty TNHH Gara Ánh Dương", Address: "12 Nguyễn Huệ, Quận 1"},
		Number: "PC-2026-00001", Date: "12/10/2026", PayeeName: "Nhà cung cấp Sao Mai", Reason: "Thanh toán tiền hàng",
		Amount: 1_500_000, Method: "Chuyển khoản", IssuerName: "Lê Thu Ngân",
		Receipts: []printmodel.PaymentOutReceiptRow{{Number: "NK-1", Amount: 1_000_000}, {Number: "NK-2", Amount: 500_000}},
	}

	pdf, err := a4(data)
	require.NoError(t, err)
	assert.Equal(t, "%PDF", string(pdf[:4]))

	data.Voided, data.VoidReason = true, "chi nhầm"
	voided, err := a4(data)
	require.NoError(t, err)
	assert.NotEqual(t, len(pdf), len(voided), "a voided voucher is marked")
}

func TestPaymentOutVoucherPDF_ManyReceiptsKeepSignaturesTogether(t *testing.T) {
	t.Parallel()

	data := printmodel.PaymentOutVoucher{
		Garage: printmodel.GarageHeader{Name: "Công ty TNHH Gara Ánh Dương"}, Number: "PC-2026-00002", Date: "12/10/2026",
		PayeeName: "Nhà cung cấp Sao Mai", Reason: "Thanh toán tiền hàng", Amount: 40_000_000, Method: "Tiền mặt", IssuerName: "Lê Thu Ngân",
	}

	one, err := a4(data)
	require.NoError(t, err)

	for i := range 40 {
		data.Receipts = append(data.Receipts, printmodel.PaymentOutReceiptRow{Number: fmt.Sprintf("NK-%d", i+1), Amount: 1_000_000})
	}

	many, err := a4(data)
	require.NoError(t, err)

	pages := func(pdf []byte) int { return bytes.Count(pdf, []byte("/Type /Page\n")) }
	assert.Equal(t, 1, pages(one))
	assert.Greater(t, pages(many), 1, "a long list runs onto a second page without breaking the render")
}

func TestPurchaseOrderPDF(t *testing.T) {
	t.Parallel()

	data := printmodel.PurchaseOrder{
		Garage: printmodel.GarageHeader{Name: "Công ty TNHH Gara Ánh Dương"}, Number: "DH-2026-00001", Status: "sent", Date: "12/10/2026",
		ExpectedDate: "20/10/2026", SupplierName: "Nhà cung cấp Sao Mai", Notes: "Giao trong giờ hành chính",
		Lines:    []printmodel.PurchaseOrderRow{{SKU: "OIL", Name: "Dầu nhớt", Quantity: "16", Unit: "L", UnitCost: 90_000, Amount: 1_440_000}},
		ShowCost: true, TotalAmount: 1_440_000,
	}

	priced, err := a4(data)
	require.NoError(t, err)
	assert.Equal(t, "%PDF", string(priced[:4]))

	data.ShowCost, data.ExpectedDate, data.Notes = false, "", ""
	plain, err := a4(data)
	require.NoError(t, err)
	assert.NotEqual(t, len(priced), len(plain), "a caller without the cost permission gets no prices")
}
