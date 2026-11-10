package receivable_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	notificationv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/notification/v1"
	receivablev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/receivable/v1"
	"github.com/xuri/excelize/v2"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
	"github.com/viettechno/vietgara-backend/testing/blackbox/timeshift"
)

const (
	cash = billingv1.PaymentMethod_PAYMENT_METHOD_CASH
	bank = billingv1.PaymentMethod_PAYMENT_METHOD_BANK_TRANSFER

	statusOpen = receivablev1.ReceivableStatus_RECEIVABLE_STATUS_OPEN
	statusPaid = receivablev1.ReceivableStatus_RECEIVABLE_STATUS_PAID
)

func remaining(want int64, status receivablev1.ReceivableStatus) func(*receivablev1.Receivable) bool {
	return func(r *receivablev1.Receivable) bool {
		return r.GetRemainingAmount() == want && r.GetStatus() == status
	}
}

// Scenario: E2E-REC-001, Release 11.0, a settlement becomes a receivable with its
// due date, and a payment settles it (FR-REC-01).
func TestSettlementBecomesAReceivable(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newKit(t, "rec-001")
	api := k.Owner.API

	settlement := k.settle(t, 1_000_000)
	r := k.receivableOf(t, settlement.GetId(), nil)

	assert.Equal(t, settlement.GetNumber(), r.GetSettlementNumber())
	assert.Equal(t, k.CustomerID, r.GetCustomerId())
	assert.EqualValues(t, 1_000_000, r.GetTotalAmount())
	assert.EqualValues(t, 0, r.GetPaidAmount())
	assert.EqualValues(t, 1_000_000, r.GetRemainingAmount())
	assert.Equal(t, statusOpen, r.GetStatus())
	assert.False(t, r.GetOverdue())
	assert.True(t, sameDay(r.GetDueDate(), today().AddDate(0, 0, 7)), "seven days of terms by default: %s", r.GetDueDate())

	list, err := api.ListReceivables(ctx, k.GarageID, client.ReceivableFilter{})
	require.NoError(t, err)
	require.Len(t, list.GetData(), 1)
	assert.EqualValues(t, 1, list.GetSummary().GetOpenCount())
	assert.EqualValues(t, 1_000_000, list.GetSummary().GetOutstandingAmount())

	k.pay(t, settlement.GetId(), 400_000, cash)
	r = k.receivableOf(t, settlement.GetId(), remaining(600_000, statusOpen))
	assert.EqualValues(t, 400_000, r.GetPaidAmount())

	k.pay(t, settlement.GetId(), 600_000, cash)
	r = k.receivableOf(t, settlement.GetId(), remaining(0, statusPaid))
	assert.EqualValues(t, 1_000_000, r.GetPaidAmount())

	list, err = api.ListReceivables(ctx, k.GarageID, client.ReceivableFilter{})
	require.NoError(t, err)
	assert.Empty(t, list.GetData(), "a paid receivable leaves the list")
	assert.EqualValues(t, 0, list.GetSummary().GetOpenCount())

	list, err = api.ListReceivables(ctx, k.GarageID, client.ReceivableFilter{IncludePaid: true})
	require.NoError(t, err)
	assert.Len(t, list.GetData(), 1)
}

