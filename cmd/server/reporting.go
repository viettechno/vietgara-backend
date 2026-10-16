package main

import (
	billingRepository "github.com/viettechno/vietgara-backend/internal/billing/repository"
	reportingHandler "github.com/viettechno/vietgara-backend/internal/reporting/handler"
	reportingUsecase "github.com/viettechno/vietgara-backend/internal/reporting/usecase"
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

func buildReporting(settlements *billingRepository.PostgresRepository) *reportingModule {
	uc := reportingUsecase.NewReportingUseCase(reportingUsecase.Deps{
		Settlements: reportingSettlements{settlements: settlements},
	})

	return &reportingModule{server: reportingHandler.NewServer(uc), uc: uc}
}
