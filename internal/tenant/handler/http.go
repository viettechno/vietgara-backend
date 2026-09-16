package handler

import (
	"encoding/json"
	"net/http"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
)

type staffHTTPRequest struct {
	GarageID  string `json:"garageId"`
	AccountID string `json:"accountId"`
	Role      string `json:"role"`
}

type staffHTTPResponse struct {
	ID        string `json:"id"`
	GarageID  string `json:"garageId"`
	AccountID string `json:"accountId"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// ServeHTTP exposes the browser-facing staff endpoints. Authentication and
// garage authorization are applied by the HTTP middleware in cmd/server.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/staff" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.listHTTPStaff(w, r)
	case http.MethodPost:
		s.addHTTPStaff(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) listHTTPStaff(w http.ResponseWriter, r *http.Request) {
	garageID := r.URL.Query().Get("garageId")
	if garageID == "" {
		writeStaffHTTPError(w, http.StatusBadRequest, "garageId is required")
		return
	}

	if !httpHasGarageAccess(r, garageID) {
		writeStaffHTTPError(w, http.StatusForbidden, "no access to this garage")
		return
	}

	list, err := s.staff.List(r.Context(), garageID)
	if err != nil {
		writeStaffHTTPError(w, http.StatusInternalServerError, "unable to list staff")
		return
	}

	response := make([]staffHTTPResponse, 0, len(list))
	for _, member := range list {
		response = append(response, staffHTTPResponse{
			ID: member.ID, GarageID: member.GarageID, AccountID: member.AccountID,
			Role: string(member.Role), Status: string(member.Status),
			CreatedAt: member.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			UpdatedAt: member.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}

	writeStaffHTTPJSON(w, http.StatusOK, map[string]any{"staff": response})
}

func (s *Server) addHTTPStaff(w http.ResponseWriter, r *http.Request) {
	var payload staffHTTPRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&payload); err != nil {
		writeStaffHTTPError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !httpHasGarageAccess(r, payload.GarageID) {
		writeStaffHTTPError(w, http.StatusForbidden, "no access to this garage")
		return
	}

	role := domain.Role(payload.Role)

	member, err := s.staff.Add(r.Context(), usecase.AddStaffRequest{
		GarageID: payload.GarageID, AccountID: payload.AccountID, Role: role,
	})
	if err != nil {
		writeStaffHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeStaffHTTPJSON(w, http.StatusCreated, staffHTTPResponse{
		ID: member.ID, GarageID: member.GarageID, AccountID: member.AccountID,
		Role: string(member.Role), Status: string(member.Status),
		CreatedAt: member.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt: member.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

func httpHasGarageAccess(r *http.Request, garageID string) bool {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return false
	}

	_, hasAccess := claims.HasAccessToGarage(garageID)

	return hasAccess
}

func writeStaffHTTPJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}

func writeStaffHTTPError(w http.ResponseWriter, statusCode int, message string) {
	writeStaffHTTPJSON(w, statusCode, map[string]string{"message": message})
}
