package main

import (
	"context"
	"errors"
	"time"

	billingRepository "github.com/viettechno/vietgara-backend/internal/billing/repository"
	catalogueDomain "github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	catalogueRepository "github.com/viettechno/vietgara-backend/internal/catalogue/repository"
	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	inventoryRepository "github.com/viettechno/vietgara-backend/internal/inventory/repository"
	repairorderDomain "github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	repairorderRepository "github.com/viettechno/vietgara-backend/internal/repairorder/repository"
	reportingHandler "github.com/viettechno/vietgara-backend/internal/reporting/handler"
	reportingUsecase "github.com/viettechno/vietgara-backend/internal/reporting/usecase"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

// reportingModule is the wired Reporting module (FRD 3.15, Release 1.5):
// revenue by period (FR-RPT-01) for the Web Admin Dashboard's KPI card and
// revenue chart (UI Design Spec 3.1). It owns no tables of its own --
// revenue is read fresh from Billing & Settlement's settlements on every
// request.
type reportingModule struct {
	server *reportingHandler.Server
	uc     *reportingUsecase.ReportingUseCase
}

func buildReporting(
	settlements *billingRepository.PostgresRepository, garages *tenantRepo.PostgresGarageRepository,
	orders *repairorderRepository.PostgresRepository, stock *inventoryRepository.PostgresRepository,
	users *identityRepo.PostgresUserRepository, jobs *catalogueRepository.PostgresRepository,
) *reportingModule {
	uc := reportingUsecase.NewReportingUseCase(reportingUsecase.Deps{
		Settlements: reportingSettlements{settlements: settlements},
		Garages:     reportingGarages{garages: garages},
		Work:        reportingOrders{orders: orders},
		Orders:      reportingOrders{orders: orders},
		Costs:       reportingCosts{stock: stock},
		Names:       reportingNames{users: users, jobs: jobs},
	})

	return &reportingModule{server: reportingHandler.NewServer(uc), uc: uc}
}

// reportingOrders lets the Reporting module read the completed work and the
// lines of repair orders without importing the Repair Order module.
type reportingOrders struct {
	orders *repairorderRepository.PostgresRepository
}

func (o reportingOrders) CompletedWork(ctx context.Context, garageID, timezone string, from, to time.Time) ([]reportingUsecase.TechnicianWork, error) {
	work, err := o.orders.CompletedWork(ctx, garageID, timezone, from, to)
	if err != nil {
		return nil, err
	}

	out := make([]reportingUsecase.TechnicianWork, len(work))
	for i, w := range work {
		out[i] = reportingUsecase.TechnicianWork(w)
	}

	return out, nil
}

func profitLines(lines []repairorderRepository.ProfitLine) []reportingUsecase.ProfitLine {
	out := make([]reportingUsecase.ProfitLine, len(lines))
	for i, l := range lines {
		out[i] = reportingUsecase.ProfitLine(l)
	}

	return out
}

func (o reportingOrders) ClosedLines(ctx context.Context, garageID, timezone string, from, to time.Time) ([]reportingUsecase.ProfitLine, error) {
	lines, err := o.orders.ClosedLines(ctx, garageID, timezone, from, to)

	return profitLines(lines), err
}

func (o reportingOrders) OrderLines(ctx context.Context, garageID, repairOrderID string) ([]reportingUsecase.ProfitLine, error) {
	lines, err := o.orders.OrderLines(ctx, garageID, repairOrderID)
	if errors.Is(err, repairorderDomain.ErrRepairOrderNotFound) {
		return nil, reportingUsecase.ErrOrderNotFound
	}

	return profitLines(lines), err
}

// reportingCosts lets the Reporting module read the cost snapshots of the
// stock issues without importing Inventory.
type reportingCosts struct {
	stock *inventoryRepository.PostgresRepository
}

func (c reportingCosts) IssueCosts(ctx context.Context, garageID string, repairOrderIDs []string) (map[string]reportingUsecase.IssueCost, error) {
	costs, err := c.stock.IssueCosts(ctx, garageID, repairOrderIDs)
	if err != nil {
		return nil, err
	}

	out := make(map[string]reportingUsecase.IssueCost, len(costs))
	for id, cost := range costs {
		out[id] = reportingUsecase.IssueCost(cost)
	}

	return out, nil
}

// reportingNames names the technicians and jobs of a report.
type reportingNames struct {
	users *identityRepo.PostgresUserRepository
	jobs  *catalogueRepository.PostgresRepository
}

func (n reportingNames) UserNames(ctx context.Context, ids []string) (map[string]string, error) {
	users, err := n.users.FindByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	names := make(map[string]string, len(users))
	for _, u := range users {
		names[u.ID] = u.FullName
	}

	return names, nil
}

func (n reportingNames) JobNames(ctx context.Context, garageID string, ids []string) (map[string]string, error) {
	names := make(map[string]string, len(ids))

	for _, id := range ids {
		job, err := n.jobs.FindJob(ctx, garageID, id)
		if errors.Is(err, catalogueDomain.ErrJobNotFound) {
			continue // a deleted job keeps its row, unnamed
		}

		if err != nil {
			return nil, err
		}

		names[id] = job.Name
	}

	return names, nil
}
