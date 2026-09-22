package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
	"github.com/viettechno/vietgara-backend/pkg/ptr"
)

// registerStaffHTTPRoutes adds the browser-facing staff endpoints. Request
// and response bodies use the protojson form of the gRPC messages.
func (s *Server) registerStaffHTTPRoutes(router *echo.Group) {
	router.GET("/staff", s.listHTTPStaff)
	router.GET("/staff/lookup", s.lookupHTTPStaff)
	router.POST("/staff", s.addHTTPStaff)
}

type staffHTTPRequest struct {
	GarageID  string `json:"garageId"`
	AccountID string `json:"accountId"`
	Role      string `json:"role"`
}

type staffHTTPResponse struct {
	ID        string `json:"id"`
	GarageID  string `json:"garageId"`
	AccountID string `json:"accountId"`
	FullName  string `json:"fullName,omitempty"`
	Email     string `json:"email,omitempty"`
	Phone     string `json:"phone,omitempty"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type staffCandidateHTTPResponse struct {
	AccountID    string `json:"accountId"`
	FullName     string `json:"fullName"`
	Email        string `json:"email,omitempty"`
	Phone        string `json:"phone,omitempty"`
	AlreadyStaff bool   `json:"alreadyStaff"`
}

// listHTTPStaff lists a garage's staff with their account details. Any member
// of the garage may read it.
func (s *Server) listHTTPStaff(c echo.Context) error {
	garageID := c.QueryParam("garageId")
	if garageID == "" {
		return writeGarageHTTPError(c, status.Error(codes.InvalidArgument, "garageId is required"))
	}

	var members []usecase.StaffMember

	err := s.inGarageTx(c.Request().Context(), garageID, func(ctx context.Context) error {
		if _, err := s.authorizedGarage(ctx, garageID, false); err != nil {
			return err
		}

		var err error

		members, err = s.staff.ListMembers(ctx, garageID)

		return err
	})
	if err != nil {
		return writeGarageHTTPError(c, err)
	}

	response := make([]staffHTTPResponse, 0, len(members))
	for _, member := range members {
		response = append(response, toStaffHTTPResponse(member))
	}

	return c.JSON(http.StatusOK, map[string]any{"staff": response})
}

// lookupHTTPStaff finds the account with exactly the given email or phone so
// the owner can invite it. Owners only.
func (s *Server) lookupHTTPStaff(c echo.Context) error {
	garageID := c.QueryParam("garageId")
	if garageID == "" {
		return writeGarageHTTPError(c, status.Error(codes.InvalidArgument, "garageId is required"))
	}

	var candidate *usecase.StaffCandidate

	err := s.inGarageTx(c.Request().Context(), garageID, func(ctx context.Context) error {
		if _, err := s.authorizedGarage(ctx, garageID, true); err != nil {
			return err
		}

		var err error

		phoneOrEmail := c.QueryParam("query")
		candidate, err = s.staff.FindCandidate(ctx, garageID, phoneOrEmail)

		return err
	})
	if err != nil {
		return writeGarageHTTPError(c, err)
	}

	return c.JSON(http.StatusOK, staffCandidateHTTPResponse{
		AccountID:    candidate.Account.ID,
		FullName:     candidate.Account.FullName,
		Email:        ptr.Deref(candidate.Account.Email, ""),
		Phone:        ptr.Deref(candidate.Account.Phone, ""),
		AlreadyStaff: candidate.AlreadyStaff,
	})
}

// addHTTPStaff adds an existing account to the garage. Owners only, and never
// as another owner: a garage's owner is whoever created it.
func (s *Server) addHTTPStaff(c echo.Context) error {
	var payload staffHTTPRequest

	body, err := io.ReadAll(http.MaxBytesReader(c.Response(), c.Request().Body, maxGarageBodyBytes))
	if err != nil || json.Unmarshal(body, &payload) != nil {
		return writeGarageHTTPError(c, status.Error(codes.InvalidArgument, "invalid request body"))
	}

	role := domain.Role(payload.Role)
	if role == domain.RoleOwner {
		return writeGarageHTTPError(c, status.Error(codes.InvalidArgument, "owners cannot be invited"))
	}

	if strings.TrimSpace(payload.GarageID) == "" || strings.TrimSpace(payload.AccountID) == "" {
		return writeGarageHTTPError(c, status.Error(codes.InvalidArgument, "garageId and accountId are required"))
	}

	var member usecase.StaffMember

	err = s.inGarageTx(c.Request().Context(), payload.GarageID, func(ctx context.Context) error {
		if _, err := s.authorizedGarage(ctx, payload.GarageID, true); err != nil {
			return err
		}

		staff, err := s.staff.Add(ctx, usecase.AddStaffRequest{GarageID: payload.GarageID, AccountID: payload.AccountID, Role: role})
		if err != nil {
			return err
		}

		member = usecase.StaffMember{Staff: staff}

		members, err := s.staff.ListMembers(ctx, payload.GarageID)
		if err != nil {
			return err
		}

		for _, listed := range members {
			if listed.Staff.ID == staff.ID {
				member = listed
			}
		}

		return nil
	})
	if err != nil {
		return writeGarageHTTPError(c, err)
	}

	return c.JSON(http.StatusCreated, toStaffHTTPResponse(member))
}

func toStaffHTTPResponse(member usecase.StaffMember) staffHTTPResponse {
	response := staffHTTPResponse{
		ID: member.Staff.ID, GarageID: member.Staff.GarageID, AccountID: member.Staff.AccountID,
		Role: string(member.Staff.Role), Status: string(member.Staff.Status),
		CreatedAt: member.Staff.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt: member.Staff.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}

	if member.Account != nil {
		response.FullName = member.Account.FullName
		response.Email = ptr.Deref(member.Account.Email, "")
		response.Phone = ptr.Deref(member.Account.Phone, "")
	}

	return response
}
