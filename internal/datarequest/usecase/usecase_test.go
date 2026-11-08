package usecase_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/datarequest/domain"
	"github.com/viettechno/vietgara-backend/internal/datarequest/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	garageID   = "11111111-1111-1111-1111-111111111111"
	garageTwo  = "11111111-1111-1111-1111-111111111112"
	customerID = "22222222-2222-2222-2222-222222222222"
	otherID    = "22222222-2222-2222-2222-222222222223"
	actorID    = "33333333-3333-3333-3333-333333333333"
	requestID  = "44444444-4444-4444-4444-444444444444"
)

var (
	errBoom = errors.New("boom")
	now     = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
)

func assertAppError(t *testing.T, err error, code apperror.Code) {
	t.Helper()

	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected an *apperror.AppError, got %T: %v", err, err)
	require.Equal(t, code, appErr.Code)
}

type staffDeps struct {
	Repo      *mocks.MockDataRequestRepository
	Customers *mocks.MockDataRequestCustomers
	Documents *mocks.MockDataRequestDocuments
	Names     *mocks.MockDataRequestNames
}

func newStaff(t *testing.T) (*usecase.StaffUseCase, *staffDeps) {
	t.Helper()

	d := &staffDeps{
		Repo: mocks.NewMockDataRequestRepository(t), Customers: mocks.NewMockDataRequestCustomers(t),
		Documents: mocks.NewMockDataRequestDocuments(t), Names: mocks.NewMockDataRequestNames(t),
	}

	return usecase.NewStaffUseCase(d.Repo, d.Customers, d.Documents, d.Names), d
}

func TestStaffUseCase_Create(t *testing.T) {
	t.Parallel()

	req := func(typ domain.Type, details string) usecase.CreateRequest {
		return usecase.CreateRequest{GarageID: garageID, CustomerID: customerID, ActorID: actorID, Type: typ, Details: details}
	}

	t.Run("a view is answered with the data and the document numbers, and stored without them", func(t *testing.T) {
		t.Parallel()

		uc, d := newStaff(t)
		d.Customers.EXPECT().Check(mock.Anything, garageID, customerID).Return(nil).Once()
		d.Customers.EXPECT().Export(mock.Anything, garageID, customerID).Return(json.RawMessage(`{"customer":{"fullName":"An"}}`), nil).Once()
		d.Documents.EXPECT().Numbers(mock.Anything, garageID, customerID).Return(usecase.DocumentNumbers{Settlements: []string{"QT-1"}}, nil).Once()
		d.Repo.EXPECT().Create(mock.Anything, mock.MatchedBy(func(r *domain.Request) bool {
			return r.Type == domain.TypeView && r.Status == domain.StatusCompleted && r.Source == domain.SourceStaff && r.Details == "" &&
				*r.RequestedBy == actorID && r.CompletedAt != nil
		})).Return(nil).Once()

		got, err := uc.Create(t.Context(), req(domain.TypeView, "ignored for a view"))
		require.NoError(t, err)
		assert.Equal(t, domain.StatusCompleted, got.Status)
		assert.Contains(t, got.ExportJSON, "QT-1")
		assert.Contains(t, got.ExportJSON, "fullName")
	})

	t.Run("a deletion anonymizes at once, says what it did and clears the details of the customer's requests", func(t *testing.T) {
		t.Parallel()

		uc, d := newStaff(t)
		d.Customers.EXPECT().Check(mock.Anything, garageID, customerID).Return(nil).Once()
		d.Customers.EXPECT().Anonymize(mock.Anything, garageID, customerID, actorID).Return(usecase.Counts{Customers: 1, Vehicles: 2, TaxProfiles: 1}, nil).Once()
		d.Repo.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().ClearDetails(mock.Anything, garageID, customerID).Return(nil).Once()

		got, err := uc.Create(t.Context(), req(domain.TypeDelete, ""))
		require.NoError(t, err)
		assert.Equal(t, domain.StatusCompleted, got.Status)
		assert.Equal(t, "Anonymized 1 customer, 2 vehicle(s) and 1 tax profile(s)", got.Outcome)
	})

	t.Run("a correction waits for the garage", func(t *testing.T) {
		t.Parallel()

		uc, d := newStaff(t)
		d.Customers.EXPECT().Check(mock.Anything, garageID, customerID).Return(nil).Once()
		d.Repo.EXPECT().Create(mock.Anything, mock.MatchedBy(func(r *domain.Request) bool {
			return r.Status == domain.StatusPending && r.Details == "new phone" && r.CompletedAt == nil
		})).Return(nil).Once()

		got, err := uc.Create(t.Context(), req(domain.TypeCorrect, "  new phone  "))
		require.NoError(t, err)
		assert.True(t, got.Pending())
	})

	t.Run("refuses what is wrong before touching anything", func(t *testing.T) {
		t.Parallel()

		uc, _ := newStaff(t)

		for name, r := range map[string]usecase.CreateRequest{
			"no type":                 {GarageID: garageID, CustomerID: customerID},
			"unknown type":            req("erase", ""),
			"a correction without it": req(domain.TypeCorrect, "   "),
			"details too long":        req(domain.TypeCorrect, strings.Repeat("é", domain.MaxTextLen+1)),
		} {
			_, err := uc.Create(t.Context(), r)
			require.Error(t, err, name)
			assertAppError(t, err, apperror.CodeInvalidArgument)
		}
	})

	failures := map[string]func(d *staffDeps){
		"the customer": func(d *staffDeps) {
			d.Customers.EXPECT().Check(mock.Anything, garageID, customerID).Return(apperror.New(apperror.CodeNotFound, "gone")).Once()
		},
		"the export": func(d *staffDeps) {
			d.Customers.EXPECT().Check(mock.Anything, garageID, customerID).Return(nil).Once()
			d.Customers.EXPECT().Export(mock.Anything, garageID, customerID).Return(nil, errBoom).Once()
		},
		"the document numbers": func(d *staffDeps) {
			d.Customers.EXPECT().Check(mock.Anything, garageID, customerID).Return(nil).Once()
			d.Customers.EXPECT().Export(mock.Anything, garageID, customerID).Return(json.RawMessage(`{}`), nil).Once()
			d.Documents.EXPECT().Numbers(mock.Anything, garageID, customerID).Return(usecase.DocumentNumbers{}, errBoom).Once()
		},
		"the anonymization": func(d *staffDeps) {
			d.Customers.EXPECT().Check(mock.Anything, garageID, customerID).Return(nil).Once()
			d.Customers.EXPECT().Anonymize(mock.Anything, garageID, customerID, actorID).Return(usecase.Counts{}, errBoom).Once()
		},
		"storing the request": func(d *staffDeps) {
			d.Customers.EXPECT().Check(mock.Anything, garageID, customerID).Return(nil).Once()
			d.Customers.EXPECT().Anonymize(mock.Anything, garageID, customerID, actorID).Return(usecase.Counts{Customers: 1}, nil).Once()
			d.Repo.EXPECT().Create(mock.Anything, mock.Anything).Return(errBoom).Once()
		},
		"clearing the details": func(d *staffDeps) {
			d.Customers.EXPECT().Check(mock.Anything, garageID, customerID).Return(nil).Once()
			d.Customers.EXPECT().Anonymize(mock.Anything, garageID, customerID, actorID).Return(usecase.Counts{Customers: 1}, nil).Once()
			d.Repo.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()
			d.Repo.EXPECT().ClearDetails(mock.Anything, garageID, customerID).Return(errBoom).Once()
		},
	}

	for name, arrange := range failures {
		t.Run("fails on "+name, func(t *testing.T) {
			t.Parallel()

			uc, d := newStaff(t)
			arrange(d)

			typ := domain.TypeDelete
			if name == "the export" || name == "the document numbers" {
				typ = domain.TypeView
			}

			_, err := uc.Create(t.Context(), req(typ, ""))
			require.Error(t, err)
		})
	}
}

