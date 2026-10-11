// Package seed creates the supported owner, plan, and garage baseline through the public API.
package seed

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"
	testdata "github.com/viettechno/vietgara-testdata/go"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
)

// API is the public API surface the baseline seeder needs.
type API interface {
	Login(context.Context, string, string) (*identityv1.Session, error)
	Register(context.Context, *identityv1.RegisterRequest) (*identityv1.Session, error)
	VerifyEmail(context.Context, string, string) (*identityv1.Session, error)
	MySubscription(context.Context) (*licensev1.Subscription, error)
	ListPlans(context.Context) ([]*licensev1.Plan, error)
	ChangeMyPlan(context.Context, string) (*licensev1.Subscription, error)
	ListGarages(context.Context) ([]*tenantv1.Garage, error)
	CreateGarage(context.Context, *tenantv1.Garage) (*tenantv1.Garage, error)
	WithToken(string) API
}

// Inbox supplies verification messages for newly registered baseline users.
type Inbox interface {
	WaitForMessage(context.Context, string) (*client.Message, error)
}

// Summary reports how many baseline records were created or already present.
type Summary struct {
	CreatedOwners   int
	ExistingOwners  int
	ChangedPlans    int
	CreatedGarages  int
	ExistingGarages int
}

// Run creates dataset owners referenced by garages, applies any dataset plan
// for those owners, and creates the garages. It preserves matching existing
// records and rejects same-name garages with different dataset values rather
// than silently changing shared state.
func Run(ctx context.Context, dataset *testdata.Dataset, apiClient API, mail Inbox, emailDomain, password string) (Summary, error) {
	if dataset == nil {
		return Summary{}, errors.New("shared dataset is nil")
	}

	if apiClient == nil || mail == nil {
		return Summary{}, errors.New("API and verification inbox are required")
	}

	if len(dataset.Garages) == 0 {
		return Summary{}, errors.New("shared dataset contains no garages to seed")
	}

	owners, err := datasetOwners(dataset)
	if err != nil {
		return Summary{}, err
	}

	sessions, summary, err := seedOwners(ctx, apiClient, mail, owners, emailDomain, password)
	if err != nil {
		return summary, err
	}

	garageSummary, err := seedGarages(ctx, apiClient, sessions, dataset.Garages)
	if err != nil {
		summary.CreatedGarages = garageSummary.CreatedGarages
		summary.ExistingGarages = garageSummary.ExistingGarages

		return summary, err
	}

	summary.CreatedGarages = garageSummary.CreatedGarages
	summary.ExistingGarages = garageSummary.ExistingGarages

	return summary, nil
}

func datasetOwners(dataset *testdata.Dataset) ([]testdata.User, error) {
	ownersByKey := make(map[string]testdata.User, len(dataset.Users))
	for _, user := range dataset.Users {
		ownersByKey[user.Key] = user
	}

	needed := make(map[string]struct{}, len(dataset.Garages))
	for _, garage := range dataset.Garages {
		needed[garage.Owner] = struct{}{}
		if _, ok := ownersByKey[garage.Owner]; !ok {
			return nil, fmt.Errorf("garage owner %q is missing from the dataset", garage.Owner)
		}
	}

	owners := make([]testdata.User, 0, len(needed))
	for _, user := range dataset.Users {
		if _, ok := needed[user.Key]; ok {
			owners = append(owners, user)
		}
	}

	return owners, nil
}

func seedOwners(
	ctx context.Context,
	apiClient API,
	mail Inbox,
	owners []testdata.User,
	emailDomain, password string,
) (map[string]*identityv1.Session, Summary, error) {
	sessions := make(map[string]*identityv1.Session, len(owners))

	var summary Summary

	for _, user := range owners {
		if user.PlatformAdmin || user.Requires != "" {
			return nil, summary, fmt.Errorf("owner %q requires unsupported platform or feature setup", user.Key)
		}

		email := user.Key + "@" + emailDomain

		session, created, err := ensureOwner(ctx, apiClient, mail, user, email, password)
		if err != nil {
			return nil, summary, fmt.Errorf("seed owner %q: %w", user.Key, err)
		}

		sessions[user.Key] = session

		if created {
			summary.CreatedOwners++
		} else {
			summary.ExistingOwners++
		}

		if user.Plan != "" {
			ownerAPI := apiClient.WithToken(session.GetAccessToken())

			changed, err := ensurePlan(ctx, ownerAPI, user.Plan)
			if err != nil {
				return nil, summary, fmt.Errorf("set plan for owner %q: %w", user.Key, err)
			}

			if changed {
				summary.ChangedPlans++
			}
		}
	}

	return sessions, summary, nil
}

