package usecase_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const orderID = "77777777-7777-7777-7777-777777777777"

func contacts(email string) map[string]usecase.SupplierContact {
	return map[string]usecase.SupplierContact{supplierA: {Name: "Supplier 1", Email: email}}
}

// orderUseCase is the use case with the lookups every order operation makes.
func orderUseCase(t *testing.T) (*usecase.InventoryUseCase, *deps) {
	t.Helper()

	uc, d := newUseCase(t)
	d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Maybe()
	d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Maybe()
	d.Directory.EXPECT().SupplierContacts(mock.Anything, garageID, mock.Anything).Return(contacts("sao@mai.test"), nil).Maybe()
	d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Maybe()

	return uc, d
}

func draftOrder() *domain.PurchaseOrder {
	return &domain.PurchaseOrder{
		ID: orderID, GarageID: garageID, Number: "DH-2026-00001", Status: domain.PurchaseOrderDraft, SupplierID: supplierA,
		OrderDate: "2026-10-15", Lines: []domain.PurchaseOrderLine{
			{ID: "ol-1", PartID: partID, EnteredQuantity: "10", Quantity: "10", UnitCost: 1000},
		},
	}
}

func createRequest(lines ...usecase.PurchaseOrderLineInput) usecase.CreatePurchaseOrderRequest {
	return usecase.CreatePurchaseOrderRequest{
		GarageID: garageID, ActorID: actorID, CanSetCost: true, SupplierID: supplierA, ExpectedDate: "2026-10-20", Notes: "morning",
		Lines: lines,
	}
}

func expectResolve(d *deps, entered string, resolved usecase.ResolvedQuantity) {
	d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, mock.Anything, entered).Return(resolved, nil).Once()
}

//nolint:funlen // one table of order cases
func TestInventoryUseCase_CreatePurchaseOrder(t *testing.T) {
	t.Parallel()

	line := usecase.PurchaseOrderLineInput{PartID: partID, EnteredQuantity: "10", UnitCost: 1000}

	t.Run("saves a numbered draft with the supplier and the cost", func(t *testing.T) {
		t.Parallel()

		uc, d := orderUseCase(t)
		expectResolve(d, "10", usecase.ResolvedQuantity{Base: "10"})
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "purchase_order", "DH-2026-{SEQ:5}").Return(1, nil).Once()
		d.Repo.EXPECT().CreatePurchaseOrder(mock.Anything, mock.MatchedBy(func(o *domain.PurchaseOrder) bool {
			return o.Number == "DH-2026-00001" && o.Status == domain.PurchaseOrderDraft && o.OrderDate == "2026-10-15" &&
				o.ExpectedDate == "2026-10-20" && o.SupplierID == supplierA && len(o.Lines) == 1 && o.Lines[0].UnitCost == 1000
		})).Return(nil).Once()

		got, err := uc.CreatePurchaseOrder(t.Context(), createRequest(line))
		require.NoError(t, err)
		assert.Equal(t, int64(10_000), got.TotalAmount)
		assert.Equal(t, "Supplier 1", got.SupplierName)
		assert.Equal(t, "sao@mai.test", got.SupplierEmail)
		assert.Equal(t, "BEER-330", got.Lines[0].PartSKU)
	})

	t.Run("keeps the packaging unit and ignores the cost of a caller who may not set it", func(t *testing.T) {
		t.Parallel()

		uc, d := orderUseCase(t)
		entered := "3"
		expectResolve(d, "3", usecase.ResolvedQuantity{Base: "12", UnitID: new(unitID), Entered: &entered})
		d.Parts.EXPECT().UnitAbbreviations(mock.Anything, garageID, []string{unitID}).Return(map[string]string{unitID: "ctn"}, nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "purchase_order", mock.Anything).Return(2, nil).Once()
		d.Repo.EXPECT().CreatePurchaseOrder(mock.Anything, mock.MatchedBy(func(o *domain.PurchaseOrder) bool {
			return o.Lines[0].Quantity == "12" && o.Lines[0].EnteredQuantity == "3" && o.Lines[0].UnitCost == 0
		})).Return(nil).Once()

		req := createRequest(usecase.PurchaseOrderLineInput{PartID: partID, UnitID: unitID, EnteredQuantity: "3", UnitCost: 999})
		req.CanSetCost = false

		got, err := uc.CreatePurchaseOrder(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "ctn", got.Lines[0].UnitAbbreviation)
	})

	refused := map[string]struct {
		req    func() usecase.CreatePurchaseOrderRequest
		code   apperror.Code
		reason string
		ahead  func(d *deps)
	}{
		"no lines": {func() usecase.CreatePurchaseOrderRequest { return createRequest() }, apperror.CodeInvalidArgument, "", nil},
		"notes too long": {func() usecase.CreatePurchaseOrderRequest {
			r := createRequest(line)
			r.Notes = strings.Repeat("n", domain.MaxNotesLength+1)

			return r
		}, apperror.CodeInvalidArgument, "", nil},
		"expected date not a date": {func() usecase.CreatePurchaseOrderRequest {
			r := createRequest(line)
			r.ExpectedDate = "soon"

			return r
		}, apperror.CodeInvalidArgument, "", nil},
		"expected date in the past": {func() usecase.CreatePurchaseOrderRequest {
			r := createRequest(line)
			r.ExpectedDate = "2026-10-14"

			return r
		}, apperror.CodeInvalidArgument, "", nil},
		"part twice": {func() usecase.CreatePurchaseOrderRequest { return createRequest(line, line) }, apperror.CodeInvalidArgument, "", func(d *deps) {
			expectResolve(d, "10", usecase.ResolvedQuantity{Base: "10"})
		}},
		"zero quantity": {func() usecase.CreatePurchaseOrderRequest {
			return createRequest(usecase.PurchaseOrderLineInput{PartID: partID, EnteredQuantity: "0"})
		}, apperror.CodeInvalidArgument, "", nil},
		"quantity not a number": {func() usecase.CreatePurchaseOrderRequest {
			return createRequest(usecase.PurchaseOrderLineInput{PartID: partID, EnteredQuantity: "many"})
		}, apperror.CodeInvalidArgument, "", nil},
		"negative cost": {func() usecase.CreatePurchaseOrderRequest {
			return createRequest(usecase.PurchaseOrderLineInput{PartID: partID, EnteredQuantity: "10", UnitCost: -1})
		}, apperror.CodeInvalidArgument, "", func(d *deps) { expectResolve(d, "10", usecase.ResolvedQuantity{Base: "10"}) }},
	}

	for name, c := range refused {
		t.Run("refuses "+name, func(t *testing.T) {
			t.Parallel()

			uc, d := orderUseCase(t)
			if c.ahead != nil {
				c.ahead(d)
			}

			_, err := uc.CreatePurchaseOrder(t.Context(), c.req())
			assertAppError(t, err, c.code)
		})
	}

	t.Run("a supplier that is not the garage's is a 404 of its own", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
		d.Directory.EXPECT().SupplierContacts(mock.Anything, garageID, mock.Anything).Return(map[string]usecase.SupplierContact{}, nil).Once()

		_, err := uc.CreatePurchaseOrder(t.Context(), createRequest(line))
		assertAppError(t, err, apperror.CodeNotFound)

		appErr, _ := apperror.As(err)
		assert.Equal(t, usecase.ReasonSupplierNotFound, appErr.Reason)
	})

	t.Run("an inactive part is refused", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		inactive := samplePart()
		inactive.IsActive = false

		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
		d.Directory.EXPECT().SupplierContacts(mock.Anything, garageID, mock.Anything).Return(contacts(""), nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{inactive}, nil).Once()

		_, err := uc.CreatePurchaseOrder(t.Context(), createRequest(line))
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("a part that does not exist is refused", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
		d.Directory.EXPECT().SupplierContacts(mock.Anything, garageID, mock.Anything).Return(contacts(""), nil).Once()

		_, err := uc.CreatePurchaseOrder(t.Context(), createRequest(usecase.PurchaseOrderLineInput{PartID: "not-an-id", EnteredQuantity: "1"}))
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("failures of the collaborators are internal errors", func(t *testing.T) {
		t.Parallel()

		steps := map[string]func(d *deps){
			"garage": func(d *deps) {
				d.Garages.ExpectedCalls = nil
				d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageSettings{}, errBoom).Once()
			},
			"supplier lookup": func(d *deps) {
				d.Directory.ExpectedCalls = nil
				d.Directory.EXPECT().SupplierContacts(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
			},
			"quantity": func(d *deps) {
				d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, mock.Anything, "10").Return(usecase.ResolvedQuantity{}, errBoom).Once()
			},
			"number": func(d *deps) {
				expectResolve(d, "10", usecase.ResolvedQuantity{Base: "10"})
				d.Sequences.EXPECT().Next(mock.Anything, garageID, "purchase_order", mock.Anything).Return(0, errBoom).Once()
			},
			"save": func(d *deps) {
				expectResolve(d, "10", usecase.ResolvedQuantity{Base: "10"})
				d.Sequences.EXPECT().Next(mock.Anything, garageID, "purchase_order", mock.Anything).Return(1, nil).Once()
				d.Repo.EXPECT().CreatePurchaseOrder(mock.Anything, mock.Anything).Return(errBoom).Once()
			},
			"audit": func(d *deps) {
				expectResolve(d, "10", usecase.ResolvedQuantity{Base: "10"})
				d.Sequences.EXPECT().Next(mock.Anything, garageID, "purchase_order", mock.Anything).Return(1, nil).Once()
				d.Repo.EXPECT().CreatePurchaseOrder(mock.Anything, mock.Anything).Return(nil).Once()
				d.Audit.ExpectedCalls = nil
				d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(errBoom).Once()
			},
			"description": func(d *deps) {
				expectResolve(d, "10", usecase.ResolvedQuantity{Base: "10"})
				d.Sequences.EXPECT().Next(mock.Anything, garageID, "purchase_order", mock.Anything).Return(1, nil).Once()
				d.Repo.EXPECT().CreatePurchaseOrder(mock.Anything, mock.Anything).Return(nil).Once()
				d.Directory.ExpectedCalls = nil
				d.Directory.EXPECT().SupplierContacts(mock.Anything, garageID, mock.Anything).Return(contacts(""), nil).Once()
				d.Directory.EXPECT().SupplierContacts(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
			},
		}

		for name, arrange := range steps {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				uc, d := orderUseCase(t)
				arrange(d)

				_, err := uc.CreatePurchaseOrder(t.Context(), createRequest(line))
				require.Error(t, err)
			})
		}
	})
}

