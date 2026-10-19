package handler

import (
	"context"

	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"

	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/internal/quote/domain"
	"github.com/viettechno/vietgara-backend/internal/quote/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// ListQuotes lists a garage's quotes, optionally filtered by customer,
// vehicle or status.
func (s *Server) ListQuotes(ctx context.Context, req *quotev1.ListQuotesRequest) (*quotev1.ListQuotesResponse, error) {
	return run(ctx, func(string) (*quotev1.ListQuotesResponse, error) {
		page, err := s.uc.List(ctx, usecase.ListQuotesRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()),
			GarageID:    req.GetGarageId(), CustomerID: req.GetCustomerId(), VehicleID: req.GetVehicleId(),
			Status: statusFromProto(req.GetStatus()),
		})
		if err != nil {
			return nil, err
		}

		resp := &quotev1.ListQuotesResponse{Data: make([]*quotev1.Quote, 0, len(page.Items)), Pagination: paginationToProto(page)}
		for _, q := range page.Items {
			resp.Data = append(resp.Data, quoteToProto(q))
		}

		return resp, nil
	})
}

// CreateQuote creates a draft quote (FR-QUO-01); answers 201.
func (s *Server) CreateQuote(ctx context.Context, req *quotev1.CreateQuoteRequest) (*quotev1.CreateQuoteResponse, error) {
	return run(ctx, func(actorID string) (*quotev1.CreateQuoteResponse, error) {
		in := req.GetQuote()

		var mode *domain.ApprovalMode

		if m := approvalModeFromProto(in.GetApprovalMode()); m != "" {
			mode = &m
		}

		q, err := s.uc.Create(ctx, usecase.CreateQuoteRequest{
			GarageID: req.GetGarageId(), ActorID: actorID, CustomerID: in.GetCustomerId(), VehicleID: in.GetVehicleId(),
			ApprovalMode: mode, Notes: in.GetNotes(), Lines: linesFromProto(in.GetLines()),
			BillingTaxProfileID: httpapi.BillingTaxProfileID(in.GetBillingParty()),
		})
		if err != nil {
			return nil, err
		}

		httpapi.Created(ctx)

		return &quotev1.CreateQuoteResponse{Quote: quoteToProto(q)}, nil
	})
}

// GetQuote returns one quote.
func (s *Server) GetQuote(ctx context.Context, req *quotev1.GetQuoteRequest) (*quotev1.GetQuoteResponse, error) {
	return run(ctx, func(string) (*quotev1.GetQuoteResponse, error) {
		q, err := s.uc.Get(ctx, req.GetGarageId(), req.GetQuoteId())
		if err != nil {
			return nil, err
		}

		return &quotev1.GetQuoteResponse{Quote: quoteToProto(q)}, nil
	})
}

// quoteSettable are the fields an update without a mask changes; the
// billing party is only changed when the mask names it, so an update of the
// lines never resets who the quote is billed to.
var quoteSettable = []string{"customer_id", "vehicle_id", "approval_mode", "notes", "lines"}

// UpdateQuote changes the fields named by the update mask; only while the
// quote is still a draft.
func (s *Server) UpdateQuote(ctx context.Context, req *quotev1.UpdateQuoteRequest) (*quotev1.UpdateQuoteResponse, error) {
	return run(ctx, func(actorID string) (*quotev1.UpdateQuoteResponse, error) {
		in := req.GetQuote()
		update := usecase.UpdateQuoteRequest{GarageID: req.GetGarageId(), QuoteID: req.GetQuoteId(), ActorID: actorID}

		for _, path := range maskPaths(req.GetUpdateMask().GetPaths(), quoteSettable...) {
			switch path {
			case "customer_id":
				customerID := in.GetCustomerId()
				update.CustomerID = &customerID
			case "vehicle_id":
				vehicleID := in.GetVehicleId()
				update.VehicleID = &vehicleID
			case "approval_mode":
				mode := approvalModeFromProto(in.GetApprovalMode())
				update.ApprovalMode = &mode
			case "notes":
				notes := in.GetNotes()
				update.Notes = &notes
			case "lines":
				lines := linesFromProto(in.GetLines())
				update.Lines = &lines
			case "billing_party":
				update.BillingTaxProfileID = new(in.GetBillingParty().GetTaxProfileId())
			default:
				return nil, badMask(path)
			}
		}

		q, err := s.uc.Update(ctx, update)
		if err != nil {
			return nil, err
		}

		return &quotev1.UpdateQuoteResponse{Quote: quoteToProto(q)}, nil
	})
}

// DeleteQuote soft-deletes a quote.
func (s *Server) DeleteQuote(ctx context.Context, req *quotev1.DeleteQuoteRequest) (*quotev1.DeleteQuoteResponse, error) {
	return run(ctx, func(actorID string) (*quotev1.DeleteQuoteResponse, error) {
		if err := s.uc.Delete(ctx, req.GetGarageId(), req.GetQuoteId(), actorID); err != nil {
			return nil, err
		}

		return &quotev1.DeleteQuoteResponse{}, nil
	})
}

// AddQuoteJob appends a job's labor and part lines to a draft quote
// (FR-CAT-06).
func (s *Server) AddQuoteJob(ctx context.Context, req *quotev1.AddQuoteJobRequest) (*quotev1.AddQuoteJobResponse, error) {
	return run(ctx, func(actorID string) (*quotev1.AddQuoteJobResponse, error) {
		q, err := s.uc.AddJob(ctx, usecase.AddJobRequest{
			GarageID: req.GetGarageId(), QuoteID: req.GetQuoteId(), JobID: req.GetJobId(), ActorID: actorID,
		})
		if err != nil {
			return nil, err
		}

		return &quotev1.AddQuoteJobResponse{Quote: quoteToProto(q)}, nil
	})
}

// maskPaths returns the update mask paths, or settable when it is empty.
func maskPaths(paths []string, settable ...string) []string {
	if len(paths) == 0 {
		return settable
	}

	return paths
}

func badMask(path string) error {
	return apperror.Validation("updateMask", apperror.IssueInvalid, "field "+path+" cannot be updated")
}
