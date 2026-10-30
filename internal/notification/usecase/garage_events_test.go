package usecase_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/notification/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
)

func staffLan() usecase.UserInfo {
	return usecase.UserInfo{ID: creatorID, Email: "lan@example.com", FullName: "Lan", Locale: "en"}
}

func TestHandleQuoteDecided(t *testing.T) {
	t.Parallel()

	payload := `{"quote_id":"` + quoteID + `","number":"BG-001","customer_name":"An","vehicle_plate":"51A-111.11","created_by":"` + creatorID + `"}`

	t.Run("tells the creator and the owner, each in their language", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{})
		d.Staff.EXPECT().Recipients(mock.Anything, garageID, []string{creatorID}, "").
			Return([]usecase.UserInfo{staffLan(), {ID: "owner", Email: "owner@example.com", FullName: "Minh", Locale: "vi"}}, nil).Once()
		d.notSuppressed("lan@example.com")
		d.notSuppressed("owner@example.com")

		var subjects []string

		d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).
			Run(func(_ mockContext, m mail.Message) { subjects = append(subjects, m.Subject) }).Return(nil).Twice()
		rows := d.logged()

		require.NoError(t, uc.HandleQuoteDecided(t.Context(), event(garageID, "quote.approved", payload)))
		assert.Equal(t, []string{"Quote BG-001 was approved", "Báo giá BG-001 đã được duyệt"}, subjects)
		require.Len(t, *rows, 2)
		assert.Equal(t, domain.TypeQuoteDecided, (*rows)[0].Type)
		assert.Equal(t, "quote", (*rows)[0].EntityType)
		assert.Equal(t, "event-1", (*rows)[0].EventID)
	})

	t.Run("a rejection says so", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{})
		d.Staff.EXPECT().Recipients(mock.Anything, garageID, []string{creatorID}, "").Return([]usecase.UserInfo{staffLan()}, nil).Once()
		d.notSuppressed("lan@example.com")

		msg := d.sentMessage()
		d.logged()

		require.NoError(t, uc.HandleQuoteDecided(t.Context(), event(garageID, "quote.rejected", payload)))
		assert.Equal(t, "Quote BG-001 was rejected", msg.Subject)
	})

	t.Run("a switched-off type is logged as skipped", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{Off: map[domain.Type]bool{domain.TypeQuoteDecided: true}})
		d.Staff.EXPECT().Recipients(mock.Anything, garageID, []string{creatorID}, "").Return([]usecase.UserInfo{staffLan()}, nil).Once()
		rows := d.logged()

		require.NoError(t, uc.HandleQuoteDecided(t.Context(), event(garageID, "quote.approved", payload)))
		require.Len(t, *rows, 1)
		assert.Equal(t, domain.StatusSkipped, (*rows)[0].Status)
		assert.Equal(t, domain.SkipDisabled, (*rows)[0].SkipReason)
	})

	t.Run("a suppressed address is skipped and the others still get it", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{})
		d.Staff.EXPECT().Recipients(mock.Anything, garageID, []string{creatorID}, "").
			Return([]usecase.UserInfo{staffLan(), {ID: "owner", Email: "owner@example.com", FullName: "Minh", Locale: "vi"}}, nil).Once()
		d.Suppressions.EXPECT().Suppressed(mock.Anything, "lan@example.com").Return(true, nil).Once()
		d.notSuppressed("owner@example.com")
		d.sentMessage()
		rows := d.logged()

		require.NoError(t, uc.HandleQuoteDecided(t.Context(), event(garageID, "quote.approved", payload)))
		require.Len(t, *rows, 2)
		assert.Equal(t, domain.SkipSuppressed, (*rows)[0].SkipReason)
		assert.Equal(t, domain.StatusSent, (*rows)[1].Status)
	})

	t.Run("a malformed event or one without a garage is dropped", func(t *testing.T) {
		t.Parallel()

		uc, _ := newGarageUseCase(t)

		require.NoError(t, uc.HandleQuoteDecided(t.Context(), event(garageID, "quote.approved", `nope`)))
		require.NoError(t, uc.HandleQuoteDecided(t.Context(), event("", "quote.approved", payload)))
	})

	t.Run("a failure reads back as an error so the event is delivered again", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.Settings.EXPECT().Get(mock.Anything, garageID).Return(domain.Settings{}, errBoom).Once()
		require.ErrorIs(t, uc.HandleQuoteDecided(t.Context(), event(garageID, "quote.approved", payload)), errBoom)

		uc, d = newGarageUseCase(t)
		d.garageReads(domain.Settings{})
		d.Staff.EXPECT().Recipients(mock.Anything, garageID, []string{creatorID}, "").Return(nil, errBoom).Once()
		require.ErrorIs(t, uc.HandleQuoteDecided(t.Context(), event(garageID, "quote.approved", payload)), errBoom)
	})

	t.Run("a garage that is gone drops the event", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.Settings.EXPECT().Get(mock.Anything, garageID).Return(domain.Settings{}, nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageInfo{}, usecase.ErrGarageNotFound).Once()

		require.NoError(t, uc.HandleQuoteDecided(t.Context(), event(garageID, "quote.approved", payload)))
	})
}