func TestInventoryUseCase_UpdatePurchaseOrder(t *testing.T) {
	t.Parallel()

	req := func() usecase.UpdatePurchaseOrderRequest {
		return usecase.UpdatePurchaseOrderRequest{
			GarageID: garageID, OrderID: orderID, ActorID: actorID, CanSetCost: true, SupplierID: supplierA, ExpectedDate: "2026-10-18",
			Notes: "changed", Lines: []usecase.PurchaseOrderLineInput{{ID: "ol-1", PartID: partID, EnteredQuantity: "12", UnitCost: 2000}},
		}
	}

	t.Run("replaces a draft", func(t *testing.T) {
		t.Parallel()

		uc, d := orderUseCase(t)
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()
		expectResolve(d, "12", usecase.ResolvedQuantity{Base: "12"})
		d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.MatchedBy(func(o *domain.PurchaseOrder) bool {
			return o.ExpectedDate == "2026-10-18" && o.Notes == "changed" && o.Lines[0].Quantity == "12" && o.Lines[0].UnitCost == 2000
		})).Return(nil).Once()

		got, err := uc.UpdatePurchaseOrder(t.Context(), req())
		require.NoError(t, err)
		assert.Equal(t, int64(24_000), got.TotalAmount)
	})

	for name, c := range map[string]struct {
		unit     string
		resolved usecase.ResolvedQuantity
		cost     int64
	}{
		"keeps its stored cost while part and unit stay": {"", usecase.ResolvedQuantity{Base: "12"}, 1000},
		"loses it when the unit changes":                 {unitID, usecase.ResolvedQuantity{Base: "12", UnitID: new(unitID), Entered: new("12")}, 0},
	} {
		t.Run("without the cost permission a line "+name, func(t *testing.T) {
			t.Parallel()

			uc, d := orderUseCase(t)
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()
			d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, c.unit, "12").Return(c.resolved, nil).Once()
			d.Parts.EXPECT().UnitAbbreviations(mock.Anything, garageID, mock.Anything).Return(nil, nil).Maybe()
			d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.MatchedBy(func(o *domain.PurchaseOrder) bool {
				return o.Lines[0].UnitCost == c.cost
			})).Return(nil).Once()

			in := req()
			in.CanSetCost = false
			in.Lines = []usecase.PurchaseOrderLineInput{{ID: "ol-1", PartID: partID, UnitID: c.unit, EnteredQuantity: "12", UnitCost: 777}}

			_, err := uc.UpdatePurchaseOrder(t.Context(), in)
			require.NoError(t, err)
		})
	}

	t.Run("refuses an order that is no longer a draft", func(t *testing.T) {
		t.Parallel()

		uc, d := orderUseCase(t)
		sent := draftOrder()
		sent.Status = domain.PurchaseOrderSent
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(sent, nil).Once()

		_, err := uc.UpdatePurchaseOrder(t.Context(), req())
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	failures := map[string]func(d *deps){
		"unknown order": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(nil, domain.ErrPurchaseOrderNotFound).Once()
		},
		"garage": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()
			d.Garages.ExpectedCalls = nil
			d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageSettings{}, errBoom).Once()
		},
		"lines": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()
			d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, mock.Anything, "12").Return(usecase.ResolvedQuantity{}, errBoom).Once()
		},
		"save": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()
			expectResolve(d, "12", usecase.ResolvedQuantity{Base: "12"})
			d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.Anything).Return(errBoom).Once()
		},
		"audit": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()
			expectResolve(d, "12", usecase.ResolvedQuantity{Base: "12"})
			d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.Anything).Return(nil).Once()
			d.Audit.ExpectedCalls = nil
			d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(errBoom).Once()
		},
		"description": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()
			expectResolve(d, "12", usecase.ResolvedQuantity{Base: "12"})
			d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.Anything).Return(nil).Once()
			d.Directory.ExpectedCalls = nil
			d.Directory.EXPECT().SupplierContacts(mock.Anything, garageID, mock.Anything).Return(contacts(""), nil).Once()
			d.Directory.EXPECT().SupplierContacts(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
		},
	}

	for name, arrange := range failures {
		t.Run("fails on "+name, func(t *testing.T) {
			t.Parallel()

			uc, d := orderUseCase(t)
			arrange(d)

			_, err := uc.UpdatePurchaseOrder(t.Context(), req())
			require.Error(t, err)
		})
	}
}

