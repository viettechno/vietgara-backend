package usecase_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/issuer"
)

func voucherRequest(allocations ...usecase.AllocationInput) usecase.CreateVoucherRequest {
	return usecase.CreateVoucherRequest{
		GarageID: garageID, ActorID: actorID, SupplierID: supplierID, Amount: 1_000_000, Method: "cash", Allocations: allocations,
	}
}

func (d *payableDeps) supplier() {
	d.Suppliers.EXPECT().Get(mock.Anything, garageID, supplierID).Return(usecase.SupplierView{Name: "Sao Mai", Address: "12 Le Loi"}, nil).Maybe()
}

func TestPayableUseCase_CreateVoucher(t *testing.T) {
	t.Parallel()

	t.Run("issues a voucher, applies the allocations and audits it", func(t *testing.T) {
		t.Parallel()

		uc, d := newPayableUseCase(t)
		d.issuing()
		d.supplier()

		one, two := openPayable(payableID, supplierID, 700_000, 0), openPayable(payableTwoID, supplierID, 300_000, 100_000)
		d.Repo.EXPECT().LockPayables(mock.Anything, garageID, []string{payableID, payableTwoID}).Return([]*domain.Payable{one, two}, nil).Once()

		var voucher *domain.PaymentOutVoucher

		d.Repo.EXPECT().AddVoucher(mock.Anything, mock.Anything).
			Run(func(_ context.Context, v *domain.PaymentOutVoucher) { voucher = v }).Return(nil).Once()

		var saved []*domain.Payable

		d.Repo.EXPECT().SavePayable(mock.Anything, mock.Anything).Run(func(_ context.Context, p *domain.Payable) { saved = append(saved, p) }).Return(nil).Twice()

		entry := d.expectAudit()

		v, err := uc.CreateVoucher(t.Context(), voucherRequest(
			usecase.AllocationInput{PayableID: payableID, Amount: 700_000}, usecase.AllocationInput{PayableID: payableTwoID, Amount: 200_000}))
		require.NoError(t, err)
		require.Same(t, voucher, v)
		require.Equal(t, "PC-2026-00007", v.Number)
		require.Equal(t, "Sao Mai", v.PayeeName, "the payee defaults to the supplier")
		require.Equal(t, "12 Le Loi", v.PayeeAddress)
		require.Equal(t, "Binh", v.IssuerName)
		require.JSONEq(t, `{"name":"Gara An Phat"}`, string(v.Issuer))
		require.Contains(t, v.Reason, "Thanh toán tiền hàng theo phiếu nhập")
		require.EqualValues(t, 100_000, v.Unallocated())
		require.Len(t, v.Allocations, 2)
		require.Equal(t, domain.PayablePaid, one.Status)
		require.EqualValues(t, 700_000, one.PaidAmount)
		require.Equal(t, domain.PayablePaid, two.Status)
		require.Len(t, saved, 2)
		require.Equal(t, usecase.ActionVoucherIssued, entry.Action)
	})

	t.Run("keeps what the staff typed and may pay nothing off (an advance)", func(t *testing.T) {
		t.Parallel()

		uc, d := newPayableUseCase(t)
		d.issuing()
		d.supplier()
		d.Repo.EXPECT().AddVoucher(mock.Anything, mock.Anything).Return(nil).Once()
		d.expectAudit()

		req := voucherRequest()
		req.PayeeName, req.PayeeAddress, req.Reason, req.Note = " Anh Tu ", "5 Hai Ba Trung", "Ung truoc", "ghi chu"

		v, err := uc.CreateVoucher(t.Context(), req)
		require.NoError(t, err)
		require.Equal(t, "Anh Tu", v.PayeeName)
		require.Equal(t, "Ung truoc", v.Reason)
		require.EqualValues(t, 1_000_000, v.Unallocated())

		uc, d = newPayableUseCase(t)
		d.issuing()
		d.supplier()
		d.Repo.EXPECT().AddVoucher(mock.Anything, mock.Anything).Return(nil).Once()
		d.expectAudit()

		v, err = uc.CreateVoucher(t.Context(), voucherRequest())
		require.NoError(t, err)
		require.Equal(t, "Thanh toán tiền hàng", v.Reason)
	})
}