func TestHandleVehicleReady(t *testing.T) {
	t.Parallel()

	payload := `{"repair_order_id":"ro-1","vehicle_id":"` + vehicleID + `","number":"RO-1","vehicle_plate":"51A-111.11"}`

	t.Run("e-mails the customer with the opt-out link and headers", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{SenderName: "Gara Minh"})
		d.Customers.EXPECT().ByVehicle(mock.Anything, garageID, vehicleID).Return(customerAn(), nil).Once()
		d.notSuppressed("customer.an@example.com")

		msg := d.sentMessage()
		rows := d.logged()

		require.NoError(t, uc.HandleVehicleReady(t.Context(), event(garageID, "repair_order.completed", payload)))
		assert.Equal(t, "Xe 51A-111.11 của bạn đã sẵn sàng", msg.Subject)
		assert.Equal(t, "Gara Minh", msg.FromName, "the garage's own sender name")
		assert.Equal(t, "gara@example.com", msg.ReplyTo)
		assert.Contains(t, msg.Text, "Địa chỉ: 1 Le Loi")

		unsubscribe := msg.Headers["List-Unsubscribe"]
		require.True(t, strings.HasPrefix(unsubscribe, "<https://app.example.com/api/v1/public/email-opt-out/"+garageID+"."+customerID+"."), unsubscribe)
		assert.Equal(t, "List-Unsubscribe=One-Click", msg.Headers["List-Unsubscribe-Post"])
		assert.Contains(t, msg.Text, strings.Trim(unsubscribe, "<>"))

		require.Len(t, *rows, 1)
		assert.Equal(t, customerID, (*rows)[0].CustomerID)
		assert.Equal(t, "customer.an@example.com", (*rows)[0].Recipient)
	})

	t.Run("the garage's name is the sender when it set no name", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{})
		d.Customers.EXPECT().ByVehicle(mock.Anything, garageID, vehicleID).Return(customerAn(), nil).Once()
		d.notSuppressed("customer.an@example.com")

		msg := d.sentMessage()
		d.logged()

		require.NoError(t, uc.HandleVehicleReady(t.Context(), event(garageID, "repair_order.completed", payload)))
		assert.Equal(t, "Gara Anh Minh", msg.FromName)
	})

	t.Run("skips a customer who opted out, has no address or whose address bounced", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			customer   usecase.CustomerInfo
			suppressed bool
			reason     domain.SkipReason
		}{
			"opted out":  {customer: func() usecase.CustomerInfo { c := customerAn(); c.OptedOut = true; return c }(), reason: domain.SkipOptedOut},
			"no address": {customer: func() usecase.CustomerInfo { c := customerAn(); c.Email = ""; return c }(), reason: domain.SkipNoEmail},
			"suppressed": {customer: customerAn(), suppressed: true, reason: domain.SkipSuppressed},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				uc, d := newGarageUseCase(t)
				d.garageReads(domain.Settings{})
				d.Customers.EXPECT().ByVehicle(mock.Anything, garageID, vehicleID).Return(tc.customer, nil).Once()

				if tc.suppressed {
					d.Suppressions.EXPECT().Suppressed(mock.Anything, "customer.an@example.com").Return(true, nil).Once()
				}

				rows := d.logged()

				require.NoError(t, uc.HandleVehicleReady(t.Context(), event(garageID, "repair_order.completed", payload)))
				require.Len(t, *rows, 1)
				assert.Equal(t, domain.StatusSkipped, (*rows)[0].Status)
				assert.Equal(t, tc.reason, (*rows)[0].SkipReason)
			})
		}
	})

	t.Run("a customer who is gone drops the event", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{})
		d.Customers.EXPECT().ByVehicle(mock.Anything, garageID, vehicleID).Return(usecase.CustomerInfo{}, usecase.ErrCustomerNotFound).Once()

		require.NoError(t, uc.HandleVehicleReady(t.Context(), event(garageID, "repair_order.completed", payload)))
	})

	t.Run("a malformed event is dropped", func(t *testing.T) {
		t.Parallel()

		uc, _ := newGarageUseCase(t)
		require.NoError(t, uc.HandleVehicleReady(t.Context(), event(garageID, "repair_order.completed", `{}`)))
	})
}

