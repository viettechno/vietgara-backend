package handler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/internal/document/handler"
	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/issuer"
)

func TestPrintPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()

	for method, permission := range map[string]access.Permission{
		documentv1.DocumentService_PrintQuotePdf_FullMethodName:             access.QuoteRead,
		documentv1.DocumentService_PrintRepairOrderPdf_FullMethodName:       access.RepairOrderRead,
		documentv1.DocumentService_PrintSettlementReceiptPdf_FullMethodName: access.BillingRead,
		documentv1.DocumentService_PrintPaymentVoucherPdf_FullMethodName:    access.BillingRead,
		documentv1.DocumentService_PrintGoodsReceiptPdf_FullMethodName:      access.InventoryRead,
		documentv1.DocumentService_PrintStockIssuePdf_FullMethodName:        access.InventoryRead,
		documentv1.DocumentService_UpdatePrintSetting_FullMethodName:        access.GarageWrite,
	} {
		require.NotNil(t, p[method].Garage, method)
		assert.Equal(t, permission, p[method].Garage.Permission, method)
		assert.False(t, p[method].Idempotent, "a print is not an idempotent creation: %s", method)
	}

	assert.NotNil(t, p[documentv1.DocumentService_GetPrintSettings_FullMethodName].Garage)
}

func TestPrintEveryDocument(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.garages.EXPECT().Get(anyArg, garageID).Return(defaultGarage(), nil).Times(4)
	e.quotes.EXPECT().Get(anyArg, garageID, quoteID).Return(usecase.QuoteInfo{Number: "BG-1"}, nil).Once()
	e.repairOrders.EXPECT().Get(anyArg, garageID, repairOrderID).Return(usecase.RepairOrderInfo{Number: "LSC-1"}, nil).Once()
	e.settlements.EXPECT().Get(anyArg, garageID, settlementID).Return(usecase.SettlementInfo{
		Number:   "QT-1",
		Vouchers: []usecase.VoucherInfo{{ID: "p-1", Number: "PT-1", Issuer: issuer.Issuer{Name: "Công ty An Phát"}}},
		Issuer:   issuer.Issuer{Name: "Công ty An Phát"},
	}, nil).Twice()
	e.inventory.EXPECT().GoodsReceipt(anyArg, garageID, "r-1").Return(usecase.GoodsReceiptInfo{Number: "PN-1"}, nil).Once()
	e.inventory.EXPECT().StockIssue(anyArg, garageID, repairOrderID).Return(usecase.StockIssueInfo{Number: "PX-1"}, nil).Once()

	ctx := t.Context()

	for name, call := range map[string]func() (string, error){
		"quote": func() (string, error) {
			r, err := e.srv.PrintQuotePdf(ctx, &documentv1.PrintQuotePdfRequest{GarageId: garageID, QuoteId: quoteID})
			return string(r.GetData()), err
		},
		"repair order": func() (string, error) {
			r, err := e.srv.PrintRepairOrderPdf(ctx, &documentv1.PrintRepairOrderPdfRequest{GarageId: garageID, RepairOrderId: repairOrderID})
			return string(r.GetData()), err
		},
		"settlement": func() (string, error) {
			r, err := e.srv.PrintSettlementReceiptPdf(ctx, &documentv1.PrintSettlementReceiptPdfRequest{GarageId: garageID, SettlementId: settlementID})
			return string(r.GetData()), err
		},
		"voucher": func() (string, error) {
			r, err := e.srv.PrintPaymentVoucherPdf(ctx, &documentv1.PrintPaymentVoucherPdfRequest{GarageId: garageID, SettlementId: settlementID, PaymentId: "p-1"})
			return string(r.GetData()), err
		},
		"goods receipt": func() (string, error) {
			r, err := e.srv.PrintGoodsReceiptPdf(ctx, &documentv1.PrintGoodsReceiptPdfRequest{GarageId: garageID, GoodsReceiptId: "r-1"})
			return string(r.GetData()), err
		},
		"stock issue": func() (string, error) {
			r, err := e.srv.PrintStockIssuePdf(ctx, &documentv1.PrintStockIssuePdfRequest{GarageId: garageID, RepairOrderId: repairOrderID})
			return string(r.GetData()), err
		},
	} {
		data, err := call()
		require.NoError(t, err, name)
		assert.Equal(t, "%PDF", data[:4], name)
	}

	require.Len(t, e.prints.log, 6, "every print is logged once")

	for _, entry := range e.prints.log {
		if entry.Type == printmodel.TypePaymentVoucher {
			assert.Equal(t, "p-1", entry.DocumentID, "a voucher is logged under its payment")
		}
	}
}