// Scenario: E2E-REC-002, Release 11.0, payments and voids move the remaining amount
// and the ledger (FR-REC-01).
func TestPaymentsAndVoidsMoveTheLedger(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newKit(t, "rec-002")

	settlement := k.settle(t, 1_000_000)
	payment := k.pay(t, settlement.GetId(), 400_000, cash)
	k.receivableOf(t, settlement.GetId(), remaining(600_000, statusOpen))

	_, err := k.Owner.API.VoidSettlementPayment(ctx, k.GarageID, settlement.GetId(), payment.GetId(), "nhập nhầm")
	require.NoError(t, err)

	r := k.receivableOf(t, settlement.GetId(), remaining(1_000_000, statusOpen))

	kinds := make([]receivablev1.ReceivableEntryKind, 0, len(r.GetEntries()))
	for _, e := range r.GetEntries() {
		kinds = append(kinds, e.GetKind())
	}

	assert.Equal(t, []receivablev1.ReceivableEntryKind{
		receivablev1.ReceivableEntryKind_RECEIVABLE_ENTRY_KIND_CHARGE,
		receivablev1.ReceivableEntryKind_RECEIVABLE_ENTRY_KIND_PAYMENT,
		receivablev1.ReceivableEntryKind_RECEIVABLE_ENTRY_KIND_VOID,
	}, kinds)
	assert.EqualValues(t, 400_000, r.GetEntries()[1].GetAmount())
	assert.Equal(t, payment.GetVoucherNumber(), r.GetEntries()[1].GetReference())
	assert.Equal(t, cash, r.GetEntries()[1].GetMethod())

	k.pay(t, settlement.GetId(), 1_000_000, cash)
	r = k.receivableOf(t, settlement.GetId(), remaining(0, statusPaid))
	assert.Len(t, r.GetEntries(), 4, "nothing earlier is rewritten")
}

// Scenario: E2E-REC-003, Release 11.0, the payment terms and the reminder schedule:
// defaults, validation and the effect on new settlements (FR-REC-03).
func TestSettingsDefaultsValidationAndEffect(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newKit(t, "rec-003")
	api := k.Owner.API

	defaults, err := api.GetReceivableSettings(ctx, k.GarageID)
	require.NoError(t, err)
	assert.EqualValues(t, 7, defaults.GetPaymentTermsDays())
	assert.True(t, defaults.GetRemindersEnabled())
	assert.Equal(t, []int32{3, 7}, defaults.GetReminderOffsetDays())

	saved, err := api.UpdateReceivableSettings(ctx, k.GarageID, &receivablev1.ReceivableSettings{
		PaymentTermsDays: new(int32(0)), RemindersEnabled: new(true), ReminderOffsetDays: []int32{10, 2, 5},
	})
	require.NoError(t, err)
	assert.Equal(t, []int32{2, 5, 10}, saved.GetReminderOffsetDays())

	read, err := api.GetReceivableSettings(ctx, k.GarageID)
	require.NoError(t, err)
	assert.EqualValues(t, 0, read.GetPaymentTermsDays())

	r := k.receivableOf(t, k.settle(t, 100_000).GetId(), nil)
	assert.True(t, sameDay(r.GetDueDate(), today()), "no terms: due the day it is issued, got %s", r.GetDueDate())

	for _, bad := range map[string]*receivablev1.ReceivableSettings{
		"long terms":     {PaymentTermsDays: new(int32(366)), RemindersEnabled: new(true)},
		"same offsets":   {PaymentTermsDays: new(int32(7)), RemindersEnabled: new(true), ReminderOffsetDays: []int32{1, 1}},
		"six offsets":    {PaymentTermsDays: new(int32(7)), RemindersEnabled: new(true), ReminderOffsetDays: []int32{1, 2, 3, 4, 5, 6}},
		"offset zero":    {PaymentTermsDays: new(int32(7)), RemindersEnabled: new(true), ReminderOffsetDays: []int32{0}},
		"offset over 90": {PaymentTermsDays: new(int32(7)), RemindersEnabled: new(true), ReminderOffsetDays: []int32{91}},
		"no terms":       {RemindersEnabled: new(true)},
		"no switch":      {PaymentTermsDays: new(int32(7))},
		"on, no days":    {PaymentTermsDays: new(int32(7)), RemindersEnabled: new(true)},
	} {
		_, err = api.UpdateReceivableSettings(ctx, k.GarageID, bad)
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
	}

	still, err := api.GetReceivableSettings(ctx, k.GarageID)
	require.NoError(t, err)
	assert.Equal(t, []int32{2, 5, 10}, still.GetReminderOffsetDays(), "a refused update changes nothing")

	other := fixture.NewBusiness(t, "rec-003-other")
	theirs, err := other.Owner.API.GetReceivableSettings(ctx, other.GarageID)
	require.NoError(t, err)
	assert.EqualValues(t, 7, theirs.GetPaymentTermsDays(), "another garage keeps its own settings")
}

