package handler

import (
	"context"

	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// ListStaff lists the garage's members.
func (s *Server) ListStaff(ctx context.Context, req *tenantv1.ListStaffRequest) (*tenantv1.ListStaffResponse, error) {
	members, err := s.uc.ListStaff(ctx, req.GetGarageId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &tenantv1.ListStaffResponse{Data: make([]*tenantv1.Staff, 0, len(members))}
	for _, m := range members {
		resp.Data = append(resp.Data, staffToProto(m))
	}

	return resp, nil
}

// UpdateStaff changes a member's status or groups.
func (s *Server) UpdateStaff(ctx context.Context, req *tenantv1.UpdateStaffRequest) (*tenantv1.UpdateStaffResponse, error) {
	var fields usecase.StaffFields

	for _, path := range maskPaths(req.GetUpdateMask().GetPaths(), "status", "group_ids") {
		switch path {
		case "status":
			status := staffStatusFromProto(req.GetStaff().GetStatus())
			fields.Status = &status
		case "group_ids":
			groupIDs := req.GetStaff().GetGroupIds()
			fields.GroupIDs = &groupIDs
		default:
			return nil, apperror.ToGRPCError(apperror.Validation("updateMask", apperror.IssueInvalid, "field "+path+" cannot be updated"))
		}
	}

	member, err := s.uc.UpdateStaff(ctx, req.GetGarageId(), req.GetStaffId(), fields)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.UpdateStaffResponse{Staff: staffToProto(member)}, nil
}

// DeleteStaff removes a member.
func (s *Server) DeleteStaff(ctx context.Context, req *tenantv1.DeleteStaffRequest) (*tenantv1.DeleteStaffResponse, error) {
	if err := s.uc.DeleteStaff(ctx, req.GetGarageId(), req.GetStaffId()); err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.DeleteStaffResponse{}, nil
}

var candidateMemberships = map[string]tenantv1.CandidateMembership{
	usecase.CandidateNone:    tenantv1.CandidateMembership_CANDIDATE_MEMBERSHIP_NONE,
	usecase.CandidateMember:  tenantv1.CandidateMembership_CANDIDATE_MEMBERSHIP_MEMBER,
	usecase.CandidateInvited: tenantv1.CandidateMembership_CANDIDATE_MEMBERSHIP_INVITED,
}

// LookupStaffCandidate finds an account to invite by e-mail or phone.
func (s *Server) LookupStaffCandidate(ctx context.Context, req *tenantv1.LookupStaffCandidateRequest) (*tenantv1.LookupStaffCandidateResponse, error) {
	candidate, err := s.uc.LookupCandidate(ctx, req.GetGarageId(), req.GetQuery())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.LookupStaffCandidateResponse{Candidate: &tenantv1.StaffCandidate{
		AccountId:  candidate.Account.ID,
		FullName:   candidate.Account.FullName,
		Email:      candidate.Account.Email,
		Phone:      candidate.Account.Phone,
		Membership: candidateMemberships[candidate.Membership],
	}}, nil
}

// CreateInvitation invites an existing account; answers 201.
func (s *Server) CreateInvitation(ctx context.Context, req *tenantv1.CreateInvitationRequest) (*tenantv1.CreateInvitationResponse, error) {
	inv, err := s.uc.CreateInvitation(ctx, req.GetGarageId(), req.GetAccountId(), req.GetGroupIds())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	httpapi.Created(ctx)

	return &tenantv1.CreateInvitationResponse{Invitation: invitationToProto(inv)}, nil
}

// ListInvitations lists the garage's invitations.
func (s *Server) ListInvitations(ctx context.Context, req *tenantv1.ListInvitationsRequest) (*tenantv1.ListInvitationsResponse, error) {
	invitations, err := s.uc.ListInvitations(ctx, req.GetGarageId(), invitationStatusFromProto(req.GetStatus()))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &tenantv1.ListInvitationsResponse{Data: make([]*tenantv1.Invitation, 0, len(invitations))}
	for _, inv := range invitations {
		resp.Data = append(resp.Data, invitationToProto(inv))
	}

	return resp, nil
}

// RevokeInvitation cancels a pending invitation.
func (s *Server) RevokeInvitation(ctx context.Context, req *tenantv1.RevokeInvitationRequest) (*tenantv1.RevokeInvitationResponse, error) {
	inv, err := s.uc.RevokeInvitation(ctx, req.GetGarageId(), req.GetInvitationId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.RevokeInvitationResponse{Invitation: invitationToProto(inv)}, nil
}

// ListMyInvitations lists the caller's invitations.
func (s *Server) ListMyInvitations(ctx context.Context, req *tenantv1.ListMyInvitationsRequest) (*tenantv1.ListMyInvitationsResponse, error) {
	invitations, err := s.uc.ListMyInvitations(ctx, middleware.AccountID(ctx), invitationStatusFromProto(req.GetStatus()))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &tenantv1.ListMyInvitationsResponse{Data: make([]*tenantv1.Invitation, 0, len(invitations))}
	for _, inv := range invitations {
		resp.Data = append(resp.Data, invitationToProto(inv))
	}

	return resp, nil
}

// GetMyInvitation returns one of the caller's invitations.
func (s *Server) GetMyInvitation(ctx context.Context, req *tenantv1.GetMyInvitationRequest) (*tenantv1.GetMyInvitationResponse, error) {
	inv, err := s.uc.GetMyInvitation(ctx, middleware.AccountID(ctx), req.GetInvitationId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.GetMyInvitationResponse{Invitation: invitationToProto(inv)}, nil
}

// AcceptInvitation joins the garage.
func (s *Server) AcceptInvitation(ctx context.Context, req *tenantv1.AcceptInvitationRequest) (*tenantv1.AcceptInvitationResponse, error) {
	inv, err := s.uc.AcceptInvitation(ctx, middleware.AccountID(ctx), req.GetInvitationId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.AcceptInvitationResponse{Invitation: invitationToProto(inv)}, nil
}

// DeclineInvitation turns an invitation down.
func (s *Server) DeclineInvitation(ctx context.Context, req *tenantv1.DeclineInvitationRequest) (*tenantv1.DeclineInvitationResponse, error) {
	inv, err := s.uc.DeclineInvitation(ctx, middleware.AccountID(ctx), req.GetInvitationId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.DeclineInvitationResponse{Invitation: invitationToProto(inv)}, nil
}