func TestPrintErrors(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	gone := apperror.New(apperror.CodeNotFound, "gone")
	e.quotes.EXPECT().Get(anyArg, garageID, quoteID).Return(usecase.QuoteInfo{}, gone).Once()
	e.repairOrders.EXPECT().Get(anyArg, garageID, repairOrderID).Return(usecase.RepairOrderInfo{}, gone).Once()
	e.settlements.EXPECT().Get(anyArg, garageID, settlementID).Return(usecase.SettlementInfo{}, gone).Times(2)
	e.inventory.EXPECT().GoodsReceipt(anyArg, garageID, "r-1").Return(usecase.GoodsReceiptInfo{}, gone).Once()
	e.inventory.EXPECT().StockIssue(anyArg, garageID, repairOrderID).Return(usecase.StockIssueInfo{}, gone).Once()

	ctx := t.Context()

	_, err := e.srv.PrintQuotePdf(ctx, &documentv1.PrintQuotePdfRequest{GarageId: garageID, QuoteId: quoteID})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = e.srv.PrintRepairOrderPdf(ctx, &documentv1.PrintRepairOrderPdfRequest{GarageId: garageID, RepairOrderId: repairOrderID})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = e.srv.PrintSettlementReceiptPdf(ctx, &documentv1.PrintSettlementReceiptPdfRequest{GarageId: garageID, SettlementId: settlementID})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = e.srv.PrintPaymentVoucherPdf(ctx, &documentv1.PrintPaymentVoucherPdfRequest{GarageId: garageID, SettlementId: settlementID, PaymentId: "p-1"})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = e.srv.PrintGoodsReceiptPdf(ctx, &documentv1.PrintGoodsReceiptPdfRequest{GarageId: garageID, GoodsReceiptId: "r-1"})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = e.srv.PrintStockIssuePdf(ctx, &documentv1.PrintStockIssuePdfRequest{GarageId: garageID, RepairOrderId: repairOrderID})
	assert.Equal(t, codes.NotFound, status.Code(err))

	assert.Empty(t, e.prints.log, "a print that failed is not logged")
}

func TestPrintSettingsRPCs(t *testing.T) {
	t.Parallel()

	t.Run("get returns every document type with defaults", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)

		resp, err := e.srv.GetPrintSettings(t.Context(), &documentv1.GetPrintSettingsRequest{GarageId: garageID})
		require.NoError(t, err)
		require.Len(t, resp.GetSettings(), len(printmodel.Types()))
		assert.Equal(t, documentv1.DocumentType_DOCUMENT_TYPE_QUOTE, resp.GetSettings()[0].GetDocumentType())
		assert.Equal(t, documentv1.PaperSize_PAPER_SIZE_A4, resp.GetSettings()[0].GetPaperSize())
		assert.Equal(t, []documentv1.PaperSize{documentv1.PaperSize_PAPER_SIZE_A4, documentv1.PaperSize_PAPER_SIZE_A5}, resp.GetSettings()[0].GetAvailablePaperSizes())
	})

	t.Run("get fails when the store does", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.prints.err = errDocumentTestBoom

		_, err := e.srv.GetPrintSettings(t.Context(), &documentv1.GetPrintSettingsRequest{GarageId: garageID})
		assert.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("update saves and answers the setting", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)

		got, err := e.srv.UpdatePrintSetting(t.Context(), &documentv1.UpdatePrintSettingRequest{
			GarageId: garageID, DocumentType: documentv1.DocumentType_DOCUMENT_TYPE_PAYMENT_VOUCHER,
			PaperSize: documentv1.PaperSize_PAPER_SIZE_A5, CopyMark: true,
		})
		require.NoError(t, err)
		assert.Equal(t, documentv1.PaperSize_PAPER_SIZE_A5, got.GetPaperSize())
		assert.True(t, got.GetCopyMark())
		assert.Equal(t, usecase.PrintSetting{Paper: printmodel.A5, CopyMark: true}, e.prints.settings[printmodel.TypePaymentVoucher])
	})

	t.Run("update refuses what it cannot print", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)

		for name, req := range map[string]*documentv1.UpdatePrintSettingRequest{
			"unspecified type": {GarageId: garageID, PaperSize: documentv1.PaperSize_PAPER_SIZE_A4},
			"unspecified size": {GarageId: garageID, DocumentType: documentv1.DocumentType_DOCUMENT_TYPE_QUOTE},
			"no A5 template": {
				GarageId: garageID, DocumentType: documentv1.DocumentType_DOCUMENT_TYPE_REPAIR_ORDER, PaperSize: documentv1.PaperSize_PAPER_SIZE_A5,
			},
		} {
			_, err := e.srv.UpdatePrintSetting(t.Context(), req)
			assert.Equal(t, codes.InvalidArgument, status.Code(err), name)
		}

		assert.Empty(t, e.prints.settings)
	})
}