func TestInventoryUseCase_GetAndListPurchaseOrders(t *testing.T) {
	t.Parallel()

	t.Run("get describes the order", func(t *testing.T) {
		t.Parallel()

		uc, d := orderUseCase(t)
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, false).Return(draftOrder(), nil).Once()

		got, err := uc.GetPurchaseOrder(t.Context(), garageID, orderID)
		require.NoError(t, err)
		assert.Equal(t, "Supplier 1", got.SupplierName)
		assert.Equal(t, int64(10_000), got.TotalAmount)
	})

	t.Run("get fails for an unknown order or a broken lookup", func(t *testing.T) {
		t.Parallel()

		uc, d := orderUseCase(t)
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, false).Return(nil, domain.ErrPurchaseOrderNotFound).Once()

		_, err := uc.GetPurchaseOrder(t.Context(), garageID, orderID)
		assertAppError(t, err, apperror.CodeNotFound)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, false).Return(draftOrder(), nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		_, err = uc.GetPurchaseOrder(t.Context(), garageID, orderID)
		require.Error(t, err)
	})

	t.Run("list pages the orders", func(t *testing.T) {
		t.Parallel()

		uc, d := orderUseCase(t)
		header := draftOrder()
		header.Lines = nil
		d.Repo.EXPECT().ListPurchaseOrders(mock.Anything, garageID, usecase.PurchaseOrderFilter{
			Query: "dh", SupplierID: supplierA, Status: domain.PurchaseOrderDraft, Limit: 20, Offset: 0,
		}).Return([]*domain.PurchaseOrder{header}, 1, nil).Once()

		page, err := uc.ListPurchaseOrders(t.Context(), usecase.ListPurchaseOrdersRequest{
			GarageID: garageID, Query: "dh", SupplierID: supplierA, Status: domain.PurchaseOrderDraft,
		})
		require.NoError(t, err)
		assert.Equal(t, 1, page.Total)
		assert.Equal(t, "Supplier 1", page.Items[0].SupplierName)
	})

	t.Run("list fails when the repository or a lookup does", func(t *testing.T) {
		t.Parallel()

		uc, d := orderUseCase(t)
		d.Repo.EXPECT().ListPurchaseOrders(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()

		_, err := uc.ListPurchaseOrders(t.Context(), usecase.ListPurchaseOrdersRequest{GarageID: garageID})
		require.Error(t, err)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().ListPurchaseOrders(mock.Anything, garageID, mock.Anything).Return([]*domain.PurchaseOrder{draftOrder()}, 1, nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Maybe()
		d.Directory.EXPECT().SupplierContacts(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		_, err = uc.ListPurchaseOrders(t.Context(), usecase.ListPurchaseOrdersRequest{GarageID: garageID})
		require.Error(t, err)
	})
}

func TestInventoryUseCase_CreatePurchaseOrdersFromLowStock(t *testing.T) {
	t.Parallel()

	second := samplePart()
	second.ID, second.SKU, second.ReorderThreshold = otherPartID, "FLT-1", "4"
	third := samplePart()
	third.ID, third.SKU = "22222222-2222-2222-2222-222222222224", "NOSUP"

	arrange := func(t *testing.T) (*usecase.InventoryUseCase, *deps) {
		t.Helper()

		uc, d := orderUseCase(t)
		d.Parts.EXPECT().SearchParts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart(), second, third}, 3, nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string(nil)).Return(map[string]usecase.Balance{
			partID:      {OnHand: "4", Reserved: "1"},
			otherPartID: {OnHand: "9"}, // not low: threshold 4
			third.ID:    {OnHand: "1"},
		}, nil).Once()

		return uc, d
	}

	t.Run("one draft per default supplier, the rest returned as unassigned", func(t *testing.T) {
		t.Parallel()

		uc, d := arrange(t)
		d.Repo.EXPECT().OnOrder(mock.Anything, garageID, mock.Anything).Return(map[string]string{partID: "2"}, nil).Once()
		d.Repo.EXPECT().DefaultSuppliers(mock.Anything, garageID, mock.Anything).Return(map[string]string{partID: supplierA}, nil).Once()
		// threshold 5: twice is 10, less 3 available and 2 on order is 5.
		expectResolve(d, "5", usecase.ResolvedQuantity{Base: "5"})
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "purchase_order", mock.Anything).Return(1, nil).Once()
		d.Repo.EXPECT().CreatePurchaseOrder(mock.Anything, mock.MatchedBy(func(o *domain.PurchaseOrder) bool {
			return o.SupplierID == supplierA && o.ExpectedDate == "2026-10-20" && len(o.Lines) == 1 && o.Lines[0].Quantity == "5"
		})).Return(nil).Once()

		res, err := uc.CreatePurchaseOrdersFromLowStock(t.Context(), usecase.CreatePurchaseOrdersFromLowStockRequest{
			GarageID: garageID, ActorID: actorID, ExpectedDate: "2026-10-20",
		})
		require.NoError(t, err)
		require.Len(t, res.Orders, 1)
		require.Len(t, res.Unassigned, 1)
		assert.Equal(t, "NOSUP", res.Unassigned[0].SKU)
	})

	t.Run("naming parts limits the order; what is covered is left out", func(t *testing.T) {
		t.Parallel()

		uc, d := arrange(t)
		d.Repo.EXPECT().OnOrder(mock.Anything, garageID, []string{partID}).Return(map[string]string{partID: "20"}, nil).Once()
		d.Repo.EXPECT().DefaultSuppliers(mock.Anything, garageID, []string{partID}).Return(map[string]string{partID: supplierA}, nil).Once()

		res, err := uc.CreatePurchaseOrdersFromLowStock(t.Context(), usecase.CreatePurchaseOrdersFromLowStockRequest{
			GarageID: garageID, ActorID: actorID, PartIDs: []string{partID},
		})
		require.NoError(t, err)
		assert.Empty(t, res.Orders, "what is on order already covers the part")
		assert.Empty(t, res.Unassigned)
	})

	failures := map[string]func(d *deps){
		"on order": func(d *deps) {
			d.Repo.EXPECT().OnOrder(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
		},
		"default suppliers": func(d *deps) {
			d.Repo.EXPECT().OnOrder(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()
			d.Repo.EXPECT().DefaultSuppliers(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
		},
		"creating an order": func(d *deps) {
			d.Repo.EXPECT().OnOrder(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()
			d.Repo.EXPECT().DefaultSuppliers(mock.Anything, garageID, mock.Anything).Return(map[string]string{partID: supplierA}, nil).Once()
			d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, mock.Anything, mock.Anything).Return(usecase.ResolvedQuantity{}, errBoom).Once()
		},
	}

	for name, arrangeFailure := range failures {
		t.Run("fails on "+name, func(t *testing.T) {
			t.Parallel()

			uc, d := arrange(t)
			arrangeFailure(d)

			_, err := uc.CreatePurchaseOrdersFromLowStock(t.Context(), usecase.CreatePurchaseOrdersFromLowStockRequest{GarageID: garageID})
			require.Error(t, err)
		})
	}

	t.Run("fails when the stock cannot be read", func(t *testing.T) {
		t.Parallel()

		uc, d := orderUseCase(t)
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string(nil)).Return(nil, errBoom).Once()

		_, err := uc.CreatePurchaseOrdersFromLowStock(t.Context(), usecase.CreatePurchaseOrdersFromLowStockRequest{GarageID: garageID})
		require.Error(t, err)
	})
}

var sendRequest = usecase.SendPurchaseOrderRequest{GarageID: garageID, OrderID: orderID, ActorID: actorID, Message: "please confirm"}

var sendMail = usecase.OrderMail{
	GarageID: garageID, OrderID: orderID, Number: "DH-2026-00001", SupplierName: "Supplier 1", To: "sao@mai.test", Message: "please confirm",
}

func mailerUseCase(t *testing.T) (*usecase.InventoryUseCase, *deps, *mocksMailer) {
	t.Helper()

	uc, d := orderUseCase(t)
	m := newMailer(t)
	uc.SetOrderMailer(m)

	return uc, d, m
}

func TestInventoryUseCase_SendPurchaseOrder(t *testing.T) {
	t.Parallel()

	t.Run("e-mails the supplier and marks a draft sent", func(t *testing.T) {
		t.Parallel()

		uc, d, m := mailerUseCase(t)
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()
		m.EXPECT().SendPurchaseOrder(mock.Anything, sendMail).Return(true, nil).Once()
		d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.MatchedBy(func(o *domain.PurchaseOrder) bool {
			return o.Status == domain.PurchaseOrderSent && o.SentTo == "sao@mai.test" && o.SentAt != nil && o.SentAt.Equal(now)
		})).Return(nil).Once()

		got, err := uc.SendPurchaseOrder(t.Context(), sendRequest)
		require.NoError(t, err)
		assert.Equal(t, domain.PurchaseOrderSent, got.Status)
	})

	t.Run("a sent order may be sent again", func(t *testing.T) {
		t.Parallel()

		uc, d, m := mailerUseCase(t)
		sent := draftOrder()
		sent.Status, sent.SentAt = domain.PurchaseOrderSent, new(now.Add(-time.Hour))
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(sent, nil).Once()
		m.EXPECT().SendPurchaseOrder(mock.Anything, sendMail).Return(true, nil).Once()
		d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.Anything).Return(nil).Once()

		got, err := uc.SendPurchaseOrder(t.Context(), sendRequest)
		require.NoError(t, err)
		assert.True(t, got.SentAt.Equal(now))
	})

	t.Run("an order in another state is not sendable", func(t *testing.T) {
		t.Parallel()

		for _, status := range []domain.PurchaseOrderStatus{domain.PurchaseOrderCancelled, domain.PurchaseOrderReceived, domain.PurchaseOrderPartiallyReceived} {
			uc, d, _ := mailerUseCase(t)
			o := draftOrder()
			o.Status = status
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(o, nil).Once()

			_, err := uc.SendPurchaseOrder(t.Context(), sendRequest)
			assertAppError(t, err, apperror.CodeFailedPrecondition)
		}
	})

	t.Run("a message over the limit, a supplier without an address, or an address that cannot be mailed are refused", func(t *testing.T) {
		t.Parallel()

		uc, d, _ := mailerUseCase(t)
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()

		long := sendRequest
		long.Message = strings.Repeat("m", domain.MaxOrderMessageLength+1)

		_, err := uc.SendPurchaseOrder(t.Context(), long)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		uc, d = newUseCase(t)
		uc.SetOrderMailer(newMailer(t))
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Maybe()
		d.Directory.EXPECT().SupplierContacts(mock.Anything, garageID, mock.Anything).Return(contacts("  "), nil).Once()

		_, err = uc.SendPurchaseOrder(t.Context(), sendRequest)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		appErr, _ := apperror.As(err)
		assert.Equal(t, usecase.ReasonSupplierEmailRequired, appErr.Reason)

		uc, d, m := mailerUseCase(t)
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()
		m.EXPECT().SendPurchaseOrder(mock.Anything, sendMail).Return(false, nil).Once()

		_, err = uc.SendPurchaseOrder(t.Context(), sendRequest)
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("without a mailer there is nothing to send with", func(t *testing.T) {
		t.Parallel()

		uc, d := orderUseCase(t)
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()

		_, err := uc.SendPurchaseOrder(t.Context(), sendRequest)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestInventoryUseCase_SendPurchaseOrder_Failures(t *testing.T) {
	t.Parallel()

	failures := map[string]func(d *deps, m *mocksMailer){
		"unknown order": func(d *deps, _ *mocksMailer) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(nil, domain.ErrPurchaseOrderNotFound).Once()
		},
		"lookup": func(d *deps, _ *mocksMailer) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()
			d.Directory.ExpectedCalls = nil
			d.Directory.EXPECT().SupplierContacts(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
		},
		"mailer": func(d *deps, m *mocksMailer) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()
			m.EXPECT().SendPurchaseOrder(mock.Anything, sendMail).Return(false, errBoom).Once()
		},
		"save": func(d *deps, m *mocksMailer) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()
			m.EXPECT().SendPurchaseOrder(mock.Anything, sendMail).Return(true, nil).Once()
			d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.Anything).Return(errBoom).Once()
		},
		"audit": func(d *deps, m *mocksMailer) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()
			m.EXPECT().SendPurchaseOrder(mock.Anything, sendMail).Return(true, nil).Once()
			d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.Anything).Return(nil).Once()
			d.Audit.ExpectedCalls = nil
			d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(errBoom).Once()
		},
	}

	for name, arrange := range failures {
		t.Run("fails on "+name, func(t *testing.T) {
			t.Parallel()

			uc, d, m := mailerUseCase(t)
			arrange(d, m)

			_, err := uc.SendPurchaseOrder(t.Context(), sendRequest)
			require.Error(t, err)
		})
	}
}