// Scenario: E2E-REC-004, Release 11.0, an overdue receivable is reminded at each
// offset, once (FR-REC-03). It moves the due date with SQL, so it runs on the stubbed
// target only.
func TestOverdueReceivableIsRemindedAtEachOffsetOnce(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newKit(t, "rec-004")

	settlement := k.settle(t, 1_000_000)
	k.receivableOf(t, settlement.GetId(), nil)

	timeshift.AgeReceivable(t, settlement.GetId(), 2)
	k.noMail(t, reminderSubject)

	timeshift.AgeReceivable(t, settlement.GetId(), 3)

	first := k.mailTo(t, reminderSubject)
	assert.Contains(t, first.Subject, settlement.GetNumber())
	assert.Contains(t, first.Text, "1.000.000")
	assert.Contains(t, first.Text, "3 ngày")

	k.noMail(t, "never") // gives the job time for a second run: the same offset sends nothing more

	count, err := k.inbox.Count(ctx, k.email, reminderSubject)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	timeshift.AgeReceivable(t, settlement.GetId(), 7)

	eventually(t, arrives, "the second reminder", func(ctx context.Context) bool {
		n, err := k.inbox.Count(ctx, k.email, reminderSubject)

		return err == nil && n == 2
	})

	r := k.receivableOf(t, settlement.GetId(), func(r *receivablev1.Receivable) bool { return r.GetRemindersSent() == 2 })
	require.Len(t, r.GetReminders(), 2)

	for i, offset := range []int32{3, 7} {
		assert.Equal(t, offset, r.GetReminders()[i].GetOffsetDays())
		assert.Equal(t, receivablev1.ReminderOutcome_REMINDER_OUTCOME_SENT, r.GetReminders()[i].GetOutcome())
		assert.Equal(t, "email", r.GetReminders()[i].GetChannel())
	}

	assert.NotNil(t, r.GetLastReminderAt())
	assert.True(t, r.GetOverdue())
	assert.EqualValues(t, 7, r.GetDaysOverdue())

	entries, err := k.Owner.API.ListNotificationLog(ctx, k.GarageID, notificationv1.NotificationType_NOTIFICATION_TYPE_PAYMENT_REMINDER,
		notificationv1.NotificationStatus_NOTIFICATION_STATUS_SENT)
	require.NoError(t, err)
	assert.Len(t, entries, 2, "the garage's log shows both reminders")

	// A receivable paid before its next offset is not reminded again.
	other := k.settle(t, 500_000)
	k.receivableOf(t, other.GetId(), nil)
	timeshift.AgeReceivable(t, other.GetId(), 3)
	k.mailTo(t, other.GetNumber())
	k.pay(t, other.GetId(), 500_000, cash)
	k.receivableOf(t, other.GetId(), remaining(0, statusPaid))
	timeshift.AgeReceivable(t, other.GetId(), 8)
	k.noMail(t, "Quyết toán "+other.GetNumber()+" tại") // no further reminder for the paid one
}