func TestStaffUseCase_Complete(t *testing.T) {
	t.Parallel()

	pending := func() *domain.Request {
		return &domain.Request{ID: requestID, GarageID: garageID, CustomerID: customerID, Type: domain.TypeCorrect, Status: domain.StatusPending}
	}
	req := usecase.CompleteRequest{GarageID: garageID, RequestID: requestID, ActorID: actorID, Outcome: "  Phone changed  "}

	t.Run("completes a waiting correction with the staff member's name", func(t *testing.T) {
		t.Parallel()

		uc, d := newStaff(t)
		d.Repo.EXPECT().FindForUpdate(mock.Anything, garageID, requestID).Return(pending(), nil).Once()
		d.Repo.EXPECT().Save(mock.Anything, mock.MatchedBy(func(r *domain.Request) bool {
			return r.Status == domain.StatusCompleted && r.Outcome == "Phone changed" && *r.CompletedBy == actorID && r.CompletedAt != nil
		})).Return(nil).Once()
		d.Names.EXPECT().UserNames(mock.Anything, []string{actorID}).Return(map[string]string{actorID: "Binh"}, nil).Once()

		got, err := uc.Complete(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "Binh", got.CompletedByName)
	})

	t.Run("declines it with the reason", func(t *testing.T) {
		t.Parallel()

		uc, d := newStaff(t)
		d.Repo.EXPECT().FindForUpdate(mock.Anything, garageID, requestID).Return(pending(), nil).Once()
		d.Repo.EXPECT().Save(mock.Anything, mock.MatchedBy(func(r *domain.Request) bool { return r.Status == domain.StatusRejected })).Return(nil).Once()
		d.Names.EXPECT().UserNames(mock.Anything, mock.Anything).Return(nil, nil).Once()

		declined := req
		declined.Declined = true

		got, err := uc.Complete(t.Context(), declined)
		require.NoError(t, err)
		assert.Equal(t, domain.StatusRejected, got.Status)
	})

	t.Run("a request that is not waiting is refused with its reason", func(t *testing.T) {
		t.Parallel()

		uc, d := newStaff(t)
		done := pending()
		done.Status = domain.StatusCompleted
		d.Repo.EXPECT().FindForUpdate(mock.Anything, garageID, requestID).Return(done, nil).Once()

		_, err := uc.Complete(t.Context(), req)
		assertAppError(t, err, apperror.CodeFailedPrecondition)

		appErr, _ := apperror.As(err)
		assert.Equal(t, domain.ReasonNotPending, appErr.Reason)
	})

	t.Run("needs an outcome of a sensible length", func(t *testing.T) {
		t.Parallel()

		uc, _ := newStaff(t)

		for _, outcome := range []string{"", "   ", strings.Repeat("x", domain.MaxTextLen+1)} {
			r := req
			r.Outcome = outcome

			_, err := uc.Complete(t.Context(), r)
			assertAppError(t, err, apperror.CodeInvalidArgument)
		}
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newStaff(t)
		d.Repo.EXPECT().FindForUpdate(mock.Anything, garageID, requestID).Return(nil, domain.ErrRequestNotFound).Once()

		_, err := uc.Complete(t.Context(), req)
		assertAppError(t, err, apperror.CodeNotFound)

		uc, d = newStaff(t)
		d.Repo.EXPECT().FindForUpdate(mock.Anything, garageID, requestID).Return(pending(), nil).Once()
		d.Repo.EXPECT().Save(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err = uc.Complete(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newStaff(t)
		d.Repo.EXPECT().FindForUpdate(mock.Anything, garageID, requestID).Return(pending(), nil).Once()
		d.Repo.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()
		d.Names.EXPECT().UserNames(mock.Anything, mock.Anything).Return(nil, errBoom).Once()

		_, err = uc.Complete(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestStaffUseCase_GetAndList(t *testing.T) {
	t.Parallel()

	done := func() *domain.Request {
		return &domain.Request{ID: requestID, GarageID: garageID, CompletedBy: new(actorID)}
	}

	t.Run("get names the staff member", func(t *testing.T) {
		t.Parallel()

		uc, d := newStaff(t)
		d.Repo.EXPECT().Find(mock.Anything, garageID, requestID).Return(done(), nil).Once()
		d.Names.EXPECT().UserNames(mock.Anything, []string{actorID}).Return(map[string]string{actorID: "Binh"}, nil).Once()

		got, err := uc.Get(t.Context(), garageID, requestID)
		require.NoError(t, err)
		assert.Equal(t, "Binh", got.CompletedByName)
	})

	t.Run("get fails", func(t *testing.T) {
		t.Parallel()

		uc, d := newStaff(t)
		d.Repo.EXPECT().Find(mock.Anything, garageID, requestID).Return(nil, domain.ErrRequestNotFound).Once()

		_, err := uc.Get(t.Context(), garageID, requestID)
		assertAppError(t, err, apperror.CodeNotFound)

		uc, d = newStaff(t)
		d.Repo.EXPECT().Find(mock.Anything, garageID, requestID).Return(done(), nil).Once()
		d.Names.EXPECT().UserNames(mock.Anything, mock.Anything).Return(nil, errBoom).Once()

		_, err = uc.Get(t.Context(), garageID, requestID)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("list pages, filters and names", func(t *testing.T) {
		t.Parallel()

		uc, d := newStaff(t)
		d.Repo.EXPECT().List(mock.Anything, garageID, usecase.Filter{
			CustomerID: customerID, Type: domain.TypeDelete, Status: domain.StatusPending, Limit: 100, Offset: 100,
		}).Return([]*domain.Request{done()}, 150, nil).Once()
		d.Names.EXPECT().UserNames(mock.Anything, mock.Anything).Return(map[string]string{actorID: "Binh"}, nil).Once()

		page, err := uc.List(t.Context(), usecase.ListRequest{
			GarageID: garageID, CustomerID: customerID, Type: domain.TypeDelete, Status: domain.StatusPending, Page: 2, PageSize: 1000,
		})
		require.NoError(t, err)
		assert.Equal(t, 150, page.Total)
		assert.Equal(t, 100, page.PageSize)
		assert.Equal(t, "Binh", page.Items[0].CompletedByName)
	})

	t.Run("list defaults and failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newStaff(t)
		d.Repo.EXPECT().List(mock.Anything, garageID, usecase.Filter{Limit: usecase.DefaultPageSize}).Return(nil, 0, nil).Once()

		page, err := uc.List(t.Context(), usecase.ListRequest{GarageID: garageID})
		require.NoError(t, err)
		assert.Equal(t, 1, page.Page)

		uc, d = newStaff(t)
		d.Repo.EXPECT().List(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()

		_, err = uc.List(t.Context(), usecase.ListRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newStaff(t)
		d.Repo.EXPECT().List(mock.Anything, garageID, mock.Anything).Return([]*domain.Request{done()}, 1, nil).Once()
		d.Names.EXPECT().UserNames(mock.Anything, mock.Anything).Return(nil, errBoom).Once()

		_, err = uc.List(t.Context(), usecase.ListRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

type publicDeps struct {
	Confirmations *mocks.MockDataRequestConfirmations
	Customers     *mocks.MockDataRequestCustomers
	Documents     *mocks.MockDataRequestDocuments
	Repo          *mocks.MockDataRequestRepository
	Names         *mocks.MockDataRequestNames
	Tx            *mocks.MockDataRequestTransactor
	Mailer        *mocks.MockDataRequestMailer
}

func newPublic(t *testing.T) (*usecase.PublicUseCase, *publicDeps) {
	t.Helper()

	return newPublicLimit(t, 0)
}

// newPublicLimit is newPublic with a limit of requests per source; zero is the default.
func newPublicLimit(t *testing.T, perSource int) (*usecase.PublicUseCase, *publicDeps) {
	t.Helper()

	d := &publicDeps{
		Confirmations: mocks.NewMockDataRequestConfirmations(t), Customers: mocks.NewMockDataRequestCustomers(t),
		Documents: mocks.NewMockDataRequestDocuments(t), Repo: mocks.NewMockDataRequestRepository(t), Names: mocks.NewMockDataRequestNames(t),
		Tx: mocks.NewMockDataRequestTransactor(t), Mailer: mocks.NewMockDataRequestMailer(t),
	}
	d.Tx.EXPECT().InGarage(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, _ string, fn func(context.Context) error) error { return fn(ctx) }).Maybe()

	uc := usecase.NewPublicUseCase(usecase.PublicDeps{
		Confirmations: d.Confirmations, Customers: d.Customers, Documents: d.Documents, Repo: d.Repo, Names: d.Names, Tx: d.Tx,
		Mailer: d.Mailer, BaseURL: "https://app.example.com/", SourceKey: []byte("key"), MaxPerSource: perSource,
	})
	usecase.SetClock(uc, func() time.Time { return now })

	return uc, d
}

// The limit of requests per source is a setting, so an environment whose callers share one address can raise it.
func TestPublicUseCase_SourceLimitCanBeRaised(t *testing.T) {
	t.Parallel()

	req := usecase.SubmitRequest{Email: "binh@example.com", Type: domain.TypeDelete, SourceIP: "203.0.113.5", Locale: "en"}

	uc, d := newPublicLimit(t, 50)
	d.Confirmations.EXPECT().CountByEmail(mock.Anything, mock.Anything, mock.Anything).Return(0, nil).Once()
	d.Confirmations.EXPECT().CountBySource(mock.Anything, mock.Anything, mock.Anything).Return(50, nil).Once()

	assertAppError(t, uc.Submit(t.Context(), req), apperror.CodeResourceExhausted)

	uc, d = newPublicLimit(t, 50)
	d.Confirmations.EXPECT().CountByEmail(mock.Anything, mock.Anything, mock.Anything).Return(0, nil).Once()
	d.Confirmations.EXPECT().CountBySource(mock.Anything, mock.Anything, mock.Anything).Return(usecase.MaxPerSourcePerHour, nil).Once()
	d.Confirmations.EXPECT().CreateConfirmation(mock.Anything, mock.Anything).Return(nil).Once()
	d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Return(nil).Once()

	require.NoError(t, uc.Submit(t.Context(), req))
}

// restored expects the consumed link to be given back.
func (d *publicDeps) restored() {
	d.Confirmations.EXPECT().CreateConfirmation(mock.Anything, mock.Anything).Return(nil).Once()
}

func (d *publicDeps) quiet(count int) {
	d.Confirmations.EXPECT().CountByEmail(mock.Anything, mock.Anything, mock.Anything).Return(count, nil).Maybe()
	d.Confirmations.EXPECT().CountBySource(mock.Anything, mock.Anything, mock.Anything).Return(count, nil).Maybe()
}

func TestPublicUseCase_Submit(t *testing.T) {
	t.Parallel()

	req := usecase.SubmitRequest{Email: " Binh@Example.com ", Type: domain.TypeDelete, SourceIP: "203.0.113.5", Locale: "en"}

	t.Run("stores the request, e-mails the link only to the address and looks nobody up", func(t *testing.T) {
		t.Parallel()

		uc, d := newPublic(t)
		d.quiet(0)

		var sent mail.Message

		d.Confirmations.EXPECT().CreateConfirmation(mock.Anything, mock.MatchedBy(func(c *domain.Confirmation) bool {
			return c.Email == "binh@example.com" && c.Type == domain.TypeDelete && c.Locale == "en" && len(c.TokenHash) == 64 &&
				c.ExpiresAt.Equal(now.Add(usecase.LinkValidity)) && c.SourceHash != "" && c.SourceHash != "203.0.113.5"
		})).Return(nil).Once()
		d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Run(func(_ context.Context, m mail.Message) { sent = m }).Return(nil).Once()

		require.NoError(t, uc.Submit(t.Context(), req))
		assert.Equal(t, "binh@example.com", sent.ToEmail)
		assert.Contains(t, sent.Text, "https://app.example.com/data-request/confirm?token=")
		assert.Contains(t, sent.Subject, "Confirm")
	})

	t.Run("a failing e-mail does not change the answer", func(t *testing.T) {
		t.Parallel()

		uc, d := newPublic(t)
		d.quiet(0)
		d.Confirmations.EXPECT().CreateConfirmation(mock.Anything, mock.Anything).Return(nil).Once()
		d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Return(errBoom).Once()

		require.NoError(t, uc.Submit(t.Context(), req))
	})

	t.Run("a correction keeps its details and the others drop them; Vietnamese is the default language", func(t *testing.T) {
		t.Parallel()

		uc, d := newPublic(t)
		d.quiet(0)
		d.Confirmations.EXPECT().CreateConfirmation(mock.Anything, mock.MatchedBy(func(c *domain.Confirmation) bool {
			return c.Type == domain.TypeCorrect && c.Details == "my phone" && c.Locale == "vi"
		})).Return(nil).Once()
		d.Confirmations.EXPECT().CreateConfirmation(mock.Anything, mock.MatchedBy(func(c *domain.Confirmation) bool {
			return c.Type == domain.TypeView && c.Details == ""
		})).Return(nil).Once()
		d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Return(nil).Twice()

		require.NoError(t, uc.Submit(t.Context(), usecase.SubmitRequest{Email: "a@b.vn", Type: domain.TypeCorrect, Details: "my phone", Locale: "fr"}))
		require.NoError(t, uc.Submit(t.Context(), usecase.SubmitRequest{Email: "a@b.vn", Type: domain.TypeView, Details: "dropped"}))
	})

	t.Run("refuses a bad address, type or correction before anything is stored", func(t *testing.T) {
		t.Parallel()

		uc, _ := newPublic(t)

		for name, r := range map[string]usecase.SubmitRequest{
			"address":     {Email: "not an address", Type: domain.TypeView},
			"no address":  {Type: domain.TypeView},
			"type":        {Email: "a@b.vn", Type: "erase"},
			"correction":  {Email: "a@b.vn", Type: domain.TypeCorrect},
			"long detail": {Email: "a@b.vn", Type: domain.TypeCorrect, Details: strings.Repeat("x", domain.MaxTextLen+1)},
		} {
			assertAppError(t, uc.Submit(t.Context(), r), apperror.CodeInvalidArgument)

			_ = name
		}
	})

	t.Run("an address or a source that asked too often is refused", func(t *testing.T) {
		t.Parallel()

		uc, d := newPublic(t)
		d.Confirmations.EXPECT().CountByEmail(mock.Anything, "binh@example.com", now.Add(-time.Hour)).Return(usecase.MaxPerAddressPerHour, nil).Once()
		d.Confirmations.EXPECT().CountBySource(mock.Anything, mock.Anything, now.Add(-time.Hour)).Return(0, nil).Once()

		assertAppError(t, uc.Submit(t.Context(), req), apperror.CodeResourceExhausted)

		uc, d = newPublic(t)
		d.Confirmations.EXPECT().CountByEmail(mock.Anything, mock.Anything, mock.Anything).Return(0, nil).Once()
		d.Confirmations.EXPECT().CountBySource(mock.Anything, mock.Anything, mock.Anything).Return(usecase.MaxPerSourcePerHour, nil).Once()

		assertAppError(t, uc.Submit(t.Context(), req), apperror.CodeResourceExhausted)
	})

	t.Run("an IPv6 source counts by its /64", func(t *testing.T) {
		t.Parallel()

		var first, second string

		uc, d := newPublic(t)
		d.Confirmations.EXPECT().CountByEmail(mock.Anything, mock.Anything, mock.Anything).Return(0, nil).Twice()
		d.Confirmations.EXPECT().CountBySource(mock.Anything, mock.Anything, mock.Anything).Return(0, nil).Twice()
		d.Confirmations.EXPECT().CreateConfirmation(mock.Anything, mock.Anything).Run(func(_ context.Context, c *domain.Confirmation) {
			if first == "" {
				first = c.SourceHash
			} else {
				second = c.SourceHash
			}
		}).Return(nil).Twice()
		d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Return(nil).Twice()

		require.NoError(t, uc.Submit(t.Context(), usecase.SubmitRequest{Email: "a@b.vn", Type: domain.TypeView, SourceIP: "2001:db8::1"}))
		require.NoError(t, uc.Submit(t.Context(), usecase.SubmitRequest{Email: "a@b.vn", Type: domain.TypeView, SourceIP: "2001:db8::ffff"}))
		assert.Equal(t, first, second)
	})
}

func TestPublicUseCase_SubmitFailures(t *testing.T) {
	t.Parallel()

	req := usecase.SubmitRequest{Email: " Binh@Example.com ", Type: domain.TypeDelete, SourceIP: "203.0.113.5", Locale: "en"}

	failures := map[string]func(d *publicDeps){
		"counting by address": func(d *publicDeps) {
			d.Confirmations.EXPECT().CountByEmail(mock.Anything, mock.Anything, mock.Anything).Return(0, errBoom).Once()
		},
		"counting by source": func(d *publicDeps) {
			d.Confirmations.EXPECT().CountByEmail(mock.Anything, mock.Anything, mock.Anything).Return(0, nil).Once()
			d.Confirmations.EXPECT().CountBySource(mock.Anything, mock.Anything, mock.Anything).Return(0, errBoom).Once()
		},
		"storing": func(d *publicDeps) {
			d.quiet(0)
			d.Confirmations.EXPECT().CreateConfirmation(mock.Anything, mock.Anything).Return(errBoom).Once()
		},
	}

	for name, arrange := range failures {
		t.Run("fails on "+name, func(t *testing.T) {
			t.Parallel()

			uc, d := newPublic(t)
			arrange(d)

			assertAppError(t, uc.Submit(t.Context(), req), apperror.CodeInternal)
		})
	}
}

func confirmation(typ domain.Type) *domain.Confirmation {
	return &domain.Confirmation{ID: "c-1", Email: "binh@example.com", Type: typ, Details: "my phone", Locale: "vi", ExpiresAt: now.Add(time.Hour)}
}

func TestPublicUseCase_Confirm(t *testing.T) {
	t.Parallel()

	refs := []usecase.CustomerRef{{ID: customerID, GarageID: garageID}, {ID: otherID, GarageID: garageTwo}}

	t.Run("a deletion anonymizes the address's customer in each garage and logs it there", func(t *testing.T) {
		t.Parallel()

		uc, d := newPublic(t)
		d.Confirmations.EXPECT().Consume(mock.Anything, mock.Anything, now).Return(confirmation(domain.TypeDelete), nil).Once()
		d.Customers.EXPECT().ByEmail(mock.Anything, "binh@example.com").Return(refs, nil).Once()
		d.Customers.EXPECT().Anonymize(mock.Anything, garageID, customerID, "").Return(usecase.Counts{Customers: 1}, nil).Once()
		d.Customers.EXPECT().Anonymize(mock.Anything, garageTwo, otherID, "").Return(usecase.Counts{Customers: 1, Vehicles: 1}, nil).Once()
		d.Repo.EXPECT().Create(mock.Anything, mock.MatchedBy(func(r *domain.Request) bool {
			return r.Source == domain.SourcePublic && r.Type == domain.TypeDelete && r.Status == domain.StatusCompleted && r.RequestedBy == nil
		})).Return(nil).Twice()
		d.Repo.EXPECT().ClearDetails(mock.Anything, garageID, customerID).Return(nil).Once()
		d.Repo.EXPECT().ClearDetails(mock.Anything, garageTwo, otherID).Return(nil).Once()

		n, err := uc.Confirm(t.Context(), "token")
		require.NoError(t, err)
		assert.Equal(t, 2, n)
	})

	t.Run("a view is e-mailed to the confirmed address with each garage's section", func(t *testing.T) {
		t.Parallel()

		uc, d := newPublic(t)
		d.Confirmations.EXPECT().Consume(mock.Anything, mock.Anything, now).Return(confirmation(domain.TypeView), nil).Once()
		d.Customers.EXPECT().ByEmail(mock.Anything, "binh@example.com").Return(refs, nil).Once()
		d.Customers.EXPECT().Export(mock.Anything, mock.Anything, mock.Anything).Return(json.RawMessage(`{"customer":{}}`), nil).Twice()
		d.Documents.EXPECT().Numbers(mock.Anything, mock.Anything, mock.Anything).Return(usecase.DocumentNumbers{}, nil).Twice()
		d.Repo.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Twice()
		d.Names.EXPECT().GarageName(mock.Anything, garageID).Return("Gara An", nil).Once()
		d.Names.EXPECT().GarageName(mock.Anything, garageTwo).Return("Gara Binh", nil).Once()

		var sent mail.Message

		d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Run(func(_ context.Context, m mail.Message) { sent = m }).Return(nil).Once()

		n, err := uc.Confirm(t.Context(), "token")
		require.NoError(t, err)
		assert.Equal(t, 2, n)
		assert.Equal(t, "binh@example.com", sent.ToEmail)
		assert.Contains(t, sent.Text, "Gara An")
		assert.Contains(t, sent.Text, "Gara Binh")
	})

	t.Run("a correction is passed to each garage as a waiting request", func(t *testing.T) {
		t.Parallel()

		uc, d := newPublic(t)
		d.Confirmations.EXPECT().Consume(mock.Anything, mock.Anything, now).Return(confirmation(domain.TypeCorrect), nil).Once()
		d.Customers.EXPECT().ByEmail(mock.Anything, "binh@example.com").Return(refs[:1], nil).Once()
		d.Repo.EXPECT().Create(mock.Anything, mock.MatchedBy(func(r *domain.Request) bool {
			return r.Status == domain.StatusPending && r.Details == "my phone" && r.Source == domain.SourcePublic
		})).Return(nil).Once()

		n, err := uc.Confirm(t.Context(), "token")
		require.NoError(t, err)
		assert.Equal(t, 1, n)
	})

	t.Run("an address no garage holds reaches none and sends nothing", func(t *testing.T) {
		t.Parallel()

		uc, d := newPublic(t)
		d.Confirmations.EXPECT().Consume(mock.Anything, mock.Anything, now).Return(confirmation(domain.TypeView), nil).Once()
		d.Customers.EXPECT().ByEmail(mock.Anything, mock.Anything).Return(nil, nil).Once()

		n, err := uc.Confirm(t.Context(), "token")
		require.NoError(t, err)
		assert.Zero(t, n)
	})

	t.Run("an empty, unknown, used or expired token is not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newPublic(t)

		_, err := uc.Confirm(t.Context(), "  ")
		assertAppError(t, err, apperror.CodeNotFound)

		d.Confirmations.EXPECT().Consume(mock.Anything, mock.Anything, now).Return(nil, domain.ErrTokenInvalid).Once()

		_, err = uc.Confirm(t.Context(), "wrong")
		assertAppError(t, err, apperror.CodeNotFound)
	})
}

func TestPublicUseCase_ConfirmFailures(t *testing.T) {
	t.Parallel()

	failures := map[string]func(d *publicDeps) domain.Type{
		"consuming": func(d *publicDeps) domain.Type {
			d.Confirmations.EXPECT().Consume(mock.Anything, mock.Anything, now).Return(nil, errBoom).Once()

			return domain.TypeView
		},
		"looking the address up": func(d *publicDeps) domain.Type {
			d.Confirmations.EXPECT().Consume(mock.Anything, mock.Anything, now).Return(confirmation(domain.TypeDelete), nil).Once()
			d.Customers.EXPECT().ByEmail(mock.Anything, mock.Anything).Return(nil, errBoom).Once()
			d.restored()

			return domain.TypeDelete
		},
		"acting in a garage": func(d *publicDeps) domain.Type {
			d.Confirmations.EXPECT().Consume(mock.Anything, mock.Anything, now).Return(confirmation(domain.TypeDelete), nil).Once()
			d.Customers.EXPECT().ByEmail(mock.Anything, mock.Anything).Return([]usecase.CustomerRef{{ID: customerID, GarageID: garageID}}, nil).Once()
			d.Customers.EXPECT().Anonymize(mock.Anything, garageID, customerID, "").Return(usecase.Counts{}, errBoom).Once()
			d.restored()

			return domain.TypeDelete
		},
		"storing in a garage": func(d *publicDeps) domain.Type {
			d.Confirmations.EXPECT().Consume(mock.Anything, mock.Anything, now).Return(confirmation(domain.TypeCorrect), nil).Once()
			d.Customers.EXPECT().ByEmail(mock.Anything, mock.Anything).Return([]usecase.CustomerRef{{ID: customerID, GarageID: garageID}}, nil).Once()
			d.Repo.EXPECT().Create(mock.Anything, mock.Anything).Return(errBoom).Once()
			d.restored()

			return domain.TypeCorrect
		},
		"clearing the details": func(d *publicDeps) domain.Type {
			d.Confirmations.EXPECT().Consume(mock.Anything, mock.Anything, now).Return(confirmation(domain.TypeDelete), nil).Once()
			d.Customers.EXPECT().ByEmail(mock.Anything, mock.Anything).Return([]usecase.CustomerRef{{ID: customerID, GarageID: garageID}}, nil).Once()
			d.Customers.EXPECT().Anonymize(mock.Anything, garageID, customerID, "").Return(usecase.Counts{Customers: 1}, nil).Once()
			d.Repo.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()
			d.Repo.EXPECT().ClearDetails(mock.Anything, garageID, customerID).Return(errBoom).Once()
			d.restored()

			return domain.TypeDelete
		},
		"naming the garage": func(d *publicDeps) domain.Type {
			d.Confirmations.EXPECT().Consume(mock.Anything, mock.Anything, now).Return(confirmation(domain.TypeView), nil).Once()
			d.Customers.EXPECT().ByEmail(mock.Anything, mock.Anything).Return([]usecase.CustomerRef{{ID: customerID, GarageID: garageID}}, nil).Once()
			d.Customers.EXPECT().Export(mock.Anything, mock.Anything, mock.Anything).Return(json.RawMessage(`{}`), nil).Once()
			d.Documents.EXPECT().Numbers(mock.Anything, mock.Anything, mock.Anything).Return(usecase.DocumentNumbers{}, nil).Once()
			d.Repo.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()
			d.Names.EXPECT().GarageName(mock.Anything, garageID).Return("", errBoom).Once()
			d.restored()

			return domain.TypeView
		},
		"mailing the view": func(d *publicDeps) domain.Type {
			d.Confirmations.EXPECT().Consume(mock.Anything, mock.Anything, now).Return(confirmation(domain.TypeView), nil).Once()
			d.Customers.EXPECT().ByEmail(mock.Anything, mock.Anything).Return([]usecase.CustomerRef{{ID: customerID, GarageID: garageID}}, nil).Once()
			d.Customers.EXPECT().Export(mock.Anything, mock.Anything, mock.Anything).Return(json.RawMessage(`{}`), nil).Once()
			d.Documents.EXPECT().Numbers(mock.Anything, mock.Anything, mock.Anything).Return(usecase.DocumentNumbers{}, nil).Once()
			d.Repo.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()
			d.Names.EXPECT().GarageName(mock.Anything, garageID).Return("Gara", nil).Once()
			d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Return(errBoom).Once()
			d.restored()

			return domain.TypeView
		},
	}

	for name, arrange := range failures {
		t.Run("fails on "+name, func(t *testing.T) {
			t.Parallel()

			uc, d := newPublic(t)
			arrange(d)

			_, err := uc.Confirm(t.Context(), "token")
			assertAppError(t, err, apperror.CodeInternal)
		})
	}
}

func TestPublicUseCase_PurgeAndSchedule(t *testing.T) {
	t.Parallel()

	uc, d := newPublic(t)
	d.Confirmations.EXPECT().PurgeExpired(mock.Anything, now).Return(3, nil).Once()

	n, err := uc.Purge(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	d.Confirmations.EXPECT().PurgeExpired(mock.Anything, mock.Anything).Return(0, errBoom).Once()

	_, err = uc.Purge(t.Context())
	assertAppError(t, err, apperror.CodeInternal)

	// Scheduled: purges once at once, then stops with the context.
	ctx, cancel := context.WithCancel(t.Context())

	d.Confirmations.EXPECT().PurgeExpired(mock.Anything, mock.Anything).Run(func(context.Context, time.Time) { cancel() }).Return(0, nil).Once()

	uc.Schedule(ctx, time.Hour)

	// A failing purge is logged, not fatal.
	ctx, cancel = context.WithCancel(t.Context())

	d.Confirmations.EXPECT().PurgeExpired(mock.Anything, mock.Anything).Run(func(context.Context, time.Time) { cancel() }).Return(0, errBoom).Once()

	uc.Schedule(ctx, time.Hour)
}

func TestPublicUseCase_ConfirmGivesTheLinkBackEvenWhenThatFails(t *testing.T) {
	t.Parallel()

	uc, d := newPublic(t)
	c := confirmation(domain.TypeDelete)
	d.Confirmations.EXPECT().Consume(mock.Anything, mock.Anything, now).Return(c, nil).Once()
	d.Customers.EXPECT().ByEmail(mock.Anything, mock.Anything).Return(nil, errBoom).Once()
	d.Confirmations.EXPECT().CreateConfirmation(mock.Anything, c).Return(errBoom).Once()

	_, err := uc.Confirm(t.Context(), "token")
	assertAppError(t, err, apperror.CodeInternal)
}

func TestPublicUseCase_SubmitWithoutASourceIsNotCountedAgainstAnyBucket(t *testing.T) {
	t.Parallel()

	uc, d := newPublic(t)
	d.Confirmations.EXPECT().CountByEmail(mock.Anything, mock.Anything, mock.Anything).Return(0, nil).Once()
	d.Confirmations.EXPECT().CreateConfirmation(mock.Anything, mock.MatchedBy(func(c *domain.Confirmation) bool { return c.SourceHash == "" })).Return(nil).Once()
	d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Return(nil).Once()

	require.NoError(t, uc.Submit(t.Context(), usecase.SubmitRequest{Email: "a@b.vn", Type: domain.TypeView}))
}

func TestPublicUseCase_ConfirmNamesAGarageOnce(t *testing.T) {
	t.Parallel()

	uc, d := newPublic(t)
	d.Confirmations.EXPECT().Consume(mock.Anything, mock.Anything, now).Return(confirmation(domain.TypeView), nil).Once()
	d.Customers.EXPECT().ByEmail(mock.Anything, mock.Anything).Return([]usecase.CustomerRef{{ID: customerID, GarageID: garageID}, {ID: otherID, GarageID: garageID}}, nil).Once()
	d.Customers.EXPECT().Export(mock.Anything, mock.Anything, mock.Anything).Return(json.RawMessage(`{}`), nil).Twice()
	d.Documents.EXPECT().Numbers(mock.Anything, mock.Anything, mock.Anything).Return(usecase.DocumentNumbers{}, nil).Twice()
	d.Repo.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Twice()
	d.Names.EXPECT().GarageName(mock.Anything, garageID).Return("Gara An", nil).Once()
	d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Return(nil).Once()

	n, err := uc.Confirm(t.Context(), "token")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}
