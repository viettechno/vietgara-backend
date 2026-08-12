package handler_test

import (
	"errors"
	"testing"
	"time"

	tenantv1 "github.com/viettechno/vietgara-proto/gen/go/vietgara/tenant/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/handler"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// newTestServer wires the handler to real usecases backed by mockery
// mocks of the repository layer.
func newTestServer(t *testing.T) (*handler.Server, *mocks.MockGarageRepository, *mocks.MockGarageStaffRepository, *mocks.MockEventPublisher) {
	t.Helper()

	garages := mocks.NewMockGarageRepository(t)
	staff := mocks.NewMockGarageStaffRepository(t)
	events := mocks.NewMockEventPublisher(t)

	return handler.NewServer(
		usecase.NewGarageUseCase(garages, staff, events),
		usecase.NewStaffUseCase(staff),
	), garages, staff, events
}

func testGarage() *domain.Garage {
	taxCode, address, logoURL := "0123456789", "12 Nguyen Hue", "https://storage.example.com/logo.png"

	return &domain.Garage{
		ID:        "garage-1",
		AccountID: "acc-1",
		Name:      "Gara A",
		TaxCode:   &taxCode,
		Address:   &address,
		LogoURL:   &logoURL,
		Timezone:  "Asia/Ho_Chi_Minh",
		Currency:  "VND",
		Status:    domain.GarageStatusActive,
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
}

func testStaff() *domain.GarageStaff {
	return &domain.GarageStaff{
		ID:        "staff-1",
		GarageID:  "garage-1",
		AccountID: "acc-1",
		Role:      domain.RoleOwner,
		Status:    domain.StaffStatusActive,
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
}

func TestServer_CreateGarage(t *testing.T) {
	t.Run("missing account context is rejected", func(t *testing.T) {
		s, _, _, _ := newTestServer(t)

		_, err := s.CreateGarage(t.Context(), &tenantv1.CreateGarageRequest{Name: "Gara A"})
		require.Error(t, err)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})
}

func TestServer_GetGarage(t *testing.T) {
	tests := []struct {
		name     string
		garageID string
		setup    func(*mocks.MockGarageRepository)
		verify   func(*testing.T, *tenantv1.GetGarageResponse)
		wantCode codes.Code
	}{
		{
			name:     "returns the requested garage",
			garageID: "garage-1",
			setup: func(g *mocks.MockGarageRepository) {
				g.EXPECT().FindByID(mock.Anything, "garage-1").Return(testGarage(), nil).Once()
			},
			verify: func(t *testing.T, resp *tenantv1.GetGarageResponse) {
				t.Helper()

				gar := resp.GetGarage()
				require.NotNil(t, gar)
				assert.Equal(t, "garage-1", gar.GetId())
				assert.Equal(t, "acc-1", gar.GetAccountId())
				assert.Equal(t, "Gara A", gar.GetName())
				assert.Equal(t, "0123456789", gar.GetTaxCode())
				assert.Equal(t, "12 Nguyen Hue", gar.GetAddress())
				assert.Equal(t, "https://storage.example.com/logo.png", gar.GetLogoUrl())
				assert.Equal(t, "Asia/Ho_Chi_Minh", gar.GetTimezone())
				assert.Equal(t, "VND", gar.GetCurrency())
				assert.Equal(t, tenantv1.GarageStatus_GARAGE_STATUS_ACTIVE, gar.GetStatus())
				assert.Equal(t, timestamppb.New(testGarage().CreatedAt), gar.GetCreatedAt())
				assert.Equal(t, timestamppb.New(testGarage().UpdatedAt), gar.GetUpdatedAt())
			},
		},
		{
			name:     "keeps optional fields absent when the domain garage is minimal",
			garageID: "garage-1",
			setup: func(g *mocks.MockGarageRepository) {
				g.EXPECT().FindByID(mock.Anything, "garage-1").Return(
					&domain.Garage{ID: "garage-1", AccountID: "acc-1", Name: "Minimal"}, nil,
				).Once()
			},
			verify: func(t *testing.T, resp *tenantv1.GetGarageResponse) {
				t.Helper()

				gar := resp.GetGarage()
				require.NotNil(t, gar)
				assert.Equal(t, "Minimal", gar.GetName())
				assert.Nil(t, gar.TaxCode)
				assert.Nil(t, gar.Address)
				assert.Nil(t, gar.LogoUrl)
				assert.Equal(t, tenantv1.GarageStatus_GARAGE_STATUS_UNSPECIFIED, gar.GetStatus())
				assert.Nil(t, gar.GetCreatedAt())
				assert.Nil(t, gar.GetUpdatedAt())
			},
		},
		{
			name:     "unknown garage maps to not found",
			garageID: "garage-unknown",
			setup: func(g *mocks.MockGarageRepository) {
				g.EXPECT().FindByID(mock.Anything, "garage-unknown").Return(nil, errors.New("no rows")).Once()
			},
			wantCode: codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, garages, _, _ := newTestServer(t)
			tt.setup(garages)

			resp, err := s.GetGarage(t.Context(), &tenantv1.GetGarageRequest{GarageId: tt.garageID})
			if tt.wantCode != codes.OK {
				require.Error(t, err)
				assert.Equal(t, tt.wantCode, status.Code(err))

				return
			}

			require.NoError(t, err)
			tt.verify(t, resp)
		})
	}
}

func TestServer_GetGarage_StatusMapping(t *testing.T) {
	statuses := []struct {
		name string
		in   domain.GarageStatus
		want tenantv1.GarageStatus
	}{
		{name: "active", in: domain.GarageStatusActive, want: tenantv1.GarageStatus_GARAGE_STATUS_ACTIVE},
		{name: "inactive", in: domain.GarageStatusInactive, want: tenantv1.GarageStatus_GARAGE_STATUS_INACTIVE},
		{name: "suspended", in: domain.GarageStatusSuspended, want: tenantv1.GarageStatus_GARAGE_STATUS_SUSPENDED},
		{name: "unset", in: domain.GarageStatus(""), want: tenantv1.GarageStatus_GARAGE_STATUS_UNSPECIFIED},
		{name: "unknown", in: domain.GarageStatus("archived"), want: tenantv1.GarageStatus_GARAGE_STATUS_UNSPECIFIED},
	}

	for _, tt := range statuses {
		t.Run(tt.name, func(t *testing.T) {
			s, garages, _, _ := newTestServer(t)
			garages.EXPECT().FindByID(mock.Anything, "garage-1").Return(
				&domain.Garage{ID: "garage-1", Name: "Gara A", Status: tt.in}, nil,
			).Once()

			resp, err := s.GetGarage(t.Context(), &tenantv1.GetGarageRequest{GarageId: "garage-1"})
			require.NoError(t, err)
			require.NotNil(t, resp.GetGarage())
			assert.Equal(t, tt.want, resp.GetGarage().GetStatus())
		})
	}
}

func TestServer_UpdateGarage(t *testing.T) {
	name, currency := "Gara A Renamed", "USD"

	tests := []struct {
		name     string
		req      *tenantv1.UpdateGarageRequest
		setup    func(*mocks.MockGarageRepository)
		verify   func(*testing.T, *tenantv1.UpdateGarageResponse)
		wantCode codes.Code
	}{
		{
			name: "applies only the provided fields",
			req:  &tenantv1.UpdateGarageRequest{GarageId: "garage-1", Name: &name, Currency: &currency},
			setup: func(g *mocks.MockGarageRepository) {
				g.EXPECT().FindByID(mock.Anything, "garage-1").Return(testGarage(), nil).Once()
				g.EXPECT().Update(mock.Anything, mock.MatchedBy(func(gar *domain.Garage) bool {
					return gar.Name == name && gar.Currency == currency &&
						gar.Timezone == "Asia/Ho_Chi_Minh" && gar.Address != nil
				})).Return(nil).Once()
			},
			verify: func(t *testing.T, resp *tenantv1.UpdateGarageResponse) {
				t.Helper()

				gar := resp.GetGarage()
				require.NotNil(t, gar)
				assert.Equal(t, name, gar.GetName())
				assert.Equal(t, currency, gar.GetCurrency())
			},
		},
		{
			name: "unknown garage maps to not found",
			req:  &tenantv1.UpdateGarageRequest{GarageId: "garage-unknown"},
			setup: func(g *mocks.MockGarageRepository) {
				g.EXPECT().FindByID(mock.Anything, "garage-unknown").Return(nil, errors.New("no rows")).Once()
			},
			wantCode: codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, garages, _, _ := newTestServer(t)
			tt.setup(garages)

			resp, err := s.UpdateGarage(t.Context(), tt.req)
			if tt.wantCode != codes.OK {
				require.Error(t, err)
				assert.Equal(t, tt.wantCode, status.Code(err))

				return
			}

			require.NoError(t, err)
			tt.verify(t, resp)
		})
	}
}

func TestServer_ListGarages(t *testing.T) {
	t.Run("missing account context is rejected", func(t *testing.T) {
		s, _, _, _ := newTestServer(t)

		_, err := s.ListGarages(t.Context(), &tenantv1.ListGaragesRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})
}

func TestServer_AddStaff(t *testing.T) {
	tests := []struct {
		name     string
		req      *tenantv1.AddStaffRequest
		setup    func(*mocks.MockGarageStaffRepository)
		verify   func(*testing.T, *tenantv1.AddStaffResponse)
		wantCode codes.Code
	}{
		{
			name: "adds staff with the given role",
			req:  &tenantv1.AddStaffRequest{GarageId: "garage-1", AccountId: "acc-2", Role: tenantv1.Role_ROLE_ACCOUNTANT},
			setup: func(s *mocks.MockGarageStaffRepository) {
				s.EXPECT().FindByGarageAndAccount(mock.Anything, "garage-1", "acc-2").Return(nil, nil).Once()
				s.EXPECT().Create(mock.Anything, mock.MatchedBy(func(st *domain.GarageStaff) bool {
					return st.GarageID == "garage-1" && st.AccountID == "acc-2" &&
						st.Role == domain.RoleAccountant && st.Status == domain.StaffStatusActive
				})).Return(nil).Once()
			},
			verify: func(t *testing.T, resp *tenantv1.AddStaffResponse) {
				t.Helper()

				st := resp.GetStaff()
				require.NotNil(t, st)
				assert.Equal(t, "garage-1", st.GetGarageId())
				assert.Equal(t, "acc-2", st.GetAccountId())
				assert.Equal(t, tenantv1.Role_ROLE_ACCOUNTANT, st.GetRole())
				assert.Equal(t, tenantv1.StaffStatus_STAFF_STATUS_ACTIVE, st.GetStatus())
			},
		},
		{
			name:     "unspecified role is rejected",
			req:      &tenantv1.AddStaffRequest{GarageId: "garage-1", AccountId: "acc-2", Role: tenantv1.Role_ROLE_UNSPECIFIED},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "unknown role value is rejected",
			req:      &tenantv1.AddStaffRequest{GarageId: "garage-1", AccountId: "acc-2", Role: tenantv1.Role(99)},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "duplicate active staff maps to already exists",
			req:  &tenantv1.AddStaffRequest{GarageId: "garage-1", AccountId: "acc-2", Role: tenantv1.Role_ROLE_TECHNICIAN},
			setup: func(s *mocks.MockGarageStaffRepository) {
				s.EXPECT().FindByGarageAndAccount(mock.Anything, "garage-1", "acc-2").Return(testStaff(), nil).Once()
			},
			wantCode: codes.AlreadyExists,
		},
		{
			name: "persistence failure maps to internal",
			req:  &tenantv1.AddStaffRequest{GarageId: "garage-1", AccountId: "acc-2", Role: tenantv1.Role_ROLE_TECHNICIAN},
			setup: func(s *mocks.MockGarageStaffRepository) {
				s.EXPECT().FindByGarageAndAccount(mock.Anything, "garage-1", "acc-2").Return(nil, nil).Once()
				s.EXPECT().Create(mock.Anything, mock.Anything).Return(errors.New("connection lost")).Once()
			},
			wantCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, staff, _ := newTestServer(t)
			if tt.setup != nil {
				tt.setup(staff)
			}

			resp, err := s.AddStaff(t.Context(), tt.req)
			if tt.wantCode != codes.OK {
				require.Error(t, err)
				assert.Equal(t, tt.wantCode, status.Code(err))

				return
			}

			require.NoError(t, err)
			tt.verify(t, resp)
		})
	}
}

func TestServer_AddStaff_EveryRole(t *testing.T) {
	roles := []struct {
		name   string
		proto  tenantv1.Role
		domain domain.Role
	}{
		{name: "owner", proto: tenantv1.Role_ROLE_OWNER, domain: domain.RoleOwner},
		{name: "accountant", proto: tenantv1.Role_ROLE_ACCOUNTANT, domain: domain.RoleAccountant},
		{name: "technician", proto: tenantv1.Role_ROLE_TECHNICIAN, domain: domain.RoleTechnician},
		{name: "service advisor", proto: tenantv1.Role_ROLE_SERVICE_ADVISOR, domain: domain.RoleServiceAdvisor},
		{name: "inventory staff", proto: tenantv1.Role_ROLE_INVENTORY_STAFF, domain: domain.RoleInventoryStaff},
	}

	for _, tt := range roles {
		t.Run(tt.name, func(t *testing.T) {
			s, _, staff, _ := newTestServer(t)
			staff.EXPECT().FindByGarageAndAccount(mock.Anything, "garage-1", "acc-2").Return(nil, nil).Once()
			staff.EXPECT().Create(mock.Anything, mock.MatchedBy(func(st *domain.GarageStaff) bool {
				return st.Role == tt.domain && st.Status == domain.StaffStatusActive
			})).Return(nil).Once()

			resp, err := s.AddStaff(t.Context(), &tenantv1.AddStaffRequest{
				GarageId: "garage-1", AccountId: "acc-2", Role: tt.proto,
			})
			require.NoError(t, err)
			require.NotNil(t, resp.GetStaff())
			assert.Equal(t, tt.proto, resp.GetStaff().GetRole())
			assert.Equal(t, tenantv1.StaffStatus_STAFF_STATUS_ACTIVE, resp.GetStaff().GetStatus())
		})
	}
}

func TestServer_ListStaff(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*mocks.MockGarageStaffRepository)
		verify   func(*testing.T, *tenantv1.ListStaffResponse)
		wantCode codes.Code
	}{
		{
			name: "returns the staff of a garage",
			setup: func(s *mocks.MockGarageStaffRepository) {
				s.EXPECT().ListByGarageID(mock.Anything, "garage-1").Return([]*domain.GarageStaff{testStaff()}, nil).Once()
			},
			verify: func(t *testing.T, resp *tenantv1.ListStaffResponse) {
				t.Helper()

				require.Len(t, resp.GetStaff(), 1)
				st := resp.GetStaff()[0]
				assert.Equal(t, "staff-1", st.GetId())
				assert.Equal(t, "garage-1", st.GetGarageId())
				assert.Equal(t, "acc-1", st.GetAccountId())
				assert.Equal(t, tenantv1.Role_ROLE_OWNER, st.GetRole())
				assert.Equal(t, tenantv1.StaffStatus_STAFF_STATUS_ACTIVE, st.GetStatus())
				assert.Equal(t, timestamppb.New(testStaff().CreatedAt), st.GetCreatedAt())
				assert.Equal(t, timestamppb.New(testStaff().UpdatedAt), st.GetUpdatedAt())
			},
		},
		{
			name: "garage without staff gets an empty list",
			setup: func(s *mocks.MockGarageStaffRepository) {
				s.EXPECT().ListByGarageID(mock.Anything, "garage-1").Return([]*domain.GarageStaff{}, nil).Once()
			},
			verify: func(t *testing.T, resp *tenantv1.ListStaffResponse) {
				t.Helper()

				assert.Empty(t, resp.GetStaff())
			},
		},
		{
			name: "repository failure maps to internal",
			setup: func(s *mocks.MockGarageStaffRepository) {
				s.EXPECT().ListByGarageID(mock.Anything, "garage-1").Return(nil, errors.New("connection lost")).Once()
			},
			wantCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, staff, _ := newTestServer(t)
			tt.setup(staff)

			resp, err := s.ListStaff(t.Context(), &tenantv1.ListStaffRequest{GarageId: "garage-1"})
			if tt.wantCode != codes.OK {
				require.Error(t, err)
				assert.Equal(t, tt.wantCode, status.Code(err))

				return
			}

			require.NoError(t, err)
			tt.verify(t, resp)
		})
	}
}

