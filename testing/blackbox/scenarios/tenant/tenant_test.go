package tenant_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// groupIDs maps default group names to IDs for a garage.
func groupIDs(t *testing.T, api *client.Client, garageID string) map[string]string {
	t.Helper()

	groups, err := api.ListStaffGroups(t.Context(), garageID)
	require.NoError(t, err)

	out := make(map[string]string, len(groups))
	for _, g := range groups {
		out[g.GetName()] = g.GetId()
	}

	return out
}

// staffByUserID returns the staff row of a garage member.
func staffByUserID(t *testing.T, api *client.Client, garageID, userID string) *tenantv1.Staff {
	t.Helper()

	staff, err := api.ListStaff(t.Context(), garageID)
	require.NoError(t, err)

	for _, s := range staff {
		if s.GetUserId() == userID {
			return s
		}
	}

	t.Fatalf("no staff row for user %s in garage %s", userID, garageID)

	return nil
}

// invitePending registers a fresh verified user, looks it up and invites
// it to the garage; the invitation stays pending for the caller to drive.
func invitePending(t *testing.T, ownerAPI *client.Client, garageID string, groupIDs []string, label string) (*fixture.Actor, *tenantv1.Invitation) {
	t.Helper()

	invitee := fixture.NewVerifiedOwner(t, label)

	cand, err := ownerAPI.LookupStaffCandidate(t.Context(), garageID, invitee.Email)
	require.NoError(t, err)
	require.Equal(t, invitee.Session.GetUser().GetId(), cand.GetUserId())

	inv, err := ownerAPI.CreateInvitation(t.Context(), garageID, cand.GetUserId(), groupIDs)
	require.NoError(t, err)
	require.Equal(t, tenantv1.InvitationStatus_INVITATION_STATUS_PENDING, inv.GetStatus())

	return invitee, inv
}

// inviteMember invites a fresh verified user and accepts as them.
func inviteMember(t *testing.T, ownerAPI *client.Client, garageID string, groupIDs []string, label string) *fixture.Actor {
	t.Helper()

	invitee, inv := invitePending(t, ownerAPI, garageID, groupIDs, label)

	accepted, err := invitee.API.AcceptInvitation(t.Context(), inv.GetId())
	require.NoError(t, err)
	require.Equal(t, tenantv1.InvitationStatus_INVITATION_STATUS_ACCEPTED, accepted.GetStatus())

	return invitee
}

// invitationByID finds an invitation in a list.
func invitationByID(t *testing.T, invitations []*tenantv1.Invitation, id string) *tenantv1.Invitation {
	t.Helper()

	for _, inv := range invitations {
		if inv.GetId() == id {
			return inv
		}
	}

	t.Fatalf("invitation %s not listed", id)

	return nil
}

// upgradeToPro moves a fresh (trial) owner to the Pro plan so tests that
// need two garages pass the plan limit.
func upgradeToPro(t *testing.T, owner *fixture.Actor) {
	t.Helper()

	fixture.SwitchPlan(t, owner, "Pro")
}