// Scenario: E2E-REC-005, Release 11.0, a reminder is skipped, with its reason, for a
// customer without e-mail, an opt-out or a switched-off type (FR-REC-03). Stubbed only.
func TestReminderSkipsWithItsReason(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	reminder := func(t *testing.T, k *kit, settlementID string) *receivablev1.ReceivableReminder {
		t.Helper()

		var got *receivablev1.ReceivableReminder

		eventually(t, arrives, "the reminder to be logged", func(context.Context) bool {
			r := k.receivableOf(t, settlementID, nil)
			if len(r.GetReminders()) == 0 {
				return false
			}

			got = r.GetReminders()[0]

			return true
		})

		return got
	}

	t.Run("no e-mail address", func(t *testing.T) {
		t.Parallel()

		k := &kit{Business: fixture.NewBusiness(t, "rec-005a"), inbox: fixture.Mailpit(t)}
		s := k.settle(t, 400_000)
		k.receivableOf(t, s.GetId(), nil)
		timeshift.AgeReceivable(t, s.GetId(), 3)

		got := reminder(t, k, s.GetId())
		assert.Equal(t, receivablev1.ReminderOutcome_REMINDER_OUTCOME_SKIPPED, got.GetOutcome())
		assert.Equal(t, "NO_EMAIL", got.GetReason())
	})

	t.Run("the customer opted out", func(t *testing.T) {
		t.Parallel()

		k := newKit(t, "rec-005b")
		first := k.settle(t, 400_000)
		k.pay(t, first.GetId(), 100_000, cash)

		mail := k.mailTo(t, "đã nhận")
		token := client.OptOutLinkToken(mail.Text)
		require.NotEmpty(t, token)

		status, _, err := fixture.Anonymous(t).OptOut(ctx, token)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)

		timeshift.AgeReceivable(t, first.GetId(), 3)

		got := reminder(t, k, first.GetId())
		assert.Equal(t, "OPTED_OUT", got.GetReason())
		k.noMail(t, reminderSubject)
	})

	t.Run("the garage switched the type off", func(t *testing.T) {
		t.Parallel()

		k := newKit(t, "rec-005c")
		_, err := k.Owner.API.UpdateNotificationSettings(ctx, k.GarageID, "", &notificationv1.NotificationTypeSetting{
			Type: notificationv1.NotificationType_NOTIFICATION_TYPE_PAYMENT_REMINDER, Enabled: false,
		})
		require.NoError(t, err)

		s := k.settle(t, 400_000)
		k.receivableOf(t, s.GetId(), nil)
		timeshift.AgeReceivable(t, s.GetId(), 3)

		got := reminder(t, k, s.GetId())
		assert.Equal(t, "DISABLED", got.GetReason())
		k.noMail(t, reminderSubject)
	})

	t.Run("a late receivable gets the latest offset and supersedes the earlier one", func(t *testing.T) {
		t.Parallel()

		k := newKit(t, "rec-005d")
		s := k.settle(t, 400_000)
		k.receivableOf(t, s.GetId(), nil)
		timeshift.AgeReceivable(t, s.GetId(), 10)

		k.mailTo(t, reminderSubject)

		r := k.receivableOf(t, s.GetId(), func(r *receivablev1.Receivable) bool { return len(r.GetReminders()) == 2 })
		assert.Equal(t, receivablev1.ReminderOutcome_REMINDER_OUTCOME_SKIPPED, r.GetReminders()[0].GetOutcome())
		assert.Equal(t, "SUPERSEDED", r.GetReminders()[0].GetReason())
		assert.Equal(t, receivablev1.ReminderOutcome_REMINDER_OUTCOME_SENT, r.GetReminders()[1].GetOutcome())
		assert.EqualValues(t, 7, r.GetReminders()[1].GetOffsetDays())
	})

	t.Run("reminders switched off in the receivable settings", func(t *testing.T) {
		t.Parallel()

		k := newKit(t, "rec-005e")
		_, err := k.Owner.API.UpdateReceivableSettings(ctx, k.GarageID, &receivablev1.ReceivableSettings{
			PaymentTermsDays: new(int32(7)), RemindersEnabled: new(false), ReminderOffsetDays: []int32{3, 7},
		})
		require.NoError(t, err)

		s := k.settle(t, 400_000)
		k.receivableOf(t, s.GetId(), nil)
		timeshift.AgeReceivable(t, s.GetId(), 8)
		k.noMail(t, reminderSubject)
	})
}

