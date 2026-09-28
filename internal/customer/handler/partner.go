package handler

import (
	"context"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
)

// CreatePartner adds a supplier or insurer (FR-CUS-04); answers 201.
func (s *Server) CreatePartner(ctx context.Context, req *customerv1.CreatePartnerRequest) (*customerv1.CreatePartnerResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.CreatePartnerResponse, error) {
		in := req.GetPartner()

		p, err := s.partners.Create(ctx, usecase.CreatePartnerRequest{
			GarageID: req.GetGarageId(),
			ActorID:  actorID,
			Type:     partnerTypeFromProto(in.GetType()),
			Name:     in.GetName(),
			TaxCode:  in.TaxCode,
			Contact:  partnerContactFromProto(in.GetContact()),
			Note:     in.Note,
		})
		if err != nil {
			return nil, err
		}

		httpapi.Created(ctx)

		return &customerv1.CreatePartnerResponse{Partner: partnerToProto(p)}, nil
	})
}

// GetPartner returns one partner.
func (s *Server) GetPartner(ctx context.Context, req *customerv1.GetPartnerRequest) (*customerv1.GetPartnerResponse, error) {
	return run(ctx, func(string) (*customerv1.GetPartnerResponse, error) {
		p, err := s.partners.Get(ctx, req.GetGarageId(), req.GetPartnerId())
		if err != nil {
			return nil, err
		}

		return &customerv1.GetPartnerResponse{Partner: partnerToProto(p)}, nil
	})
}

// UpdatePartner changes the fields named by the update mask; "contact"
// replaces the whole contact block.
func (s *Server) UpdatePartner(ctx context.Context, req *customerv1.UpdatePartnerRequest) (*customerv1.UpdatePartnerResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.UpdatePartnerResponse, error) {
		in := req.GetPartner()
		update := usecase.UpdatePartnerRequest{GarageID: req.GetGarageId(), PartnerID: req.GetPartnerId(), ActorID: actorID}

		for _, path := range contactPaths(maskPaths(req.GetUpdateMask().GetPaths(), "type", "name", "tax_code", "contact", "note")) {
			switch path {
			case "type":
				partnerType := partnerTypeFromProto(in.GetType())
				update.Type = &partnerType
			case "name":
				name := in.GetName()
				update.Name = &name
			case "tax_code":
				update.TaxCode = stringField(in.TaxCode)
			case "contact":
				contact := partnerContactFromProto(in.GetContact())
				update.Contact = &contact
			case "note":
				update.Note = stringField(in.Note)
			default:
				return nil, badMask(path)
			}
		}

		p, err := s.partners.Update(ctx, update)
		if err != nil {
			return nil, err
		}

		return &customerv1.UpdatePartnerResponse{Partner: partnerToProto(p)}, nil
	})
}

// contactPaths folds "contact.*" sub-paths into "contact" (the contact is
// replaced as a whole).
func contactPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seenContact := false

	for _, p := range paths {
		if p == "contact" || len(p) > len("contact.") && p[:len("contact.")] == "contact." {
			if !seenContact {
				out = append(out, "contact")
				seenContact = true
			}

			continue
		}

		out = append(out, p)
	}

	return out
}

// ListPartners lists a garage's partners.
func (s *Server) ListPartners(ctx context.Context, req *customerv1.ListPartnersRequest) (*customerv1.ListPartnersResponse, error) {
	return run(ctx, func(string) (*customerv1.ListPartnersResponse, error) {
		page, err := s.partners.List(ctx, usecase.ListPartnersRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()),
			GarageID:    req.GetGarageId(),
			Type:        partnerTypeFromProto(req.GetType()),
			Query:       req.GetQuery(),
		})
		if err != nil {
			return nil, err
		}

		resp := &customerv1.ListPartnersResponse{Data: make([]*customerv1.Partner, 0, len(page.Items)), Pagination: paginationToProto(page)}
		for _, p := range page.Items {
			resp.Data = append(resp.Data, partnerToProto(p))
		}

		return resp, nil
	})
}

// DeletePartner soft-deletes a partner.
func (s *Server) DeletePartner(ctx context.Context, req *customerv1.DeletePartnerRequest) (*customerv1.DeletePartnerResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.DeletePartnerResponse, error) {
		if err := s.partners.Delete(ctx, req.GetGarageId(), req.GetPartnerId(), actorID); err != nil {
			return nil, err
		}

		return &customerv1.DeletePartnerResponse{}, nil
	})
}