func TestInventoryUseCase_CancelPurchaseOrder(t *testing.T) {
	t.Parallel()

	with := func(status domain.PurchaseOrderStatus) *domain.PurchaseOrder {
		o := draftOrder()
		o.Status = status

		return o
	}

	for _, status := range []domain.PurchaseOrderStatus{domain.PurchaseOrderDraft, domain.PurchaseOrderSent, domain.PurchaseOrderPartiallyReceived} {
		t.Run("cancels a "+string(status)+" order", func(t *testing.T) {
			t.Parallel()

			uc, d := orderUseCase(t)
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(with(status), nil).Once()
			d.Repo.EXPECT().HasDraftReceipt(mock.Anything, garageID, orderID).Return(false, nil).Once()
			d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.MatchedBy(func(o *domain.PurchaseOrder) bool {
				return o.Status == domain.PurchaseOrderCancelled && o.CancelledAt != nil
			})).Return(nil).Once()

			got, err := uc.CancelPurchaseOrder(t.Context(), garageID, orderID, actorID)
			require.NoError(t, err)
			assert.Equal(t, domain.PurchaseOrderCancelled, got.Status)
		})
	}

	for _, status := range []domain.PurchaseOrderStatus{domain.PurchaseOrderReceived, domain.PurchaseOrderCancelled} {
		t.Run("refuses a "+string(status)+" order", func(t *testing.T) {
			t.Parallel()

			uc, d := orderUseCase(t)
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(with(status), nil).Once()

			_, err := uc.CancelPurchaseOrder(t.Context(), garageID, orderID, actorID)
			assertAppError(t, err, apperror.CodeFailedPrecondition)
		})
	}

	t.Run("refuses an order with a draft receipt open", func(t *testing.T) {
		t.Parallel()

		uc, d := orderUseCase(t)
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(with(domain.PurchaseOrderSent), nil).Once()
		d.Repo.EXPECT().HasDraftReceipt(mock.Anything, garageID, orderID).Return(true, nil).Once()

		_, err := uc.CancelPurchaseOrder(t.Context(), garageID, orderID, actorID)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	failures := map[string]func(d *deps){
		"unknown order": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(nil, domain.ErrPurchaseOrderNotFound).Once()
		},
		"receipt check": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(with(domain.PurchaseOrderSent), nil).Once()
			d.Repo.EXPECT().HasDraftReceipt(mock.Anything, garageID, orderID).Return(false, errBoom).Once()
		},
		"save": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(with(domain.PurchaseOrderSent), nil).Once()
			d.Repo.EXPECT().HasDraftReceipt(mock.Anything, garageID, orderID).Return(false, nil).Once()
			d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.Anything).Return(errBoom).Once()
		},
		"audit": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(with(domain.PurchaseOrderSent), nil).Once()
			d.Repo.EXPECT().HasDraftReceipt(mock.Anything, garageID, orderID).Return(false, nil).Once()
			d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.Anything).Return(nil).Once()
			d.Audit.ExpectedCalls = nil
			d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(errBoom).Once()
		},
		"description": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(with(domain.PurchaseOrderSent), nil).Once()
			d.Repo.EXPECT().HasDraftReceipt(mock.Anything, garageID, orderID).Return(false, nil).Once()
			d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.Anything).Return(nil).Once()
			d.Parts.ExpectedCalls = nil
			d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
		},
	}

	for name, arrange := range failures {
		t.Run("fails on "+name, func(t *testing.T) {
			t.Parallel()

			uc, d := orderUseCase(t)
			arrange(d)

			_, err := uc.CancelPurchaseOrder(t.Context(), garageID, orderID, actorID)
			require.Error(t, err)
		})
	}
}