func TestServer_ListStaff_RoleAndStatusMapping(t *testing.T) {
	cases := []struct {
		name      string
		role      domain.Role
		status    domain.StaffStatus
		wantRole  tenantv1.Role
		wantState tenantv1.StaffStatus
	}{
		{name: "owner", role: domain.RoleOwner, status: domain.StaffStatusActive, wantRole: tenantv1.Role_ROLE_OWNER, wantState: tenantv1.StaffStatus_STAFF_STATUS_ACTIVE},
		{name: "accountant", role: domain.RoleAccountant, status: domain.StaffStatusInactive, wantRole: tenantv1.Role_ROLE_ACCOUNTANT, wantState: tenantv1.StaffStatus_STAFF_STATUS_INACTIVE},
		{name: "technician", role: domain.RoleTechnician, status: domain.StaffStatusSuspended, wantRole: tenantv1.Role_ROLE_TECHNICIAN, wantState: tenantv1.StaffStatus_STAFF_STATUS_SUSPENDED},
		{name: "service advisor", role: domain.RoleServiceAdvisor, status: domain.StaffStatus("removed"), wantRole: tenantv1.Role_ROLE_SERVICE_ADVISOR, wantState: tenantv1.StaffStatus_STAFF_STATUS_UNSPECIFIED},
		{name: "inventory staff", role: domain.RoleInventoryStaff, status: domain.StaffStatus(""), wantRole: tenantv1.Role_ROLE_INVENTORY_STAFF, wantState: tenantv1.StaffStatus_STAFF_STATUS_UNSPECIFIED},
		{name: "unknown", role: domain.Role("admin"), status: domain.StaffStatusActive, wantRole: tenantv1.Role_ROLE_UNSPECIFIED, wantState: tenantv1.StaffStatus_STAFF_STATUS_ACTIVE},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s, _, staff, _ := newTestServer(t)
			staff.EXPECT().ListByGarageID(mock.Anything, "garage-1").Return(
				[]*domain.GarageStaff{{ID: "staff-1", GarageID: "garage-1", AccountID: "acc-2", Role: tt.role, Status: tt.status}}, nil,
			).Once()

			resp, err := s.ListStaff(t.Context(), &tenantv1.ListStaffRequest{GarageId: "garage-1"})
			require.NoError(t, err)
			require.Len(t, resp.GetStaff(), 1)
			st := resp.GetStaff()[0]
			assert.Equal(t, tt.wantRole, st.GetRole())
			assert.Equal(t, tt.wantState, st.GetStatus())
			assert.Nil(t, st.GetCreatedAt())
		})
	}
}

func TestServer_UnimplementedMethods(t *testing.T) {
	s, _, _, _ := newTestServer(t)

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "UpdateStaff",
			call: func() error {
				_, err := s.UpdateStaff(t.Context(), &tenantv1.UpdateStaffRequest{GarageId: "garage-1", StaffId: "staff-1"})
				return err
			},
		},
		{
			name: "RemoveStaff",
			call: func() error {
				_, err := s.RemoveStaff(t.Context(), &tenantv1.RemoveStaffRequest{GarageId: "garage-1", StaffId: "staff-1"})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.Error(t, err)
			assert.Equal(t, codes.Unimplemented, status.Code(err))
		})
	}
}
