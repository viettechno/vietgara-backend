// Package usecase is the Technician BFF (Release 2.2, HLD Section 6): it
// composes the Tenant and Repair Order modules into the Technician App's
// screens, so the app makes one call per screen instead of one per garage.
// It owns no tables.
package usecase

import (
	"context"
	"slices"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Membership is a garage the technician works in.
type Membership struct {
	GarageID   string
	GarageName string
	// Active: the garage is active (FR-TEN-02).
	Active bool
	// CanReadRepairOrders: the technician's role grants repair_order.read.
	CanReadRepairOrders bool
}

// Memberships lists the garages an account owns or works in (Tenant).
type Memberships interface {
	ListForAccount(ctx context.Context, accountID string) ([]Membership, error)
}

// RepairOrders lists a garage's repair orders with a line assigned to an
// account (Repair Order); called inside that garage's scope.
type RepairOrders interface {
	ListAssigned(ctx context.Context, garageID, accountID string) ([]*domain.RepairOrder, error)
}

// Transactor switches the transaction's Row-Level Security scope for fn.
type Transactor interface {
	WithTransaction(ctx context.Context, scope platformdb.Scope, fn func(ctx context.Context) error) error
}

// Deps are the use case's collaborators.
type Deps struct {
	Memberships  Memberships
	RepairOrders RepairOrders
	Tx           Transactor
}

// TechnicianUseCase serves the Technician App.
type TechnicianUseCase struct {
	Deps
}

// NewTechnicianUseCase wires the use case.
func NewTechnicianUseCase(deps Deps) *TechnicianUseCase {
	return &TechnicianUseCase{Deps: deps}
}

// WorkOrder is an open repair order carrying only the lines assigned to the
// technician.
type WorkOrder struct {
	GarageID    string
	GarageName  string
	RepairOrder *domain.RepairOrder
}

// ListMyWork returns the open repair orders (not closed) with at least one
// line assigned to accountID, across every active garage where the account
// may read repair orders, oldest first (FR-TAPP-01). There is no schedule
// or priority field yet, so "today's work" is everything still open, in
// the order it arrived.
func (uc *TechnicianUseCase) ListMyWork(ctx context.Context, accountID string) ([]WorkOrder, error) {
	memberships, err := uc.Memberships.ListForAccount(ctx, accountID)
	if err != nil {
		return nil, apperror.Internal("list the technician's garages", err)
	}

	var work []WorkOrder

	for _, m := range memberships {
		if !m.Active || !m.CanReadRepairOrders {
			continue
		}

		// Each garage is read in its own Row-Level Security scope.
		scope := platformdb.Scope{GarageID: m.GarageID, AccountID: accountID}

		err := uc.Tx.WithTransaction(ctx, scope, func(ctx context.Context) error {
			orders, err := uc.RepairOrders.ListAssigned(ctx, m.GarageID, accountID)
			if err != nil {
				return err
			}

			for _, ro := range orders {
				if mine := assignedTo(ro, accountID); mine != nil {
					work = append(work, WorkOrder{GarageID: m.GarageID, GarageName: m.GarageName, RepairOrder: mine})
				}
			}

			return nil
		})
		if err != nil {
			return nil, apperror.Internal("list the technician's repair orders", err)
		}
	}

	slices.SortStableFunc(work, func(a, b WorkOrder) int {
		return a.RepairOrder.CreatedAt.Compare(b.RepairOrder.CreatedAt)
	})

	return work, nil
}

// assignedTo returns a copy of ro with only accountID's lines, or nil when
// the order is closed or none of its lines is assigned to them.
func assignedTo(ro *domain.RepairOrder, accountID string) *domain.RepairOrder {
	if ro.Status == domain.StatusClosed {
		return nil
	}

	mine := *ro
	mine.Lines = nil

	for _, line := range ro.Lines {
		if slices.Contains(line.AssignedTechnicianIDs, accountID) {
			mine.Lines = append(mine.Lines, line)
		}
	}

	if len(mine.Lines) == 0 {
		return nil
	}

	return &mine
}