func TestInventoryUseCase_ReceivePurchaseOrder(t *testing.T) {
	t.Parallel()

	sentOrder := func() *domain.PurchaseOrder {
		o := draftOrder()
		o.Status = domain.PurchaseOrderPartiallyReceived
		o.Lines[0].ReceivedQuantity = "4"

		return o
	}

	t.Run("pre-fills a draft receipt with what is outstanding", func(t *testing.T) {
		t.Parallel()

		uc, d := orderUseCase(t)
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(sentOrder(), nil).Once()
		d.Repo.EXPECT().HasDraftReceipt(mock.Anything, garageID, orderID).Return(false, nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "goods_receipt", mock.Anything).Return(7, nil).Once()
		d.Repo.EXPECT().DefaultSuppliers(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, "", "6").Return(usecase.ResolvedQuantity{Base: "6"}, nil).Once()
		d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(supplierNames(supplierA), nil).Maybe()
		d.Repo.EXPECT().CreateGoodsReceipt(mock.Anything, mock.MatchedBy(func(r *domain.GoodsReceipt) bool {
			return r.Status == domain.GoodsReceiptDraft && *r.PurchaseOrderID == orderID && *r.DefaultSupplierID == supplierA &&
				len(r.Lines) == 1 && r.Lines[0].Quantity == "6" && r.Lines[0].UnitCost == 1000 && *r.Lines[0].SupplierID == supplierA
		})).Return(nil).Once()

		got, err := uc.ReceivePurchaseOrder(t.Context(), garageID, orderID, actorID)
		require.NoError(t, err)
		assert.Equal(t, "DH-2026-00001", got.PurchaseOrderNumber)
		assert.Equal(t, int64(6000), got.TotalAmount)
	})

	refused := map[string]func(d *deps){
		"a draft order": func(d *deps) {
			o := draftOrder()
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(o, nil).Once()
		},
		"a draft receipt already open": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(sentOrder(), nil).Once()
			d.Repo.EXPECT().HasDraftReceipt(mock.Anything, garageID, orderID).Return(true, nil).Once()
		},
		"nothing outstanding": func(d *deps) {
			o := sentOrder()
			o.Lines[0].ReceivedQuantity = "10"
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(o, nil).Once()
			d.Repo.EXPECT().HasDraftReceipt(mock.Anything, garageID, orderID).Return(false, nil).Once()
		},
	}

	for name, arrange := range refused {
		t.Run("refuses "+name, func(t *testing.T) {
			t.Parallel()

			uc, d := orderUseCase(t)
			arrange(d)

			_, err := uc.ReceivePurchaseOrder(t.Context(), garageID, orderID, actorID)
			assertAppError(t, err, apperror.CodeFailedPrecondition)
		})
	}

	failures := map[string]func(d *deps){
		"unknown order": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(nil, domain.ErrPurchaseOrderNotFound).Once()
		},
		"receipt check": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(sentOrder(), nil).Once()
			d.Repo.EXPECT().HasDraftReceipt(mock.Anything, garageID, orderID).Return(false, errBoom).Once()
		},
		"garage": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(sentOrder(), nil).Once()
			d.Repo.EXPECT().HasDraftReceipt(mock.Anything, garageID, orderID).Return(false, nil).Once()
			d.Garages.ExpectedCalls = nil
			d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageSettings{}, errBoom).Once()
		},
		"number": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(sentOrder(), nil).Once()
			d.Repo.EXPECT().HasDraftReceipt(mock.Anything, garageID, orderID).Return(false, nil).Once()
			d.Sequences.EXPECT().Next(mock.Anything, garageID, "goods_receipt", mock.Anything).Return(0, errBoom).Once()
		},
		"lines": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(sentOrder(), nil).Once()
			d.Repo.EXPECT().HasDraftReceipt(mock.Anything, garageID, orderID).Return(false, nil).Once()
			d.Sequences.EXPECT().Next(mock.Anything, garageID, "goods_receipt", mock.Anything).Return(7, nil).Once()
			d.Repo.EXPECT().DefaultSuppliers(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
		},
		"save": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(sentOrder(), nil).Once()
			d.Repo.EXPECT().HasDraftReceipt(mock.Anything, garageID, orderID).Return(false, nil).Once()
			d.Sequences.EXPECT().Next(mock.Anything, garageID, "goods_receipt", mock.Anything).Return(7, nil).Once()
			d.Repo.EXPECT().DefaultSuppliers(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()
			d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, "", "6").Return(usecase.ResolvedQuantity{Base: "6"}, nil).Once()
			d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(supplierNames(supplierA), nil).Once()
			d.Repo.EXPECT().CreateGoodsReceipt(mock.Anything, mock.Anything).Return(errBoom).Once()
		},
		"description": func(d *deps) {
			d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(sentOrder(), nil).Once()
			d.Repo.EXPECT().HasDraftReceipt(mock.Anything, garageID, orderID).Return(false, nil).Once()
			d.Sequences.EXPECT().Next(mock.Anything, garageID, "goods_receipt", mock.Anything).Return(7, nil).Once()
			d.Repo.EXPECT().DefaultSuppliers(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()
			d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, "", "6").Return(usecase.ResolvedQuantity{Base: "6"}, nil).Once()
			d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(supplierNames(supplierA), nil).Once()
			d.Repo.EXPECT().CreateGoodsReceipt(mock.Anything, mock.Anything).Return(nil).Once()
			d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
		},
	}

	for name, arrange := range failures {
		t.Run("fails on "+name, func(t *testing.T) {
			t.Parallel()

			uc, d := orderUseCase(t)
			arrange(d)

			_, err := uc.ReceivePurchaseOrder(t.Context(), garageID, orderID, actorID)
			require.Error(t, err)
		})
	}
}