func ensurePlan(ctx context.Context, apiClient API, planCode string) (bool, error) {
	subscription, err := apiClient.MySubscription(ctx)
	if err != nil {
		return false, fmt.Errorf("read subscription: %w", err)
	}

	if planMatches(subscription.GetPlan(), planCode) {
		return false, nil
	}

	plans, err := apiClient.ListPlans(ctx)
	if err != nil {
		return false, fmt.Errorf("list public plans: %w", err)
	}

	var selected *licensev1.Plan

	for _, plan := range plans {
		if !strings.EqualFold(plan.GetCode(), planCode) {
			continue
		}

		selected = plan

		break
	}

	if selected == nil {
		for _, plan := range plans {
			if !strings.EqualFold(plan.GetName(), planCode) {
				continue
			}

			if selected != nil {
				return false, fmt.Errorf("multiple public plans match %q", planCode)
			}

			selected = plan
		}
	}

	if selected == nil {
		return false, fmt.Errorf("public plan %q was not found", planCode)
	}

	if _, err := apiClient.ChangeMyPlan(ctx, selected.GetId()); err != nil {
		return false, fmt.Errorf("change subscription: %w", err)
	}

	return true, nil
}

func planMatches(plan *licensev1.Plan, planCode string) bool {
	return strings.EqualFold(plan.GetCode(), planCode) || strings.EqualFold(plan.GetName(), planCode)
}

func seedGarages(
	ctx context.Context,
	apiClient API,
	sessions map[string]*identityv1.Session,
	garages []testdata.Garage,
) (Summary, error) {
	var summary Summary

	for _, garage := range garages {
		session := sessions[garage.Owner]
		if session == nil {
			return summary, fmt.Errorf("garage %q has no seeded owner %q", garage.Key, garage.Owner)
		}

		ownerAPI := apiClient.WithToken(session.GetAccessToken())

		created, err := seedGarage(ctx, ownerAPI, garage)
		if err != nil {
			return summary, fmt.Errorf("seed garage %q: %w", garage.Key, err)
		}

		if created {
			summary.CreatedGarages++
		} else {
			summary.ExistingGarages++
		}
	}

	return summary, nil
}

func seedGarage(ctx context.Context, apiClient API, garage testdata.Garage) (bool, error) {
	existingGarages, err := apiClient.ListGarages(ctx)
	if err != nil {
		return false, fmt.Errorf("list garages: %w", err)
	}

	var match *tenantv1.Garage

	for _, existing := range existingGarages {
		if existing.GetName() != garage.Name {
			continue
		}

		if match != nil {
			return false, fmt.Errorf("multiple garages are named %q", garage.Name)
		}

		match = existing
	}

	if match != nil {
		if !garageMatches(match, garage) {
			return false, errors.New("existing garage values differ from the dataset")
		}

		return false, nil
	}

	_, err = apiClient.CreateGarage(ctx, &tenantv1.Garage{
		Name:    garage.Name,
		Address: stringPointer(garage.Address),
		Phone:   stringPointer(garage.Phone),
		TaxCode: stringPointer(garage.TaxCode),
	})
	if err != nil {
		return false, fmt.Errorf("create garage: %w", err)
	}

	return true, nil
}

func ensureOwner(
	ctx context.Context,
	apiClient API,
	mail Inbox,
	user testdata.User,
	email, password string,
) (*identityv1.Session, bool, error) {
	session, err := apiClient.Login(ctx, email, password)
	if err == nil {
		return checkOwnerSession(session, user, email, false)
	}

	apiErr := client.AsAPIError(err)
	if apiErr != nil && apiErr.Status == http.StatusUnauthorized && apiErr.Code == "INVALID_CREDENTIALS" {
		return registerOwner(ctx, apiClient, mail, user, email, password)
	}

	return nil, false, fmt.Errorf("sign in: %w", err)
}

func registerOwner(
	ctx context.Context,
	apiClient API,
	mail Inbox,
	user testdata.User,
	email, password string,
) (*identityv1.Session, bool, error) {
	var phone *string
	if user.Phone != "" {
		phone = stringPointer(user.Phone)
	}

	registered, err := apiClient.Register(ctx, &identityv1.RegisterRequest{
		Email: email, Password: password, FullName: user.Name, Phone: phone,
	})
	if err != nil {
		return nil, false, fmt.Errorf("register: %w", err)
	}

	message, err := mail.WaitForMessage(ctx, email)
	if err != nil {
		return nil, false, fmt.Errorf("wait for verification e-mail: %w", err)
	}

	otp, err := message.OTP()
	if err != nil {
		return nil, false, fmt.Errorf("read verification code: %w", err)
	}

	verified, err := apiClient.WithToken(registered.GetAccessToken()).VerifyEmail(ctx, otp, registered.GetRefreshToken())
	if err != nil {
		return nil, false, fmt.Errorf("verify e-mail: %w", err)
	}

	return checkOwnerSession(verified, user, email, true)
}

func checkOwnerSession(session *identityv1.Session, user testdata.User, email string, newlyRegistered bool) (*identityv1.Session, bool, error) {
	if session.GetUser().GetEmail() != email || !session.GetUser().GetEmailVerified() {
		return nil, false, errors.New("API returned an unexpected or unverified user")
	}

	if session.GetUser().GetFullName() != user.Name {
		return nil, false, errors.New("user name differs from the dataset")
	}

	return session, newlyRegistered, nil
}

func garageMatches(existing *tenantv1.Garage, expected testdata.Garage) bool {
	return existing.GetAddress() == expected.Address &&
		existing.GetPhone() == expected.Phone &&
		existing.GetTaxCode() == expected.TaxCode
}

func stringPointer(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return &value
}