// Scenario: E2E-REC-006, Release 11.0, the reconciliation of a period agrees with the
// settlements and vouchers (FR-REC-04); the payment-out voucher part is in E2E-PAY-002.
func TestReconciliationAgreesWithTheLedger(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newKit(t, "rec-006")
	api := k.Owner.API

	// S0 is from before the period: issued, paid in part, and 400,000 still owed.
	s0 := k.settle(t, 1_000_000)
	k.pay(t, s0.GetId(), 600_000, cash)
	k.receivableOf(t, s0.GetId(), remaining(400_000, statusOpen))
	timeshift.AgeLedger(t, s0.GetId(), 40)

	s1 := k.settle(t, 1_000_000)
	k.pay(t, s1.GetId(), 1_000_000, cash)

	s2 := k.settle(t, 2_000_000)
	voided := k.pay(t, s2.GetId(), 300_000, cash)
	_, err := api.VoidSettlementPayment(ctx, k.GarageID, s2.GetId(), voided.GetId(), "nhập nhầm")
	require.NoError(t, err)
	k.pay(t, s2.GetId(), 500_000, bank)

	s3 := k.settle(t, 800_000)

	for _, s := range []*billingv1.Settlement{s1, s2, s3} {
		want := map[string]int64{s1.GetId(): 0, s2.GetId(): 1_500_000, s3.GetId(): 800_000}[s.GetId()]
		k.receivableOf(t, s.GetId(), func(r *receivablev1.Receivable) bool {
			return r.GetRemainingAmount() == want && len(r.GetEntries()) >= 1
		})
	}

	// The ledger has all seven movements once the void and the last payments arrived.
	eventually(t, arrives, "the whole ledger", func(ctx context.Context) bool {
		rep, err := api.GetReconciliation(ctx, k.GarageID, today().AddDate(0, 0, -1).Format(time.DateOnly), today().AddDate(0, 0, 2).Format(time.DateOnly))

		return err == nil && len(rep.GetLines()) == 7
	})

	from, to := today().AddDate(0, 0, -1).Format(time.DateOnly), today().AddDate(0, 0, 2).Format(time.DateOnly)

	rep, err := api.GetReconciliation(ctx, k.GarageID, from, to)
	require.NoError(t, err)

	assert.EqualValues(t, 400_000, rep.GetOpeningBalance())
	assert.EqualValues(t, 3_800_000, rep.GetChargedAmount())
	assert.EqualValues(t, 1_800_000, rep.GetCollectedAmount())
	assert.EqualValues(t, 300_000, rep.GetVoidedAmount())
	assert.EqualValues(t, 2_700_000, rep.GetClosingBalance())
	assert.EqualValues(t, 3, rep.GetSettlementCount())
	assert.EqualValues(t, 3, rep.GetPaymentCount())
	assert.True(t, rep.GetMatches(), "difference %d", rep.GetDifference())
	assert.EqualValues(t, 0, rep.GetDifference())

	byMethod := map[billingv1.PaymentMethod]int64{}
	for _, m := range rep.GetCollectedByMethod() {
		byMethod[m.GetMethod()] = m.GetAmount()
	}

	assert.Equal(t, map[billingv1.PaymentMethod]int64{cash: 1_300_000, bank: 500_000}, byMethod)

	for _, l := range rep.GetLines() {
		assert.NotEmpty(t, l.GetDate())
		assert.NotEmpty(t, l.GetReference())
		assert.NotEmpty(t, l.GetCustomerName())
	}

	// A later period starts from the closing balance of this one and holds no movement.
	later, err := api.GetReconciliation(ctx, k.GarageID, to, today().AddDate(0, 0, 3).Format(time.DateOnly))
	require.NoError(t, err)
	assert.EqualValues(t, 2_700_000, later.GetOpeningBalance())
	assert.EqualValues(t, 2_700_000, later.GetClosingBalance())
	assert.Empty(t, later.GetLines())

	for _, bad := range [][2]string{{from, from}, {to, from}, {"bad", to}, {from, ""}} {
		_, err = api.GetReconciliation(ctx, k.GarageID, bad[0], bad[1])
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
	}
}