// linked is a receipt created from the order, with one line of 6.
func linked(status domain.GoodsReceiptStatus) *domain.GoodsReceipt {
	r := draftReceipt(receiptLine(partID, supplierA, "6", 1000))
	r.Status, r.PurchaseOrderID = status, new(orderID)

	return r
}

func TestInventoryUseCase_GoodsReceiptCountsAgainstItsOrder(t *testing.T) {
	t.Parallel()

	order := func(status domain.PurchaseOrderStatus, received string) *domain.PurchaseOrder {
		o := draftOrder()
		o.Status = status
		o.Lines[0].ReceivedQuantity = received

		return o
	}

	complete := func(t *testing.T, stored *domain.PurchaseOrder, want domain.PurchaseOrderStatus, wantReceived string) {
		t.Helper()

		uc, d := newUseCase(t)
		expectDescribe(d, supplierA)
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Maybe()
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(linked(domain.GoodsReceiptDraft), nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().RecordLastCost(mock.Anything, garageID, partID, supplierA, "1000").Return(nil).Once()
		d.Repo.EXPECT().SaveGoodsReceipt(mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(stored, nil).Once()
		d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.MatchedBy(func(o *domain.PurchaseOrder) bool {
			return o.Status == want && o.Lines[0].ReceivedQuantity == wantReceived
		})).Return(nil).Once()

		_, err := uc.CompleteGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		require.NoError(t, err)
	}

	t.Run("completing adds to the order and moves it along", func(t *testing.T) {
		t.Parallel()

		complete(t, order(domain.PurchaseOrderSent, "0"), domain.PurchaseOrderPartiallyReceived, "6")
	})

	t.Run("completing the rest makes the order received", func(t *testing.T) {
		t.Parallel()

		complete(t, order(domain.PurchaseOrderPartiallyReceived, "4"), domain.PurchaseOrderReceived, "10")
	})

	t.Run("a receipt line of a part that is not on the order counts for nothing", func(t *testing.T) {
		t.Parallel()

		stored := order(domain.PurchaseOrderSent, "0")
		stored.Lines[0].PartID = otherPartID

		complete(t, stored, domain.PurchaseOrderSent, "0")
	})

	reverse := func(t *testing.T, stored *domain.PurchaseOrder, want domain.PurchaseOrderStatus, wantReceived string) {
		t.Helper()

		uc, d := newUseCase(t)
		expectDescribe(d, supplierA)
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Maybe()
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(linked(domain.GoodsReceiptCompleted), nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(map[string]usecase.Balance{partID: {OnHand: "10"}}, nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().SaveGoodsReceipt(mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(stored, nil).Once()
		d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.MatchedBy(func(o *domain.PurchaseOrder) bool {
			return o.Status == want && o.Lines[0].ReceivedQuantity == wantReceived
		})).Return(nil).Once()

		_, err := uc.CancelGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		require.NoError(t, err)
	}

	t.Run("reversing takes the quantities back", func(t *testing.T) {
		t.Parallel()

		reverse(t, order(domain.PurchaseOrderReceived, "10"), domain.PurchaseOrderPartiallyReceived, "4")
	})

	t.Run("reversing never goes below zero and a cancelled order stays cancelled", func(t *testing.T) {
		t.Parallel()

		reverse(t, order(domain.PurchaseOrderCancelled, "2"), domain.PurchaseOrderCancelled, "0")
	})

	t.Run("a failure of the order is a failure of the receipt", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectDescribe(d, supplierA)
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(linked(domain.GoodsReceiptDraft), nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().RecordLastCost(mock.Anything, garageID, partID, supplierA, "1000").Return(nil).Once()
		d.Repo.EXPECT().SaveGoodsReceipt(mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(nil, errBoom).Once()

		_, err := uc.CompleteGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		require.Error(t, err)
	})

	t.Run("cancelling a draft receipt touches no order", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectDescribe(d, supplierA)
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Maybe()
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(linked(domain.GoodsReceiptDraft), nil).Once()
		d.Repo.EXPECT().SaveGoodsReceipt(mock.Anything, mock.Anything).Return(nil).Once()

		_, err := uc.CancelGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		require.NoError(t, err)
	})
}

func TestInventoryUseCase_UpdatePurchaseOrder_Rules(t *testing.T) {
	t.Parallel()

	update := func(t *testing.T, stored *domain.PurchaseOrder, in usecase.UpdatePurchaseOrderRequest, check func(o *domain.PurchaseOrder) bool) error {
		t.Helper()

		uc, d := orderUseCase(t)
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(stored, nil).Once()
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, mock.Anything, mock.Anything).Return(usecase.ResolvedQuantity{Base: "12"}, nil).Maybe()

		if check != nil {
			d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.MatchedBy(check)).Return(nil).Once()
		}

		_, err := uc.UpdatePurchaseOrder(t.Context(), in)

		return err
	}

	req := func(date string, lines ...usecase.PurchaseOrderLineInput) usecase.UpdatePurchaseOrderRequest {
		return usecase.UpdatePurchaseOrderRequest{
			GarageID: garageID, OrderID: orderID, ActorID: actorID, SupplierID: supplierA, ExpectedDate: date, Lines: lines,
		}
	}

	t.Run("a line id that is not the order's own is never used as a key", func(t *testing.T) {
		t.Parallel()

		in := req("", usecase.PurchaseOrderLineInput{ID: "another-orders-line", PartID: partID, EnteredQuantity: "12"})
		require.NoError(t, update(t, draftOrder(), in, func(o *domain.PurchaseOrder) bool {
			return o.Lines[0].ID != "another-orders-line" && o.Lines[0].ID != ""
		}))
	})

	t.Run("the order's own line keeps its id", func(t *testing.T) {
		t.Parallel()

		in := req("", usecase.PurchaseOrderLineInput{ID: "ol-1", PartID: partID, EnteredQuantity: "12"})
		require.NoError(t, update(t, draftOrder(), in, func(o *domain.PurchaseOrder) bool { return o.Lines[0].ID == "ol-1" }))
	})

	t.Run("without the cost permission lines sent without ids keep the cost of their part", func(t *testing.T) {
		t.Parallel()

		in := req("", usecase.PurchaseOrderLineInput{PartID: partID, EnteredQuantity: "12"})
		require.NoError(t, update(t, draftOrder(), in, func(o *domain.PurchaseOrder) bool { return o.Lines[0].UnitCost == 1000 }))
	})

	t.Run("an expected date kept as stored may have passed, a new one may not", func(t *testing.T) {
		t.Parallel()

		stored := func() *domain.PurchaseOrder {
			o := draftOrder()
			o.ExpectedDate = "2026-10-10"

			return o
		}
		line := usecase.PurchaseOrderLineInput{ID: "ol-1", PartID: partID, EnteredQuantity: "12"}

		require.NoError(t, update(t, stored(), req("2026-10-10", line), func(o *domain.PurchaseOrder) bool { return o.ExpectedDate == "2026-10-10" }))

		err := update(t, stored(), req("2026-10-11", line), nil)
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})
}

func TestInventoryUseCase_SendPurchaseOrder_AddressMustBeValid(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	uc.SetOrderMailer(newMailer(t))
	d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()
	d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Maybe()
	d.Directory.EXPECT().SupplierContacts(mock.Anything, garageID, mock.Anything).Return(contacts("not an address"), nil).Once()

	_, err := uc.SendPurchaseOrder(t.Context(), sendRequest)
	assertAppError(t, err, apperror.CodeInvalidArgument)
}

func TestInventoryUseCase_ReceivePurchaseOrder_ExactQuantities(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, unitCost int64) (*usecase.InventoryUseCase, *deps) {
		t.Helper()

		uc, d := orderUseCase(t)
		o := draftOrder()
		o.Status = domain.PurchaseOrderPartiallyReceived
		// 1 carton of 3 L, 2 L received: 1 L is not a whole number of thousandths of a carton.
		o.Lines[0] = domain.PurchaseOrderLine{
			ID: "ol-1", PartID: partID, UnitID: new(unitID), EnteredQuantity: "1", Quantity: "3", ReceivedQuantity: "2", UnitCost: unitCost,
		}
		d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(o, nil).Once()
		d.Repo.EXPECT().HasDraftReceipt(mock.Anything, garageID, orderID).Return(false, nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "goods_receipt", mock.Anything).Return(1, nil).Once()
		d.Repo.EXPECT().DefaultSuppliers(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()
		d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(supplierNames(supplierA), nil).Maybe()

		return uc, d
	}

	t.Run("a quantity the unit cannot show is received in the base unit, at the cost per base unit", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t, 30_000)
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, "", "1").Return(usecase.ResolvedQuantity{Base: "1"}, nil).Once()
		d.Repo.EXPECT().CreateGoodsReceipt(mock.Anything, mock.MatchedBy(func(r *domain.GoodsReceipt) bool {
			return r.Lines[0].UnitID == nil && r.Lines[0].Quantity == "1" && r.Lines[0].UnitCost == 10_000
		})).Return(nil).Once()

		_, err := uc.ReceivePurchaseOrder(t.Context(), garageID, orderID, actorID)
		require.NoError(t, err)
	})

	t.Run("a line whose unit has no cost to convert", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t, 0)
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, "", "1").Return(usecase.ResolvedQuantity{Base: "1"}, nil).Once()
		d.Repo.EXPECT().CreateGoodsReceipt(mock.Anything, mock.MatchedBy(func(r *domain.GoodsReceipt) bool { return r.Lines[0].UnitCost == 0 })).Return(nil).Once()

		_, err := uc.ReceivePurchaseOrder(t.Context(), garageID, orderID, actorID)
		require.NoError(t, err)
	})
}