func TestPayableUseCase_CreateVoucher_Refused(t *testing.T) {
	t.Parallel()

	t.Run("refuses what is wrong before reading anything", func(t *testing.T) {
		t.Parallel()

		long := strings.Repeat("x", 600)
		one := usecase.AllocationInput{PayableID: payableID, Amount: 100}

		cases := map[string]struct {
			mutate func(r *usecase.CreateVoucherRequest)
			code   apperror.Code
			reason string
		}{
			"supplier not an id":  {func(r *usecase.CreateVoucherRequest) { r.SupplierID = "x" }, apperror.CodeNotFound, usecase.ReasonSupplierNotFound},
			"bad method":          {func(r *usecase.CreateVoucherRequest) { r.Method = "card" }, apperror.CodeInvalidArgument, ""},
			"payee too long":      {func(r *usecase.CreateVoucherRequest) { r.PayeeName = long }, apperror.CodeInvalidArgument, ""},
			"address too long":    {func(r *usecase.CreateVoucherRequest) { r.PayeeAddress = long }, apperror.CodeInvalidArgument, ""},
			"reason too long":     {func(r *usecase.CreateVoucherRequest) { r.Reason = long }, apperror.CodeInvalidArgument, ""},
			"note too long":       {func(r *usecase.CreateVoucherRequest) { r.Note = strings.Repeat("n", 1001) }, apperror.CodeInvalidArgument, ""},
			"zero amount":         {func(r *usecase.CreateVoucherRequest) { r.Amount = 0 }, apperror.CodeInvalidArgument, usecase.ReasonInvalidPaymentOutAmount},
			"less than allocated": {func(r *usecase.CreateVoucherRequest) { r.Amount = 50; r.Allocations = []usecase.AllocationInput{one} }, apperror.CodeInvalidArgument, usecase.ReasonInvalidPaymentOutAmount},
			"payable not an id": {func(r *usecase.CreateVoucherRequest) {
				r.Allocations = []usecase.AllocationInput{{PayableID: "x", Amount: 1}}
			}, apperror.CodeInvalidArgument, usecase.ReasonInvalidAllocation},
			"zero allocation": {func(r *usecase.CreateVoucherRequest) {
				r.Allocations = []usecase.AllocationInput{{PayableID: payableID}}
			}, apperror.CodeInvalidArgument, usecase.ReasonInvalidAllocation},
			"payable twice": {func(r *usecase.CreateVoucherRequest) { r.Allocations = []usecase.AllocationInput{one, one} }, apperror.CodeInvalidArgument, usecase.ReasonInvalidAllocation},
		}

		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				uc, _ := newPayableUseCase(t)

				req := voucherRequest()
				tc.mutate(&req)

				_, err := uc.CreateVoucher(t.Context(), req)
				assertAppError(t, err, tc.code)

				if tc.reason != "" {
					appErr, _ := apperror.As(err)
					require.Equal(t, tc.reason, appErr.Reason)
				}
			})
		}
	})

	t.Run("refuses an allocation that does not fit its payable", func(t *testing.T) {
		t.Parallel()

		paid := openPayable(payableID, supplierID, 100, 100)
		cases := map[string][]*domain.Payable{
			"another supplier's":  {openPayable(payableID, otherSupp, 700_000, 0)},
			"already paid":        {paid},
			"more than remaining": {openPayable(payableID, supplierID, 700_000, 650_000)},
			"cancelled": {func() *domain.Payable {
				p := openPayable(payableID, supplierID, 700_000, 0)
				p.Status = domain.PayableCancelled

				return p
			}()},
			"not found": nil,
		}

		for name, locked := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				uc, d := newPayableUseCase(t)
				d.supplier()
				d.Repo.EXPECT().LockPayables(mock.Anything, garageID, []string{payableID}).Return(locked, nil).Once()

				_, err := uc.CreateVoucher(t.Context(), voucherRequest(usecase.AllocationInput{PayableID: payableID, Amount: 100_000}))
				assertAppError(t, err, apperror.CodeInvalidArgument)

				appErr, _ := apperror.As(err)
				require.Equal(t, usecase.ReasonInvalidAllocation, appErr.Reason)
			})
		}
	})
}