// Scenario: E2E-REC-007, Release 11.0, the receivables and the reconciliation as Excel
// workbooks (FR-DOC-02, FR-REC-04).
func TestReceivablesAndReconciliationAsExcel(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newKit(t, "rec-007")
	api := k.Owner.API

	// A customer name that would be a formula in a spreadsheet stays text.
	hostile := `=HYPERLINK("http://example.test","click")`
	_, err := api.UpdateCustomer(ctx, k.GarageID, k.CustomerID, &customerv1.Customer{FullName: hostile}, "full_name")
	require.NoError(t, err)

	s1 := k.settle(t, 700_000)
	s2 := k.settle(t, 300_000)

	for _, s := range []*billingv1.Settlement{s1, s2} {
		k.receivableOf(t, s.GetId(), nil)
	}

	contentType, body, err := api.DownloadReceivablesExcel(ctx, k.GarageID, client.ReceivableFilter{})
	require.NoError(t, err)
	assert.Contains(t, contentType, "spreadsheetml")

	book, err := excelize.OpenReader(bytes.NewReader(body))
	require.NoError(t, err)

	defer func() { _ = book.Close() }()

	rows, err := book.GetRows("Receivables")
	require.NoError(t, err)
	require.Len(t, rows, 4, "a header, two receivables and the total")
	assert.Equal(t, "Total", rows[3][0])
	assert.Equal(t, "1000000", rows[3][5])
	assert.Equal(t, "1000000", rows[3][7])

	cell, err := book.GetCellValue("Receivables", "B2")
	require.NoError(t, err)
	assert.Equal(t, hostile, cell)

	formula, err := book.GetCellFormula("Receivables", "B2")
	require.NoError(t, err)
	assert.Empty(t, formula, "text that looks like a formula is not one")

	_, filtered, err := api.DownloadReceivablesExcel(ctx, k.GarageID, client.ReceivableFilter{Query: s1.GetNumber()})
	require.NoError(t, err)

	narrow, err := excelize.OpenReader(bytes.NewReader(filtered))
	require.NoError(t, err)

	defer func() { _ = narrow.Close() }()

	rows, err = narrow.GetRows("Receivables")
	require.NoError(t, err)
	assert.Len(t, rows, 3, "the search narrows the workbook like the list")

	_, none, err := api.DownloadReceivablesExcel(ctx, k.GarageID, client.ReceivableFilter{OverdueOnly: true})
	require.NoError(t, err)

	empty, err := excelize.OpenReader(bytes.NewReader(none))
	require.NoError(t, err)

	defer func() { _ = empty.Close() }()

	rows, err = empty.GetRows("Receivables")
	require.NoError(t, err)
	assert.Len(t, rows, 2, "nothing is overdue yet: the header and a zero total")

	contentType, recon, err := api.DownloadReconciliationExcel(ctx, k.GarageID,
		today().AddDate(0, 0, -1).Format(time.DateOnly), today().AddDate(0, 0, 2).Format(time.DateOnly))
	require.NoError(t, err)
	assert.Contains(t, contentType, "spreadsheetml")

	sheets, err := excelize.OpenReader(bytes.NewReader(recon))
	require.NoError(t, err)

	defer func() { _ = sheets.Close() }()

	assert.ElementsMatch(t, []string{"Summary", "Lines"}, sheets.GetSheetList())

	lines, err := sheets.GetRows("Lines")
	require.NoError(t, err)
	assert.Len(t, lines, 3, "a header and the two charges")

	_, _, err = api.DownloadReconciliationExcel(ctx, k.GarageID, "bad", "worse")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
}