func TestHandlePaymentReceived(t *testing.T) {
	t.Parallel()

	partial := `{"settlement_id":"` + settlementID + `","vehicle_id":"` + vehicleID + `","number":"ST-1","vehicle_plate":"51A-111.11","amount":500000,"balance":300000}`

	t.Run("tells the customer what was paid and what is left", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{})
		d.Customers.EXPECT().ByVehicle(mock.Anything, garageID, vehicleID).Return(customerAn(), nil).Once()
		d.notSuppressed("customer.an@example.com")

		msg := d.sentMessage()
		d.logged()

		require.NoError(t, uc.HandlePaymentReceived(t.Context(), event(garageID, "payment_voucher.issued", partial)))
		assert.Equal(t, "Gara Anh Minh đã nhận 500.000 đ cho xe 51A-111.11", msg.Subject)
		assert.Contains(t, msg.Text, "300.000 đ")
	})

	t.Run("the payment that settles the bill is answered by the receipt instead", func(t *testing.T) {
		t.Parallel()

		uc, _ := newGarageUseCase(t)

		last := `{"settlement_id":"` + settlementID + `","vehicle_id":"` + vehicleID + `","amount":800000,"balance":0}`
		require.NoError(t, uc.HandlePaymentReceived(t.Context(), event(garageID, "payment_voucher.issued", last)))
	})
}

