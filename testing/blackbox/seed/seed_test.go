package seed_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"
	testdata "github.com/viettechno/vietgara-testdata/go"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/seed"
)

var (
	errFakeUserAlreadyExists = errors.New("user already exists")
	errInvalidOTP            = errors.New("invalid OTP")
	errUnsupportedFakePlanID = errors.New("unsupported fake plan id")
	errGarageCreationFailed  = errors.New("create failed")
)

type fakeState struct {
	users         map[string]*identityv1.User
	subscriptions map[string]*licensev1.Subscription
	garages       map[string][]*tenantv1.Garage
	tokens        map[string]string
	nextToken     int
	registers     int
	verifications int
	planChanges   int
	createError   error
}

type fakeAPI struct {
	state *fakeState
	email string
}

func (a *fakeAPI) Login(_ context.Context, email, _ string) (*identityv1.Session, error) {
	user := a.state.users[email]
	if user == nil {
		return nil, &client.APIError{Status: 401, Code: "INVALID_CREDENTIALS"}
	}

	return a.state.session(email, user), nil
}

func (a *fakeAPI) Register(_ context.Context, request *identityv1.RegisterRequest) (*identityv1.Session, error) {
	if a.state.users[request.GetEmail()] != nil {
		return nil, errFakeUserAlreadyExists
	}

	a.state.registers++
	user := &identityv1.User{Email: request.GetEmail(), FullName: request.GetFullName()}
	a.state.users[request.GetEmail()] = user

	return a.state.session(request.GetEmail(), user), nil
}

func (a *fakeAPI) VerifyEmail(_ context.Context, otp, _ string) (*identityv1.Session, error) {
	if otp != "123456" {
		return nil, errInvalidOTP
	}

	a.state.verifications++
	user := a.state.users[a.email]
	user.EmailVerified = true
	a.state.subscriptions[a.email] = &licensev1.Subscription{
		Plan: &licensev1.Plan{Code: "trial", Name: "Free trial"},
	}

	return a.state.session(a.email, user), nil
}

func (a *fakeAPI) MySubscription(context.Context) (*licensev1.Subscription, error) {
	return a.state.subscriptions[a.email], nil
}

func (a *fakeAPI) ListPlans(context.Context) ([]*licensev1.Plan, error) {
	return []*licensev1.Plan{{Id: "professional-id", Code: "professional", Name: "Pro"}}, nil
}

func (a *fakeAPI) ChangeMyPlan(_ context.Context, planID string) (*licensev1.Subscription, error) {
	if planID != "professional-id" {
		return nil, fmt.Errorf("%w %q", errUnsupportedFakePlanID, planID)
	}

	a.state.planChanges++
	subscription := &licensev1.Subscription{
		Plan: &licensev1.Plan{Code: "professional", Name: "Pro"},
	}
	a.state.subscriptions[a.email] = subscription

	return subscription, nil
}

func (a *fakeAPI) ListGarages(_ context.Context) ([]*tenantv1.Garage, error) {
	return a.state.garages[a.email], nil
}

func (a *fakeAPI) CreateGarage(_ context.Context, garage *tenantv1.Garage) (*tenantv1.Garage, error) {
	if a.state.createError != nil {
		return nil, a.state.createError
	}

	garage.Id = fmt.Sprintf("garage-%d", len(a.state.garages[a.email])+1)
	a.state.garages[a.email] = append(a.state.garages[a.email], garage)

	return garage, nil
}

func (a *fakeAPI) WithToken(token string) seed.API {
	return &fakeAPI{state: a.state, email: a.state.tokens[token]}
}

type fakeInbox struct{}

func (fakeInbox) WaitForMessage(context.Context, string) (*client.Message, error) {
	return &client.Message{Text: "verification code 123456"}, nil
}

func (s *fakeState) session(email string, user *identityv1.User) *identityv1.Session {
	s.nextToken++
	token := fmt.Sprintf("token-%d", s.nextToken)
	s.tokens[token] = email

	return &identityv1.Session{
		AccessToken:  token,
		RefreshToken: "refresh-" + token,
		User: &identityv1.User{
			Email:         user.GetEmail(),
			FullName:      user.GetFullName(),
			EmailVerified: user.GetEmailVerified(),
		},
	}
}