func TestPayableUseCase_CreateVoucher_Failures(t *testing.T) {
	t.Parallel()

	t.Run("fails when something it reads or writes fails", func(t *testing.T) {
		t.Parallel()

		alloc := usecase.AllocationInput{PayableID: payableID, Amount: 100}
		fit := func(d *payableDeps) {
			d.Repo.EXPECT().LockPayables(mock.Anything, garageID, []string{payableID}).Return([]*domain.Payable{openPayable(payableID, supplierID, 700_000, 0)}, nil).Maybe()
		}

		cases := map[string]func(d *payableDeps){
			"supplier": func(d *payableDeps) {
				d.Suppliers.EXPECT().Get(mock.Anything, garageID, supplierID).Return(usecase.SupplierView{}, domain.ErrSupplierNotFound).Once()
			},
			"lock": func(d *payableDeps) {
				d.supplier()
				d.Repo.EXPECT().LockPayables(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
			},
			"timezone": func(d *payableDeps) {
				d.supplier()
				fit(d)
				d.Garages.EXPECT().Timezone(mock.Anything, garageID).Return("", errBoom).Once()
			},
			"number": func(d *payableDeps) {
				d.supplier()
				fit(d)
				d.timezone()
				d.Sequences.EXPECT().Next(mock.Anything, garageID, "payment_out", "PC-2026-{SEQ:5}").Return(0, errBoom).Once()
			},
			"issuer": func(d *payableDeps) {
				d.supplier()
				fit(d)
				d.timezone()
				d.Sequences.EXPECT().Next(mock.Anything, garageID, "payment_out", "PC-2026-{SEQ:5}").Return(1, nil).Once()
				d.Issuers.EXPECT().Issuer(mock.Anything, garageID).Return(issuer.Issuer{}, errBoom).Once()
			},
			"staff": func(d *payableDeps) {
				d.supplier()
				fit(d)
				d.issuing()
				d.Staff.ExpectedCalls = nil
				d.Staff.EXPECT().FullName(mock.Anything, actorID).Return("", errBoom).Once()
			},
			"add voucher": func(d *payableDeps) {
				d.supplier()
				fit(d)
				d.issuing()
				d.Repo.EXPECT().AddVoucher(mock.Anything, mock.Anything).Return(errBoom).Once()
			},
			"save payable": func(d *payableDeps) {
				d.supplier()
				fit(d)
				d.issuing()
				d.Repo.EXPECT().AddVoucher(mock.Anything, mock.Anything).Return(nil).Once()
				d.Repo.EXPECT().SavePayable(mock.Anything, mock.Anything).Return(errBoom).Once()
			},
			"audit": func(d *payableDeps) {
				d.supplier()
				fit(d)
				d.issuing()
				d.Repo.EXPECT().AddVoucher(mock.Anything, mock.Anything).Return(nil).Once()
				d.Repo.EXPECT().SavePayable(mock.Anything, mock.Anything).Return(nil).Once()
				d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(errBoom).Once()
			},
		}

		for name, arrange := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				uc, d := newPayableUseCase(t)
				arrange(d)

				_, err := uc.CreateVoucher(t.Context(), voucherRequest(alloc))
				require.Error(t, err)
			})
		}
	})
}

