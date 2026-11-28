package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
)

func TestJobDocument(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		req  *documentv1.CreatePrintJobRequest
		want usecase.JobDocument
	}{
		{&documentv1.CreatePrintJobRequest{Document: &documentv1.CreatePrintJobRequest_QuoteId{QuoteId: "q"}}, usecase.JobDocument{Type: printmodel.TypeQuote, ID: "q"}},
		{&documentv1.CreatePrintJobRequest{Document: &documentv1.CreatePrintJobRequest_RepairOrderId{RepairOrderId: "r"}}, usecase.JobDocument{Type: printmodel.TypeRepairOrder, ID: "r"}},
		{&documentv1.CreatePrintJobRequest{Document: &documentv1.CreatePrintJobRequest_SettlementId{SettlementId: "s"}}, usecase.JobDocument{Type: printmodel.TypeSettlement, ID: "s"}},
		{
			&documentv1.CreatePrintJobRequest{Document: &documentv1.CreatePrintJobRequest_PaymentVoucher{PaymentVoucher: &documentv1.PrintJobPaymentVoucher{SettlementId: "s", PaymentId: "p"}}},
			usecase.JobDocument{Type: printmodel.TypePaymentVoucher, ID: "s", PaymentID: "p"},
		},
		{&documentv1.CreatePrintJobRequest{Document: &documentv1.CreatePrintJobRequest_GoodsReceiptId{GoodsReceiptId: "g"}}, usecase.JobDocument{Type: printmodel.TypeGoodsReceipt, ID: "g"}},
		{&documentv1.CreatePrintJobRequest{Document: &documentv1.CreatePrintJobRequest_StockIssueRepairOrderId{StockIssueRepairOrderId: "r"}}, usecase.JobDocument{Type: printmodel.TypeStockIssue, ID: "r"}},
		{&documentv1.CreatePrintJobRequest{Document: &documentv1.CreatePrintJobRequest_PaymentOutVoucherId{PaymentOutVoucherId: "v"}}, usecase.JobDocument{Type: printmodel.TypePaymentOut, ID: "v"}},
		{&documentv1.CreatePrintJobRequest{Document: &documentv1.CreatePrintJobRequest_PurchaseOrderId{PurchaseOrderId: "o"}}, usecase.JobDocument{Type: printmodel.TypePurchaseOrder, ID: "o"}},
		{
			&documentv1.CreatePrintJobRequest{Document: &documentv1.CreatePrintJobRequest_PartLabels{PartLabels: &documentv1.PrintJobPartLabels{Parts: []*documentv1.PartLabelCopies{{PartId: "p", Copies: 2}}}}},
			usecase.JobDocument{Type: printmodel.TypePartLabel, Labels: []usecase.LabelCopies{{PartID: "p", Copies: 2}}},
		},
		{&documentv1.CreatePrintJobRequest{Document: &documentv1.CreatePrintJobRequest_TestPage{TestPage: &documentv1.PrintJobTestPage{}}}, usecase.JobDocument{TestPage: true}},
	} {
		got, err := jobDocument(c.req)
		require.NoError(t, err)
		assert.Equal(t, c.want, got)
	}

	_, err := jobDocument(&documentv1.CreatePrintJobRequest{})
	require.Error(t, err)
}

func TestJobToProto(t *testing.T) {
	t.Parallel()

	assert.Equal(t, documentv1.DocumentType_DOCUMENT_TYPE_PART_LABEL, jobToProto(usecase.PrintJob{DocumentType: printmodel.TypePartLabel}).GetDocumentType())
	assert.Equal(t, documentv1.DocumentType_DOCUMENT_TYPE_QUOTE, jobToProto(usecase.PrintJob{DocumentType: printmodel.TypeQuote}).GetDocumentType())
	assert.Equal(t, documentv1.DocumentType_DOCUMENT_TYPE_UNSPECIFIED, jobToProto(usecase.PrintJob{}).GetDocumentType())
	assert.Equal(t, documentv1.PrintJobStatus_PRINT_JOB_STATUS_EXPIRED, jobToProto(usecase.PrintJob{Status: usecase.JobExpired}).GetStatus())
}