func TestHandleSettlementPaid(t *testing.T) { //nolint:funlen // one subtest per case
	t.Parallel()

	payload := `{"settlement_id":"` + settlementID + `","vehicle_id":"` + vehicleID + `","number":"ST-1","customer_name":"An","vehicle_plate":"51A-111.11","total":800000,"created_by":"` + creatorID + `"}`

	expect := func(d *garageDeps) {
		d.garageReads(domain.Settings{})
		d.Customers.EXPECT().ByVehicle(mock.Anything, garageID, vehicleID).Return(customerAn(), nil).Once()
		d.Staff.EXPECT().Recipients(mock.Anything, garageID, []string{creatorID}, "").Return([]usecase.UserInfo{staffLan()}, nil).Once()
		d.notSuppressed("customer.an@example.com")
		d.notSuppressed("lan@example.com")
	}

	t.Run("sends the receipt as a PDF and tells the staff", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		expect(d)
		d.Receipts.EXPECT().SettlementReceipt(mock.Anything, garageID, settlementID).Return([]byte("%PDF-1.4"), nil).Once()

		var sent []mail.Message

		d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).
			Run(func(_ mockContext, m mail.Message) { sent = append(sent, m) }).Return(nil).Twice()
		rows := d.logged()

		require.NoError(t, uc.HandleSettlementPaid(t.Context(), event(garageID, "settlement.paid", payload)))
		require.Len(t, sent, 2)

		assert.Equal(t, "Biên nhận ST-1 cho xe 51A-111.11", sent[0].Subject)
		require.Len(t, sent[0].Attachments, 1)
		assert.Equal(t, "receipt-ST-1.pdf", sent[0].Attachments[0].Name)
		assert.Equal(t, "application/pdf", sent[0].Attachments[0].ContentType)
		assert.Empty(t, sent[0].Headers, "an essential e-mail has no opt-out")

		assert.Equal(t, "Settlement ST-1 is paid in full", sent[1].Subject)
		assert.Empty(t, sent[1].Attachments)

		require.Len(t, *rows, 2)
		assert.Equal(t, domain.TypeSettlementReceipt, (*rows)[0].Type)
		assert.Equal(t, domain.TypeSettlementPaid, (*rows)[1].Type)
		assert.Equal(t, customerID, (*rows)[1].CustomerID)
	})

	t.Run("the receipt ignores a customer's opt-out", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{})

		optedOut := customerAn()
		optedOut.OptedOut = true

		d.Customers.EXPECT().ByVehicle(mock.Anything, garageID, vehicleID).Return(optedOut, nil).Once()
		d.Staff.EXPECT().Recipients(mock.Anything, garageID, []string{creatorID}, "").Return(nil, nil).Once()
		d.notSuppressed("customer.an@example.com")
		d.Receipts.EXPECT().SettlementReceipt(mock.Anything, garageID, settlementID).Return([]byte("%PDF"), nil).Once()
		d.sentMessage()
		d.logged()

		require.NoError(t, uc.HandleSettlementPaid(t.Context(), event(garageID, "settlement.paid", payload)))
	})

	t.Run("goes without the PDF when it cannot be made or is too big", func(t *testing.T) {
		t.Parallel()

		for name, result := range map[string]struct {
			pdf []byte
			err error
		}{
			"render failure": {err: errBoom},
			"too big":        {pdf: make([]byte, mail.MaxAttachmentBytes+1)},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				uc, d := newGarageUseCase(t)
				expect(d)
				d.Receipts.EXPECT().SettlementReceipt(mock.Anything, garageID, settlementID).Return(result.pdf, result.err).Once()

				var sent []mail.Message

				d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).
					Run(func(_ mockContext, m mail.Message) { sent = append(sent, m) }).Return(nil).Twice()
				d.logged()

				require.NoError(t, uc.HandleSettlementPaid(t.Context(), event(garageID, "settlement.paid", payload)))
				assert.Empty(t, sent[0].Attachments)
				assert.Contains(t, sent[0].Text, "không đính kèm được")
			})
		}
	})

	t.Run("a send failure is logged apart and returned for a retry", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{})
		d.Customers.EXPECT().ByVehicle(mock.Anything, garageID, vehicleID).Return(customerAn(), nil).Once()
		d.Staff.EXPECT().Recipients(mock.Anything, garageID, []string{creatorID}, "").Return(nil, nil).Once()
		d.notSuppressed("customer.an@example.com")
		d.Receipts.EXPECT().SettlementReceipt(mock.Anything, garageID, settlementID).Return([]byte("%PDF"), nil).Once()
		d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Return(errBoom).Once()

		var failed *domain.Log

		d.Log.EXPECT().Failed(mock.Anything, mock.Anything).
			Run(func(_ mockContext, l *domain.Log) { failed = l }).Return(nil).Once()

		require.ErrorIs(t, uc.HandleSettlementPaid(t.Context(), event(garageID, "settlement.paid", payload)), errBoom)
		require.NotNil(t, failed)
		assert.Equal(t, domain.StatusFailed, failed.Status)
		assert.Equal(t, "boom", failed.Error)
		assert.Equal(t, garageID, failed.GarageID)
	})

	t.Run("a failure to log the failure does not hide it", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{})
		d.Customers.EXPECT().ByVehicle(mock.Anything, garageID, vehicleID).Return(customerAn(), nil).Once()
		d.Staff.EXPECT().Recipients(mock.Anything, garageID, []string{creatorID}, "").Return(nil, nil).Once()
		d.notSuppressed("customer.an@example.com")
		d.Receipts.EXPECT().SettlementReceipt(mock.Anything, garageID, settlementID).Return([]byte("%PDF"), nil).Once()
		d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Return(errBoom).Once()
		d.Log.EXPECT().Failed(mock.Anything, mock.Anything).Return(errBoom2).Once()

		require.ErrorIs(t, uc.HandleSettlementPaid(t.Context(), event(garageID, "settlement.paid", payload)), errBoom)
	})

	t.Run("a render or suppression error is returned", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{})
		d.Customers.EXPECT().ByVehicle(mock.Anything, garageID, vehicleID).Return(customerAn(), nil).Once()
		d.Staff.EXPECT().Recipients(mock.Anything, garageID, []string{creatorID}, "").Return(nil, nil).Once()
		d.Suppressions.EXPECT().Suppressed(mock.Anything, "customer.an@example.com").Return(false, errBoom).Once()

		require.ErrorIs(t, uc.HandleSettlementPaid(t.Context(), event(garageID, "settlement.paid", payload)), errBoom)
	})
}