// Scenario: E2E-REC-008, Release 11.0, permissions and tenant isolation of the
// receivables (FR-REC-01, NFR-SE).
func TestPermissionsAndTenantIsolation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newKit(t, "rec-008")

	accountant := k.AddStaff(t, "Accountant")
	advisor := k.AddStaff(t, "Service Advisor")
	technician := k.AddStaff(t, "Technician")

	settlement := k.settle(t, 400_000)
	r := k.receivableOf(t, settlement.GetId(), nil)

	// The accountant reads and saves.
	list, err := accountant.API.ListReceivables(ctx, k.GarageID, client.ReceivableFilter{})
	require.NoError(t, err)
	assert.Len(t, list.GetData(), 1)

	_, err = accountant.API.GetReceivable(ctx, k.GarageID, r.GetId())
	require.NoError(t, err)

	_, err = accountant.API.UpdateReceivableSettings(ctx, k.GarageID, &receivablev1.ReceivableSettings{
		PaymentTermsDays: new(int32(10)), RemindersEnabled: new(true), ReminderOffsetDays: []int32{4},
	})
	require.NoError(t, err)

	// Without billing access every call is refused.
	for _, who := range []*fixture.Actor{advisor, technician} {
		_, err = who.API.ListReceivables(ctx, k.GarageID, client.ReceivableFilter{})
		fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")

		_, err = who.API.GetReceivable(ctx, k.GarageID, r.GetId())
		fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")

		_, err = who.API.GetReceivableSettings(ctx, k.GarageID)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")

		_, err = who.API.UpdateReceivableSettings(ctx, k.GarageID, &receivablev1.ReceivableSettings{PaymentTermsDays: new(int32(1)), RemindersEnabled: new(true)})
		fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")

		_, err = who.API.GetReconciliation(ctx, k.GarageID, "2026-01-01", "2026-02-01")
		fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")

		_, _, err = who.API.DownloadReceivablesExcel(ctx, k.GarageID, client.ReceivableFilter{})
		fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")
	}

	// Another tenant sees nothing of this garage.
	other := fixture.NewBusiness(t, "rec-008-other")

	_, err = other.Owner.API.GetReceivable(ctx, other.GarageID, r.GetId())
	fixture.RequireAPIError(t, err, http.StatusNotFound, "RECEIVABLE_NOT_FOUND")

	_, err = other.Owner.API.GetReceivable(ctx, k.GarageID, r.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

	_, err = other.Owner.API.ListReceivables(ctx, k.GarageID, client.ReceivableFilter{})
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

	theirs, err := other.Owner.API.ListReceivables(ctx, other.GarageID, client.ReceivableFilter{IncludePaid: true})
	require.NoError(t, err)
	assert.Empty(t, theirs.GetData())

	// Without a token nothing is served.
	_, err = fixture.Anonymous(t).ListReceivables(ctx, k.GarageID, client.ReceivableFilter{})
	fixture.RequireAPIError(t, err, http.StatusUnauthorized, "")
}

// Scenario: E2E-REC-001 (partial), Release 11.0, a settlement that has no receivable
// (issued before the module existed, or whose event was lost) gets one from the
// reminder job (FR-REC-01). Stubbed only: it deletes the receivable with SQL.
func TestMissingReceivableIsRebuilt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newKit(t, "rec-001-heal")

	settlement := k.settle(t, 600_000)
	first := k.receivableOf(t, settlement.GetId(), nil)

	timeshift.DropReceivable(t, settlement.GetId())

	healed := k.receivableOf(t, settlement.GetId(), nil)
	assert.NotEqual(t, first.GetId(), healed.GetId(), "a new row")
	assert.EqualValues(t, 600_000, healed.GetRemainingAmount())
	assert.Equal(t, k.CustomerID, healed.GetCustomerId())

	list, err := k.Owner.API.ListReceivables(ctx, k.GarageID, client.ReceivableFilter{})
	require.NoError(t, err)
	assert.Len(t, list.GetData(), 1)
}