func TestInventoryUseCase_UpdatePurchaseOrder_TwoLinesNamingOneId(t *testing.T) {
	t.Parallel()

	uc, d := orderUseCase(t)
	other := samplePart()
	other.ID = otherPartID

	d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()
	d.Parts.ExpectedCalls = nil
	d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Maybe()
	d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{otherPartID}).Return([]usecase.PartInfo{other}, nil).Maybe()
	d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart(), other}, nil).Maybe()
	d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, mock.Anything, mock.Anything, mock.Anything).Return(usecase.ResolvedQuantity{Base: "1"}, nil).Twice()
	d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.MatchedBy(func(o *domain.PurchaseOrder) bool {
		return len(o.Lines) == 2 && o.Lines[0].ID == "ol-1" && o.Lines[1].ID != "ol-1" && o.Lines[1].ID != ""
	})).Return(nil).Once()

	_, err := uc.UpdatePurchaseOrder(t.Context(), usecase.UpdatePurchaseOrderRequest{
		GarageID: garageID, OrderID: orderID, ActorID: actorID, SupplierID: supplierA,
		Lines: []usecase.PurchaseOrderLineInput{
			{ID: "ol-1", PartID: partID, EnteredQuantity: "1"}, {ID: "ol-1", PartID: otherPartID, EnteredQuantity: "1"},
		},
	})
	require.NoError(t, err)
}

