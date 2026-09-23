package handler

import (
	"context"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
)

// CreatePartner adds a supplier or insurer to the directory (FR-CUS-04).
func (s *Server) CreatePartner(ctx context.Context, req *customerv1.CreatePartnerRequest) (*customerv1.CreatePartnerResponse, error) {
	return run(ctx, s, req.GetGarageId(), permManagePartners, func(actorID string) (*customerv1.CreatePartnerResponse, error) {
		p, err := s.partners.Create(ctx, usecase.CreatePartnerRequest{
			GarageID: req.GetGarageId(),
			ActorID:  actorID,
			Type:     partnerTypeFromProto(req.GetType()),
			Name:     req.GetName(),
			TaxCode:  req.TaxCode,
			Contact:  partnerContactFromProto(req.GetContact()),
			Note:     req.Note,
		})
		if err != nil {
			return nil, err
		}

		return &customerv1.CreatePartnerResponse{Partner: partnerToProto(p)}, nil
	})
}

// GetPartner returns one partner.
func (s *Server) GetPartner(ctx context.Context, req *customerv1.GetPartnerRequest) (*customerv1.GetPartnerResponse, error) {
	return run(ctx, s, req.GetGarageId(), permRead, func(string) (*customerv1.GetPartnerResponse, error) {
		p, err := s.partners.Get(ctx, req.GetGarageId(), req.GetPartnerId())
		if err != nil {
			return nil, err
		}

		return &customerv1.GetPartnerResponse{Partner: partnerToProto(p)}, nil
	})
}

// UpdatePartner applies partial updates to a partner.
func (s *Server) UpdatePartner(ctx context.Context, req *customerv1.UpdatePartnerRequest) (*customerv1.UpdatePartnerResponse, error) {
	return run(ctx, s, req.GetGarageId(), permManagePartners, func(actorID string) (*customerv1.UpdatePartnerResponse, error) {
		update := usecase.UpdatePartnerRequest{
			GarageID:  req.GetGarageId(),
			PartnerID: req.GetPartnerId(),
			ActorID:   actorID,
			Name:      req.Name,
			TaxCode:   req.TaxCode,
			Note:      req.Note,
		}

		if req.Type != nil {
			partnerType := partnerTypeFromProto(req.GetType())
			update.Type = &partnerType
		}

		if req.Contact != nil {
			contact := partnerContactFromProto(req.GetContact())
			update.Contact = &contact
		}

		p, err := s.partners.Update(ctx, update)
		if err != nil {
			return nil, err
		}

		return &customerv1.UpdatePartnerResponse{Partner: partnerToProto(p)}, nil
	})
}

// ListPartners searches the partner directory of a garage.
func (s *Server) ListPartners(ctx context.Context, req *customerv1.ListPartnersRequest) (*customerv1.ListPartnersResponse, error) {
	return run(ctx, s, req.GetGarageId(), permRead, func(string) (*customerv1.ListPartnersResponse, error) {
		page, err := s.partners.List(ctx, usecase.ListPartnersRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()),
			GarageID:    req.GetGarageId(),
			Type:        partnerTypeFromProto(req.GetType()),
			Query:       req.GetQuery(),
		})
		if err != nil {
			return nil, err
		}

		resp := &customerv1.ListPartnersResponse{Pagination: paginationToProto(page)}
		for _, p := range page.Items {
			resp.Partners = append(resp.Partners, partnerToProto(p))
		}

		return resp, nil
	})
}

// DeletePartner soft-deletes a partner.
func (s *Server) DeletePartner(ctx context.Context, req *customerv1.DeletePartnerRequest) (*customerv1.DeletePartnerResponse, error) {
	return run(ctx, s, req.GetGarageId(), permManagePartners, func(actorID string) (*customerv1.DeletePartnerResponse, error) {
		if err := s.partners.Delete(ctx, req.GetGarageId(), req.GetPartnerId(), actorID); err != nil {
			return nil, err
		}

		return &customerv1.DeletePartnerResponse{}, nil
	})
}