func TestHandleStockLow(t *testing.T) {
	t.Parallel()

	payload := `{"part_id":"part-1","part_name":"Lọc dầu","sku":"LD-01","quantity":"3","threshold":"4"}`

	t.Run("tells the owner and the staff who reorder", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{})
		d.Staff.EXPECT().Recipients(mock.Anything, garageID, []string(nil), "inventory.write").Return([]usecase.UserInfo{staffLan()}, nil).Once()
		d.notSuppressed("lan@example.com")

		msg := d.sentMessage()
		rows := d.logged()

		require.NoError(t, uc.HandleStockLow(t.Context(), event(garageID, "stock.low", payload)))
		assert.Equal(t, "Low stock: Lọc dầu", msg.Subject)
		assert.Contains(t, msg.Text, "reorder threshold of 4")
		assert.Equal(t, "part", (*rows)[0].EntityType)
		assert.Equal(t, "part-1", (*rows)[0].EntityID)
	})

	t.Run("a malformed event is dropped", func(t *testing.T) {
		t.Parallel()

		uc, _ := newGarageUseCase(t)
		require.NoError(t, uc.HandleStockLow(t.Context(), event(garageID, "stock.low", `{}`)))
	})
}

func TestQuoteReady(t *testing.T) {
	t.Parallel()

	req := usecase.QuoteReadyRequest{
		GarageID: garageID, QuoteID: quoteID, CustomerID: customerID, Number: "BG-001", VehiclePlate: "51A-111.11",
		Total: "1.000.000", ApproveURL: "https://app.example.com/public/quotes/abc",
	}

	t.Run("e-mails the approval link in the customer's language", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{})

		english := customerAn()
		english.Locale = "en"

		d.Customers.EXPECT().Get(mock.Anything, garageID, customerID).Return(english, nil).Once()
		d.notSuppressed("customer.an@example.com")

		msg := d.sentMessage()
		rows := d.logged()

		assert.True(t, uc.QuoteReady(t.Context(), req))
		assert.Equal(t, "Quote BG-001 for 51A-111.11 needs your approval", msg.Subject)
		assert.Contains(t, msg.Text, req.ApproveURL)
		assert.Empty(t, msg.Headers, "an essential e-mail has no opt-out")
		assert.Equal(t, domain.TypeQuoteReady, (*rows)[0].Type)
	})

	t.Run("reports false for a customer with no address, a switched-off type and any failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{})

		noEmail := customerAn()
		noEmail.Email = ""

		d.Customers.EXPECT().Get(mock.Anything, garageID, customerID).Return(noEmail, nil).Once()
		d.logged()
		assert.False(t, uc.QuoteReady(t.Context(), req))

		uc, d = newGarageUseCase(t)
		d.garageReads(domain.Settings{Off: map[domain.Type]bool{domain.TypeQuoteReady: true}})
		d.Customers.EXPECT().Get(mock.Anything, garageID, customerID).Return(customerAn(), nil).Once()
		d.logged()
		assert.False(t, uc.QuoteReady(t.Context(), req))

		uc, d = newGarageUseCase(t)
		d.garageReads(domain.Settings{})
		d.Customers.EXPECT().Get(mock.Anything, garageID, customerID).Return(usecase.CustomerInfo{}, errBoom).Once()
		assert.False(t, uc.QuoteReady(t.Context(), req))

		uc, d = newGarageUseCase(t)
		d.garageReads(domain.Settings{})
		d.Customers.EXPECT().Get(mock.Anything, garageID, customerID).Return(customerAn(), nil).Once()
		d.notSuppressed("customer.an@example.com")
		d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Return(errBoom).Once()
		d.Log.EXPECT().Failed(mock.Anything, mock.Anything).Return(nil).Once()
		assert.False(t, uc.QuoteReady(t.Context(), req))
	})
}

