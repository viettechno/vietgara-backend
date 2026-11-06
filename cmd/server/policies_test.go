package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
)

// publicMethods is the complete list of RPCs that need no access token. It is
// the attack surface of an anonymous caller (the gateway rate-limits these
// routes, kong/kong.yml): adding to it is a security decision that a review
// must see in this diff. The OAuth redirect of Google sign-in is plain HTTP
// and is not an RPC.
var publicMethods = []string{
	"/vietgara.identity.v1.AuthService/Login",
	"/vietgara.identity.v1.AuthService/RefreshToken",
	"/vietgara.identity.v1.AuthService/Logout",
	"/vietgara.identity.v1.AuthService/Register",
	"/vietgara.identity.v1.AuthService/RequestPasswordReset",
	"/vietgara.identity.v1.AuthService/ResetPassword",
	"/vietgara.identity.v1.AuthService/SignInWithGoogleIdToken",
	"/vietgara.identity.v1.AuthService/VerifyMfaChallenge",
	"/vietgara.identity.v1.AuthService/VerifyPasswordResetOtp",
	"/vietgara.quote.v1.PublicQuoteService/DecidePublicQuote",
	"/vietgara.quote.v1.PublicQuoteService/GetPublicQuote",
}

// notImplemented are RPCs the contract declares and the backend does not serve
// yet. They have no policy on purpose, so the gateway of the application
// answers NO_POLICY (403) instead of reaching the unimplemented handler. Give
// the method a policy when you implement it, and remove it from here.
var notImplemented = map[string]string{
	"/vietgara.customer.v1.DataRequestService/CompleteDataRequest":                 "Phase 13",
	"/vietgara.customer.v1.DataRequestService/CreateDataRequest":                   "Phase 13",
	"/vietgara.customer.v1.DataRequestService/GetDataRequest":                      "Phase 13",
	"/vietgara.customer.v1.DataRequestService/ListDataRequests":                    "Phase 13",
	"/vietgara.customer.v1.PublicDataRequestService/ConfirmPublicDataRequest":      "Phase 13",
	"/vietgara.customer.v1.PublicDataRequestService/SubmitPublicDataRequest":       "Phase 13",
	"/vietgara.document.v1.DocumentService/ExportPurchaseOrderPdf":                 "Phase 13",
	"/vietgara.document.v1.DocumentService/PrintPurchaseOrderPdf":                  "Phase 13",
	"/vietgara.inventory.v1.PurchaseOrderService/CancelPurchaseOrder":              "Phase 13",
	"/vietgara.inventory.v1.PurchaseOrderService/CreatePurchaseOrder":              "Phase 13",
	"/vietgara.inventory.v1.PurchaseOrderService/CreatePurchaseOrdersFromLowStock": "Phase 13",
	"/vietgara.inventory.v1.PurchaseOrderService/GetPurchaseOrder":                 "Phase 13",
	"/vietgara.inventory.v1.PurchaseOrderService/ListPurchaseOrders":               "Phase 13",
	"/vietgara.inventory.v1.PurchaseOrderService/ReceivePurchaseOrder":             "Phase 13",
	"/vietgara.inventory.v1.PurchaseOrderService/SendPurchaseOrder":                "Phase 13",
	"/vietgara.inventory.v1.PurchaseOrderService/UpdatePurchaseOrder":              "Phase 13",
	"/vietgara.reporting.v1.ReportingService/GetProfitReport":                      "Phase 13",
	"/vietgara.reporting.v1.ReportingService/GetRepairOrderProfit":                 "Phase 13",
	"/vietgara.reporting.v1.ReportingService/GetTechnicianReport":                  "Phase 13",
}

// TestEveryRPCDeclaresItsAuthorization is the automated part of the
// authorization review (Security Architecture 3): every method of every
// service the binary links has a policy (middleware.Auth already rejects the
// others at run time, so a miss is a dead endpoint, not an open one), no
// policy outlives its method, and the anonymous surface is the list above.
func TestEveryRPCDeclaresItsAuthorization(t *testing.T) {
	t.Parallel()

	policies := apiPolicies()

	var methods []string

	protoregistry.GlobalFiles.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		if !strings.HasPrefix(string(file.Package()), "vietgara.") {
			return true
		}

		for i := range file.Services().Len() {
			service := file.Services().Get(i)

			for j := range service.Methods().Len() {
				methods = append(methods, "/"+string(service.FullName())+"/"+string(service.Methods().Get(j).Name()))
			}
		}

		return true
	})

	require.NotEmpty(t, methods, "the generated services are linked into the binary")

	var public []string

	for _, method := range methods {
		policy, ok := policies[method]
		if _, pending := notImplemented[method]; pending {
			assert.False(t, ok, "%s is implemented now: remove it from notImplemented", method)

			continue
		}

		if !assert.True(t, ok, "%s has no policy", method) {
			continue
		}

		if policy.Public {
			public = append(public, method)
			assert.Nil(t, policy.Garage, "%s is public and garage-scoped at once", method)
			assert.Empty(t, policy.Platform, "%s is public and platform-only at once", method)
		}

		if policy.Garage != nil {
			assert.Empty(t, policy.Platform, "%s is garage-scoped and platform-only at once", method)
		}

		if policy.Idempotent {
			assert.NotNil(t, policy.Garage, "%s requires an Idempotency-Key but is not garage-scoped", method)
		}
	}

	known := map[string]bool{}
	for _, method := range methods {
		known[method] = true
	}

	for method := range policies {
		assert.True(t, known[method], "%s has a policy but no such method is linked", method)
	}

	sort.Strings(public)

	want := append([]string(nil), publicMethods...)
	sort.Strings(want)

	assert.Equal(t, want, public, "the anonymous surface changed: update publicMethods after a security review")
}

// reservedPlatformPermissions are catalogue permissions that no RPC checks
// yet, because the screens they are named for do not exist: staff can be
// given them in a role, and they open nothing until then. Give the permission
// to its RPC when it is built, and remove it from here.
var reservedPlatformPermissions = map[string]string{
	string(access.PlatformStatsRead): "platform statistics dashboard (Phase 3 admin dashboards)",
	string(access.PlatformUserRead):  "platform user directory (Phase 3 admin dashboards)",
}

// TestEveryPlatformPermissionGuardsSomething keeps the platform permission
// catalogue honest: each permission is the policy of at least one RPC, or is
// listed above as reserved, and a reserved one is not checked anywhere.
func TestEveryPlatformPermissionGuardsSomething(t *testing.T) {
	t.Parallel()

	guarded := map[string]bool{}

	for _, policy := range apiPolicies() {
		if policy.Platform != "" {
			guarded[string(policy.Platform)] = true
		}
	}

	for _, entry := range access.PlatformCatalog {
		name := string(entry.Permission)
		reason, reserved := reservedPlatformPermissions[name]

		if reserved {
			assert.False(t, guarded[name], "%s is checked by an RPC: remove it from reservedPlatformPermissions (%s)", name, reason)

			continue
		}

		assert.True(t, guarded[name], "%s guards no RPC: give it to the RPC it is meant for, or list it in reservedPlatformPermissions", name)
	}
}
