package render_test

import (
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