func TestInventoryUseCase_SendPurchaseOrder_UsesTheBareAddress(t *testing.T) {
	t.Parallel()

	uc, d, m := mailerUseCase(t)
	d.Directory.ExpectedCalls = nil
	d.Directory.EXPECT().SupplierContacts(mock.Anything, garageID, mock.Anything).Return(contacts("Sao Mai <sales@mai.test>"), nil).Maybe()
	d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(draftOrder(), nil).Once()
	m.EXPECT().SendPurchaseOrder(mock.Anything, mock.MatchedBy(func(mail usecase.OrderMail) bool { return mail.To == "sales@mai.test" })).Return(true, nil).Once()
	d.Repo.EXPECT().SavePurchaseOrder(mock.Anything, mock.MatchedBy(func(o *domain.PurchaseOrder) bool { return o.SentTo == "sales@mai.test" })).Return(nil).Once()

	_, err := uc.SendPurchaseOrder(t.Context(), sendRequest)
	require.NoError(t, err)
}

func TestInventoryUseCase_CreatePurchaseOrdersFromLowStock_StaleDefaultSupplier(t *testing.T) {
	t.Parallel()

	second := samplePart()
	second.ID, second.SKU = otherPartID, "FLT-1"

	uc, d := orderUseCase(t)
	d.Parts.EXPECT().SearchParts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart(), second}, 2, nil).Once()
	d.Repo.EXPECT().Balances(mock.Anything, garageID, []string(nil)).Return(map[string]usecase.Balance{partID: {OnHand: "1"}, otherPartID: {OnHand: "1"}}, nil).Once()
	d.Repo.EXPECT().OnOrder(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()
	d.Repo.EXPECT().DefaultSuppliers(mock.Anything, garageID, mock.Anything).Return(map[string]string{partID: supplierA, otherPartID: supplierB}, nil).Once()
	d.Directory.ExpectedCalls = nil
	d.Directory.EXPECT().SupplierContacts(mock.Anything, garageID, []string{supplierA}).Return(contacts("a@b.test"), nil).Maybe()
	d.Directory.EXPECT().SupplierContacts(mock.Anything, garageID, []string{supplierB}).Return(map[string]usecase.SupplierContact{}, nil).Maybe()
	d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, mock.Anything, mock.Anything).Return(usecase.ResolvedQuantity{Base: "9"}, nil).Maybe()
	d.Sequences.EXPECT().Next(mock.Anything, garageID, "purchase_order", mock.Anything).Return(1, nil).Maybe()
	d.Repo.EXPECT().CreatePurchaseOrder(mock.Anything, mock.Anything).Return(nil).Once()

	res, err := uc.CreatePurchaseOrdersFromLowStock(t.Context(), usecase.CreatePurchaseOrdersFromLowStockRequest{GarageID: garageID, ActorID: actorID})
	require.NoError(t, err)
	assert.Len(t, res.Orders, 1, "the healthy supplier is still ordered")
	require.Len(t, res.Unassigned, 1, "the part whose supplier is gone is returned")
	assert.Equal(t, otherPartID, res.Unassigned[0].PartID)
}

func TestInventoryUseCase_ReceivePurchaseOrder_SupplierMustStillExist(t *testing.T) {
	t.Parallel()

	uc, d := orderUseCase(t)
	o := draftOrder()
	o.Status, o.Lines[0].ReceivedQuantity = domain.PurchaseOrderSent, "0"
	d.Repo.EXPECT().FindPurchaseOrder(mock.Anything, garageID, orderID, true).Return(o, nil).Once()
	d.Repo.EXPECT().HasDraftReceipt(mock.Anything, garageID, orderID).Return(false, nil).Once()
	d.Sequences.EXPECT().Next(mock.Anything, garageID, "goods_receipt", mock.Anything).Return(1, nil).Once()
	d.Repo.EXPECT().DefaultSuppliers(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()
	d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, "", "10").Return(usecase.ResolvedQuantity{Base: "10"}, nil).Once()
	d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(map[string]string{}, nil).Once()

	_, err := uc.ReceivePurchaseOrder(t.Context(), garageID, orderID, actorID)
	assertAppError(t, err, apperror.CodeNotFound)
}
