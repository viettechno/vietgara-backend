// Scenario: E2E-LIC-022
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/03_License_Subscription.md
package license_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// The admin sets the trial length on the trial plan: it applies to trials
// started afterwards, a running trial keeps its end, and only the trial plan
// has a length (FR-LIC-01, FR-LIC-05). The test changes a platform-wide
// setting, so it does not run in parallel and puts the setting back.
func TestAdminSetsTheTrialLength(t *testing.T) {
	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)

	plans, err := admin.API.ListAllPlans(ctx)
	require.NoError(t, err)

	var trial, starter *licensev1.Plan

	for _, p := range plans {
		switch p.GetCode() {
		case "trial":
			trial = p
		case "starter":
			starter = p
		}
	}

	require.NotNil(t, trial)
	require.NotNil(t, starter)

	original := trial.GetTrialDays()
	require.Positive(t, original)

	t.Cleanup(func() {
		restoreCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		_, restoreErr := admin.API.UpdatePlan(restoreCtx, trial.GetId(), &licensev1.Plan{TrialDays: original}, "trial_days")
		require.NoError(t, restoreErr)
	})

	// The trial of a new owner starts when the subscription is first read.
	early := fixture.NewVerifiedOwner(t, "lic-trial-early")

	earlyBefore, err := early.API.MySubscription(ctx)
	require.NoError(t, err)

	_, err = admin.API.UpdatePlan(ctx, trial.GetId(), &licensev1.Plan{TrialDays: 14}, "trial_days")
	require.NoError(t, err)

	late := fixture.NewVerifiedOwner(t, "lic-trial-late")

	lateSub, err := late.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().AddDate(0, 0, 14), lateSub.GetTrialEndAt().AsTime(), time.Hour, "the new trial lasts 14 days")

	assert.WithinDuration(t, time.Now().AddDate(0, 0, int(original)), earlyBefore.GetTrialEndAt().AsTime(), time.Hour)

	earlyAfter, err := early.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.Equal(t, earlyBefore.GetTrialEndAt().AsTime(), earlyAfter.GetTrialEndAt().AsTime(), "a trial that started before keeps its end")

	_, err = admin.API.UpdatePlan(ctx, trial.GetId(), &licensev1.Plan{TrialDays: 0}, "trial_days")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	_, err = admin.API.UpdatePlan(ctx, starter.GetId(), &licensev1.Plan{TrialDays: 14}, "trial_days")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
}
