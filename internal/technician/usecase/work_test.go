package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/internal/technician/usecase"
)

const me = "user-tech"

type memberships struct {
	list []usecase.Membership
	err  error
}

func (m memberships) ListForUser(_ context.Context, userID string) ([]usecase.Membership, error) {
	if userID != me {
		return nil, nil
	}

	return m.list, m.err
}

// repairOrders answers per garage.
type repairOrders struct {
	byGarage map[string][]*domain.RepairOrder
	err      error
}

func (r *repairOrders) ListAssigned(_ context.Context, garageID, userID string) ([]*domain.RepairOrder, error) {
	if userID != me {
		return nil, nil
	}

	return r.byGarage[garageID], r.err
}

// scopedTx records the scope of each transaction.
type scopedTx struct{ scopes *[]platformdb.Scope }

func (s scopedTx) WithTransaction(ctx context.Context, scope platformdb.Scope, fn func(context.Context) error) error {
	if s.scopes != nil {
		*s.scopes = append(*s.scopes, scope)
	}

	return fn(ctx)
}

var base = time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC)

func order(id string, status domain.RepairOrderStatus, minutes int, lines ...domain.Line) *domain.RepairOrder {
	return &domain.RepairOrder{ID: id, Number: "LSC-" + id, Status: status, CreatedAt: base.Add(time.Duration(minutes) * time.Minute), Lines: lines}
}

func line(id string, technicians ...string) domain.Line {
	return domain.Line{ID: id, Description: "line " + id, AssignedTechnicianIDs: technicians}
}

func TestListMyWork_KeepsOpenOrdersAndOnlyMyLines(t *testing.T) {
	var scopes []platformdb.Scope

	orders := &repairOrders{byGarage: map[string][]*domain.RepairOrder{
		"g-1": {
			order("late", domain.StatusInProgress, 30, line("l1", me), line("l2", "someone-else"), line("l3", me, "other")),
			order("closed", domain.StatusClosed, 0, line("l4", me)),
			order("not-mine", domain.StatusOpen, 0, line("l5", "someone-else")),
		},
		"g-2": {order("early", domain.StatusOpen, 10, line("l6", me))},
		"g-3": {order("no-access", domain.StatusOpen, 0, line("l7", me))},
		"g-4": {order("inactive", domain.StatusOpen, 0, line("l8", me))},
	}}
	uc := usecase.NewTechnicianUseCase(usecase.Deps{
		Memberships: memberships{list: []usecase.Membership{
			{GarageID: "g-1", GarageName: "Gara An Phat", Active: true, CanReadRepairOrders: true},
			{GarageID: "g-2", GarageName: "Gara Binh Minh", Active: true, CanReadRepairOrders: true},
			{GarageID: "g-3", GarageName: "No access", Active: true},
			{GarageID: "g-4", GarageName: "Closed garage", CanReadRepairOrders: true},
		}},
		RepairOrders: orders,
		Tx:           scopedTx{scopes: &scopes},
	})

	work, err := uc.ListMyWork(context.Background(), me)

	require.NoError(t, err)
	require.Len(t, work, 2)
	assert.Equal(t, "early", work[0].RepairOrder.ID, "oldest first")
	assert.Equal(t, "Gara Binh Minh", work[0].GarageName)
	assert.Equal(t, "late", work[1].RepairOrder.ID)
	assert.Equal(t, "g-1", work[1].GarageID)

	ids := []string{}
	for _, l := range work[1].RepairOrder.Lines {
		ids = append(ids, l.ID)
	}

	assert.Equal(t, []string{"l1", "l3"}, ids)

	// Each garage is read in its own scope, keeping the caller's user.
	assert.Equal(t, []platformdb.Scope{
		{GarageID: "g-1", UserID: me}, {GarageID: "g-2", UserID: me},
	}, scopes)
}

func TestListMyWork_EmptyWithoutAssignedLines(t *testing.T) {
	uc := usecase.NewTechnicianUseCase(usecase.Deps{
		Memberships:  memberships{list: []usecase.Membership{{GarageID: "g-1", Active: true, CanReadRepairOrders: true}}},
		RepairOrders: &repairOrders{byGarage: map[string][]*domain.RepairOrder{}},
		Tx:           scopedTx{},
	})

	work, err := uc.ListMyWork(context.Background(), me)

	require.NoError(t, err)
	assert.Empty(t, work)
}

func TestListMyWork_Errors(t *testing.T) {
	boom := errTechnicianWorkBoom

	_, err := usecase.NewTechnicianUseCase(usecase.Deps{
		Memberships: memberships{err: boom}, RepairOrders: &repairOrders{}, Tx: scopedTx{},
	}).ListMyWork(context.Background(), me)
	require.ErrorIs(t, err, boom)

	_, err = usecase.NewTechnicianUseCase(usecase.Deps{
		Memberships:  memberships{list: []usecase.Membership{{GarageID: "g-1", Active: true, CanReadRepairOrders: true}}},
		RepairOrders: &repairOrders{err: boom},
		Tx:           scopedTx{},
	}).ListMyWork(context.Background(), me)
	assert.ErrorIs(t, err, boom)
}