func TestPayableUseCase_VoidVoucher(t *testing.T) {
	t.Parallel()

	stored := func() *domain.PaymentOutVoucher {
		return &domain.PaymentOutVoucher{
			ID: voucherID, GarageID: garageID, Number: "PC-2026-00001", SupplierID: supplierID, Amount: 1_000_000, Method: "cash",
			Allocations: []domain.Allocation{{PayableID: payableID, Amount: 700_000}, {PayableID: payableTwoID, Amount: 100_000}},
		}
	}
	request := usecase.VoidVoucherRequest{GarageID: garageID, VoucherID: voucherID, ActorID: actorID, Reason: " wrong supplier "}

	t.Run("takes the allocations off the payables that still stand", func(t *testing.T) {
		t.Parallel()

		uc, d := newPayableUseCase(t)
		d.Repo.EXPECT().VoidVoucher(mock.Anything, garageID, voucherID, "wrong supplier", now).Return(nil).Once()
		d.Repo.EXPECT().FindVoucher(mock.Anything, garageID, voucherID).Return(stored(), nil).Once()

		paid := openPayable(payableID, supplierID, 700_000, 700_000)
		cancelled := openPayable(payableTwoID, supplierID, 300_000, 100_000)
		cancelled.Status = domain.PayableCancelled

		d.Repo.EXPECT().LockPayables(mock.Anything, garageID, []string{payableID, payableTwoID}).Return([]*domain.Payable{paid, cancelled}, nil).Once()
		d.Repo.EXPECT().SavePayable(mock.Anything, paid).Return(nil).Once()

		entry := d.expectAudit()

		_, err := uc.VoidVoucher(t.Context(), request)
		require.NoError(t, err)
		require.Equal(t, domain.PayableOpen, paid.Status, "the payable is open again")
		require.Zero(t, paid.PaidAmount)
		require.Equal(t, domain.PayableCancelled, cancelled.Status, "a cancelled payable stays cancelled and untouched")
		require.EqualValues(t, 100_000, cancelled.PaidAmount)
		require.Equal(t, usecase.ActionVoucherVoided, entry.Action)
	})

	t.Run("a voucher with no allocations touches no payable", func(t *testing.T) {
		t.Parallel()

		uc, d := newPayableUseCase(t)
		d.Repo.EXPECT().VoidVoucher(mock.Anything, garageID, voucherID, "wrong supplier", now).Return(nil).Once()
		d.Repo.EXPECT().FindVoucher(mock.Anything, garageID, voucherID).Return(&domain.PaymentOutVoucher{ID: voucherID, GarageID: garageID}, nil).Once()
		d.expectAudit()

		_, err := uc.VoidVoucher(t.Context(), request)
		require.NoError(t, err)
	})

	t.Run("refuses a reason that is empty or too long and an id that is not one", func(t *testing.T) {
		t.Parallel()

		uc, _ := newPayableUseCase(t)

		for _, reason := range []string{"", "   ", strings.Repeat("r", 501)} {
			req := request
			req.Reason = reason

			_, err := uc.VoidVoucher(t.Context(), req)
			assertAppError(t, err, apperror.CodeInvalidArgument)
		}

		req := request
		req.VoucherID = "x"

		_, err := uc.VoidVoucher(t.Context(), req)
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("a voucher voided twice, or unknown, is refused", func(t *testing.T) {
		t.Parallel()

		uc, d := newPayableUseCase(t)
		d.Repo.EXPECT().VoidVoucher(mock.Anything, garageID, voucherID, mock.Anything, mock.Anything).Return(domain.ErrPaymentOutAlreadyVoided).Once()

		_, err := uc.VoidVoucher(t.Context(), request)
		assertAppError(t, err, apperror.CodeAlreadyExists)

		uc, d = newPayableUseCase(t)
		d.Repo.EXPECT().VoidVoucher(mock.Anything, garageID, voucherID, mock.Anything, mock.Anything).Return(domain.ErrVoucherNotFound).Once()

		_, err = uc.VoidVoucher(t.Context(), request)
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("fails when something it reads or writes fails", func(t *testing.T) {
		t.Parallel()

		cases := map[string]func(d *payableDeps){
			"find": func(d *payableDeps) {
				d.Repo.EXPECT().FindVoucher(mock.Anything, garageID, voucherID).Return(nil, errBoom).Once()
			},
			"lock": func(d *payableDeps) {
				d.Repo.EXPECT().FindVoucher(mock.Anything, garageID, voucherID).Return(stored(), nil).Once()
				d.Repo.EXPECT().LockPayables(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
			},
			"save": func(d *payableDeps) {
				d.Repo.EXPECT().FindVoucher(mock.Anything, garageID, voucherID).Return(stored(), nil).Once()
				d.Repo.EXPECT().LockPayables(mock.Anything, garageID, mock.Anything).Return([]*domain.Payable{openPayable(payableID, supplierID, 700_000, 700_000)}, nil).Once()
				d.Repo.EXPECT().SavePayable(mock.Anything, mock.Anything).Return(errBoom).Once()
			},
			"audit": func(d *payableDeps) {
				d.Repo.EXPECT().FindVoucher(mock.Anything, garageID, voucherID).Return(&domain.PaymentOutVoucher{ID: voucherID, GarageID: garageID}, nil).Once()
				d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(errBoom).Once()
			},
		}

		for name, arrange := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				uc, d := newPayableUseCase(t)
				d.Repo.EXPECT().VoidVoucher(mock.Anything, garageID, voucherID, mock.Anything, mock.Anything).Return(nil).Once()
				arrange(d)

				_, err := uc.VoidVoucher(t.Context(), request)
				require.Error(t, err)
			})
		}
	})
}

func TestPayableUseCase_ListAndGetVouchers(t *testing.T) {
	t.Parallel()

	t.Run("lists a page", func(t *testing.T) {
		t.Parallel()

		uc, d := newPayableUseCase(t)
		d.Repo.EXPECT().ListVouchers(mock.Anything, garageID, usecase.VoucherFilter{SupplierID: supplierID, IncludeVoided: true, Limit: 5, Offset: 5}).
			Return([]*domain.PaymentOutVoucher{{ID: voucherID}}, 6, nil).Once()

		res, err := uc.ListVouchers(t.Context(), usecase.ListVouchersRequest{
			GarageID: garageID, SupplierID: supplierID, IncludeVoided: true, PageRequest: usecase.PageRequest{Page: 2, PageSize: 5},
		})
		require.NoError(t, err)
		require.Equal(t, 6, res.Total)
		require.Len(t, res.Items, 1)
	})

	t.Run("refuses a supplier that is not an id and passes on a failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newPayableUseCase(t)
		_, err := uc.ListVouchers(t.Context(), usecase.ListVouchersRequest{GarageID: garageID, SupplierID: "x"})
		assertAppError(t, err, apperror.CodeInvalidArgument)

		d.Repo.EXPECT().ListVouchers(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()

		_, err = uc.ListVouchers(t.Context(), usecase.ListVouchersRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("gets one, or says it is unknown", func(t *testing.T) {
		t.Parallel()

		uc, d := newPayableUseCase(t)
		d.Repo.EXPECT().FindVoucher(mock.Anything, garageID, voucherID).Return(&domain.PaymentOutVoucher{ID: voucherID}, nil).Once()

		v, err := uc.GetVoucher(t.Context(), garageID, voucherID)
		require.NoError(t, err)
		require.Equal(t, voucherID, v.ID)

		_, err = uc.GetVoucher(t.Context(), garageID, "x")
		assertAppError(t, err, apperror.CodeNotFound)

		d.Repo.EXPECT().FindVoucher(mock.Anything, garageID, voucherID).Return(nil, domain.ErrVoucherNotFound).Once()

		_, err = uc.GetVoucher(t.Context(), garageID, voucherID)
		assertAppError(t, err, apperror.CodeNotFound)
	})
}