func TestRunSeedsOwnersAndGaragesIdempotently(t *testing.T) {
	t.Parallel()

	state := newFakeState()
	api := &fakeAPI{state: state}
	dataset := sampleDataset()

	first, err := seed.Run(t.Context(), dataset, api, fakeInbox{}, "blackbox.vietgara.test", "test-password")
	if err != nil {
		t.Fatal(err)
	}

	if first.CreatedOwners != 2 || first.ExistingOwners != 0 || first.ChangedPlans != 1 ||
		first.CreatedGarages != 2 || first.ExistingGarages != 0 {
		t.Fatalf("first run summary = %+v", first)
	}

	if state.registers != 2 || state.verifications != 2 || state.planChanges != 1 {
		t.Fatalf("registration/verification/plan-change calls = %d/%d/%d, want 2/2/1",
			state.registers, state.verifications, state.planChanges)
	}

	second, err := seed.Run(t.Context(), dataset, api, fakeInbox{}, "blackbox.vietgara.test", "test-password")
	if err != nil {
		t.Fatal(err)
	}

	if second.CreatedOwners != 0 || second.ExistingOwners != 2 || second.ChangedPlans != 0 ||
		second.CreatedGarages != 0 || second.ExistingGarages != 2 {
		t.Fatalf("second run summary = %+v", second)
	}

	if state.registers != 2 || state.verifications != 2 || state.planChanges != 1 {
		t.Fatalf("rerun changed owners or plans: calls = %d/%d/%d",
			state.registers, state.verifications, state.planChanges)
	}

	if len(state.garages["owner1@blackbox.vietgara.test"]) != 1 ||
		len(state.garages["owner2@blackbox.vietgara.test"]) != 1 {
		t.Fatal("rerun created duplicate garages")
	}
}

func TestRunRejectsSameNameGarageWithDifferentData(t *testing.T) {
	t.Parallel()

	dataset := sampleDataset()
	state := newFakeState()
	api := &fakeAPI{state: state}

	if _, err := seed.Run(t.Context(), dataset, api, fakeInbox{}, "blackbox.vietgara.test", "test-password"); err != nil {
		t.Fatal(err)
	}

	dataset.Garages[0].Address = "changed address"
	if _, err := seed.Run(t.Context(), dataset, api, fakeInbox{}, "blackbox.vietgara.test", "test-password"); err == nil {
		t.Fatal("Run() succeeded when an existing garage differed from the dataset")
	}

	if got := len(state.garages["owner1@blackbox.vietgara.test"]); got != 1 {
		t.Fatalf("garage count after mismatch = %d, want 1", got)
	}
}

func TestRunRejectsGarageWithMissingOwner(t *testing.T) {
	t.Parallel()

	dataset := sampleDataset()
	dataset.Garages[0].Owner = "unknown"

	_, err := seed.Run(t.Context(), dataset, newFakeAPI(), fakeInbox{}, "blackbox.vietgara.test", "test-password")
	if err == nil {
		t.Fatal("Run() succeeded with a garage owner missing from the dataset")
	}
}

func TestRunRejectsUnsupportedOwnerSetup(t *testing.T) {
	t.Parallel()

	dataset := sampleDataset()
	dataset.Garages[0].Owner = "admin"

	_, err := seed.Run(t.Context(), dataset, newFakeAPI(), fakeInbox{}, "blackbox.vietgara.test", "test-password")
	if err == nil {
		t.Fatal("Run() succeeded for a platform-admin owner")
	}
}

func TestRunReturnsGarageCreationError(t *testing.T) {
	t.Parallel()

	state := newFakeState()
	state.createError = errGarageCreationFailed
	api := &fakeAPI{state: state}

	_, err := seed.Run(t.Context(), sampleDataset(), api, fakeInbox{}, "blackbox.vietgara.test", "test-password")
	if err == nil || !strings.Contains(err.Error(), "create failed") {
		t.Fatalf("Run() error = %v, want create failure", err)
	}
}

func TestRunRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dataset *testdata.Dataset
		api     seed.API
		inbox   seed.Inbox
	}{
		{name: "nil dataset", api: newFakeAPI(), inbox: fakeInbox{}},
		{name: "nil API", dataset: sampleDataset(), inbox: fakeInbox{}},
		{name: "nil inbox", dataset: sampleDataset(), api: newFakeAPI()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := seed.Run(t.Context(), tt.dataset, tt.api, tt.inbox, "blackbox.vietgara.test", "test-password")
			if err == nil {
				t.Fatal("Run() succeeded with invalid input")
			}
		})
	}
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{state: newFakeState()}
}

func newFakeState() *fakeState {
	return &fakeState{
		users:         make(map[string]*identityv1.User),
		subscriptions: make(map[string]*licensev1.Subscription),
		garages:       make(map[string][]*tenantv1.Garage),
		tokens:        make(map[string]string),
	}
}

func sampleDataset() *testdata.Dataset {
	return &testdata.Dataset{
		Users: []testdata.User{
			{Key: "owner1", Name: "Owner One", Phone: "0900000001", Plan: "pro"},
			{Key: "owner2", Name: "Owner Two", Phone: "0900000002", Plan: "trial"},
			{Key: "admin", Name: "Admin", PlatformAdmin: true},
		},
		Garages: []testdata.Garage{
			{Key: "gara1-1", Owner: "owner1", Name: "Garage One", Address: "Address One", Phone: "0280000001", TaxCode: "TAX1"},
			{Key: "gara2-1", Owner: "owner2", Name: "Garage Two", Address: "Address Two", Phone: "0280000002", TaxCode: "TAX2"},
		},
	}
}