var _ = mocks.NewMockNotificationMailer

func TestEventsAreNotSentTwice(t *testing.T) {
	t.Parallel()

	payload := `{"repair_order_id":"ro-1","vehicle_id":"` + vehicleID + `","number":"RO-1","vehicle_plate":"51A-111.11"}`

	t.Run("a redelivered event finds its e-mail in the log", func(t *testing.T) {
		t.Parallel()

		_, d := newGarageUseCase(t)
		d.Log = mocks.NewMockNotificationRepository(t)
		uc := rebuild(d)
		d.garageReads(domain.Settings{})
		d.Customers.EXPECT().ByVehicle(mock.Anything, garageID, vehicleID).Return(customerAn(), nil).Once()
		d.notSuppressed("customer.an@example.com")

		var key usecase.SentKey

		d.Log.EXPECT().AlreadySent(mock.Anything, mock.Anything).
			Run(func(_ mockContext, k usecase.SentKey) { key = k }).Return(true, nil).Once()

		require.NoError(t, uc.HandleVehicleReady(t.Context(), event(garageID, "repair_order.completed", payload)))
		assert.Equal(t, usecase.SentKey{
			GarageID: garageID, Type: domain.TypeVehicleReady, Recipient: "customer.an@example.com", EventID: "event-1",
			PerEntity: true, EntityType: "repair_order", EntityID: "ro-1",
		}, key, "a vehicle is ready once per order, however often it completes")
	})

	t.Run("a payment is per event, not per settlement", func(t *testing.T) {
		t.Parallel()

		_, d := newGarageUseCase(t)
		d.Log = mocks.NewMockNotificationRepository(t)
		uc := rebuild(d)
		d.garageReads(domain.Settings{})
		d.Customers.EXPECT().ByVehicle(mock.Anything, garageID, vehicleID).Return(customerAn(), nil).Once()
		d.notSuppressed("customer.an@example.com")

		var key usecase.SentKey

		d.Log.EXPECT().AlreadySent(mock.Anything, mock.Anything).
			Run(func(_ mockContext, k usecase.SentKey) { key = k }).Return(true, nil).Once()

		partial := `{"settlement_id":"` + settlementID + `","vehicle_id":"` + vehicleID + `","amount":1,"balance":2}`
		require.NoError(t, uc.HandlePaymentReceived(t.Context(), event(garageID, "payment_voucher.issued", partial)))
		assert.False(t, key.PerEntity)
	})

	t.Run("a failed lookup of the log is returned", func(t *testing.T) {
		t.Parallel()

		_, d := newGarageUseCase(t)
		d.Log = mocks.NewMockNotificationRepository(t)
		uc := rebuild(d)
		d.garageReads(domain.Settings{})
		d.Customers.EXPECT().ByVehicle(mock.Anything, garageID, vehicleID).Return(customerAn(), nil).Once()
		d.notSuppressed("customer.an@example.com")
		d.Log.EXPECT().AlreadySent(mock.Anything, mock.Anything).Return(false, errBoom).Once()

		require.ErrorIs(t, uc.HandleVehicleReady(t.Context(), event(garageID, "repair_order.completed", payload)), errBoom)
	})

	t.Run("an e-mail of an event is logged apart from the event's transaction", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{})
		d.Customers.EXPECT().ByVehicle(mock.Anything, garageID, vehicleID).Return(customerAn(), nil).Once()
		d.notSuppressed("customer.an@example.com")
		d.sentMessage()

		var detached, inTransaction int

		d.Log.EXPECT().Sent(mock.Anything, mock.Anything).Run(func(mockContext, *domain.Log) { detached++ }).Return(nil).Once()
		d.Log.EXPECT().Create(mock.Anything, mock.Anything).Run(func(mockContext, *domain.Log) { inTransaction++ }).Return(nil).Maybe()

		require.NoError(t, uc.HandleVehicleReady(t.Context(), event(garageID, "repair_order.completed", payload)))
		assert.Equal(t, 1, detached)
		assert.Zero(t, inTransaction)
	})
}

func TestOneFailingRecipientDoesNotBlockTheOthers(t *testing.T) {
	t.Parallel()

	uc, d := newGarageUseCase(t)
	d.garageReads(domain.Settings{})
	d.Staff.EXPECT().Recipients(mock.Anything, garageID, []string(nil), "inventory.write").
		Return([]usecase.UserInfo{
			{ID: "u1", Email: "bad@example.com", FullName: "Bad", Locale: "en"},
			{ID: "u2", Email: "good@example.com", FullName: "Good", Locale: "en"},
		}, nil).Once()
	d.notSuppressed("bad@example.com")
	d.notSuppressed("good@example.com")

	var sentTo []string

	d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).RunAndReturn(func(_ mockContext, m mail.Message) error {
		if m.ToEmail == "bad@example.com" {
			return errBoom
		}

		sentTo = append(sentTo, m.ToEmail)

		return nil
	}).Twice()
	d.Log.EXPECT().Failed(mock.Anything, mock.Anything).Return(nil).Once()
	d.logged()

	payload := `{"part_id":"part-1","part_name":"Lọc dầu","quantity":"3","threshold":"4"}`

	require.ErrorIs(t, uc.HandleStockLow(t.Context(), event(garageID, "stock.low", payload)), errBoom, "the event is delivered again")
	assert.Equal(t, []string{"good@example.com"}, sentTo, "the other recipient got theirs")
}

func TestAnEmailThatWentOutIsNotUndoneByALogFailure(t *testing.T) {
	t.Parallel()

	uc, d := newGarageUseCase(t)
	d.garageReads(domain.Settings{})
	d.Customers.EXPECT().Get(mock.Anything, garageID, customerID).Return(customerAn(), nil).Once()
	d.notSuppressed("customer.an@example.com")
	d.sentMessage()
	d.Log.EXPECT().Create(mock.Anything, mock.Anything).Return(errBoom).Once()

	assert.True(t, uc.QuoteReady(t.Context(), usecase.QuoteReadyRequest{
		GarageID: garageID, QuoteID: quoteID, CustomerID: customerID, Number: "BG-1", VehiclePlate: "51A", Total: "1", ApproveURL: "https://x",
	}), "the customer has the link, so the advisor is told so")
}

func TestReceiptNameIsSafe(t *testing.T) {
	t.Parallel()

	uc, d := newGarageUseCase(t)
	d.garageReads(domain.Settings{})
	d.Customers.EXPECT().ByVehicle(mock.Anything, garageID, vehicleID).Return(customerAn(), nil).Once()
	d.Staff.EXPECT().Recipients(mock.Anything, garageID, []string(nil), "").Return(nil, nil).Once()
	d.notSuppressed("customer.an@example.com")
	d.Receipts.EXPECT().SettlementReceipt(mock.Anything, garageID, settlementID).Return([]byte("%PDF"), nil).Once()

	msg := d.sentMessage()
	d.logged()

	payload := `{"settlement_id":"` + settlementID + `","vehicle_id":"` + vehicleID + `","number":"QT/2026 0001","total":1}`
	require.NoError(t, uc.HandleSettlementPaid(t.Context(), event(garageID, "settlement.paid", payload)))
	require.Len(t, msg.Attachments, 1)
	assert.Equal(t, "receipt-QT-2026-0001.pdf", msg.Attachments[0].Name)
}

func TestTheReceiptIsNotRenderedWhenItWillNotBeSent(t *testing.T) {
	t.Parallel()

	uc, d := newGarageUseCase(t)
	d.garageReads(domain.Settings{Off: map[domain.Type]bool{domain.TypeSettlementReceipt: true, domain.TypeSettlementPaid: true}})

	noEmail := customerAn()
	noEmail.Email = ""

	d.Customers.EXPECT().ByVehicle(mock.Anything, garageID, vehicleID).Return(noEmail, nil).Once()
	d.Staff.EXPECT().Recipients(mock.Anything, garageID, []string(nil), "").Return(nil, nil).Once()
	d.logged()

	// No Receipts expectation: a call would fail the test.
	payload := `{"settlement_id":"` + settlementID + `","vehicle_id":"` + vehicleID + `","number":"ST-1","total":1}`
	require.NoError(t, uc.HandleSettlementPaid(t.Context(), event(garageID, "settlement.paid", payload)))
}

func TestACustomerFailureDoesNotBlockTheStaff(t *testing.T) {
	t.Parallel()

	uc, d := newGarageUseCase(t)
	d.garageReads(domain.Settings{})
	d.Customers.EXPECT().ByVehicle(mock.Anything, garageID, vehicleID).Return(customerAn(), nil).Once()
	d.Staff.EXPECT().Recipients(mock.Anything, garageID, []string{creatorID}, "").Return([]usecase.UserInfo{staffLan()}, nil).Once()
	d.notSuppressed("customer.an@example.com")
	d.notSuppressed("lan@example.com")
	d.Receipts.EXPECT().SettlementReceipt(mock.Anything, garageID, settlementID).Return([]byte("%PDF"), nil).Once()

	var sentTo []string

	d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).RunAndReturn(func(_ mockContext, m mail.Message) error {
		if m.ToEmail == "customer.an@example.com" {
			return errBoom
		}

		sentTo = append(sentTo, m.ToEmail)

		return nil
	}).Twice()
	d.Log.EXPECT().Failed(mock.Anything, mock.Anything).Return(nil).Once()
	d.logged()

	payload := `{"settlement_id":"` + settlementID + `","vehicle_id":"` + vehicleID + `","number":"ST-1","total":1,"created_by":"` + creatorID + `"}`

	require.ErrorIs(t, uc.HandleSettlementPaid(t.Context(), event(garageID, "settlement.paid", payload)), errBoom)
	assert.Equal(t, []string{"lan@example.com"}, sentTo, "the staff were told")
}

func TestAGarageSeesAShortReasonNotTheProvidersAnswer(t *testing.T) {
	t.Parallel()

	uc, d := newGarageUseCase(t)
	d.garageReads(domain.Settings{})
	d.Customers.EXPECT().Get(mock.Anything, garageID, customerID).Return(customerAn(), nil).Once()
	d.notSuppressed("customer.an@example.com")

	providerAnswer := fmt.Errorf(`mailjet: status 401: {"ErrorMessage":"API key abc123 of account 99 is blocked","Domain":"%s"}`, strings.Repeat("x", 500)) //nolint:err113 // a provider answer, built for the test
	d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Return(providerAnswer).Once()

	var failed *domain.Log

	d.Log.EXPECT().Failed(mock.Anything, mock.Anything).Run(func(_ mockContext, l *domain.Log) { failed = l }).Return(nil).Once()

	assert.False(t, uc.QuoteReady(t.Context(), usecase.QuoteReadyRequest{
		GarageID: garageID, QuoteID: quoteID, CustomerID: customerID, Number: "BG-1", VehiclePlate: "51A", Total: "1", ApproveURL: "https://x",
	}))
	require.NotNil(t, failed)
	assert.Equal(t, "mailjet: status 401", failed.Error)
	assert.NotContains(t, failed.Error, "abc123")

	long := fmt.Errorf("%s", strings.Repeat("a", 500)) //nolint:err113 // a long error, built for the test
	assert.Len(t, usecase.FailureReasonForTest(long), 200)
}
