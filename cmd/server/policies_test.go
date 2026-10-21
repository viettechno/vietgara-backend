package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
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
	"/vietgara.customer.v1.CustomerService/LookupCustomers": "FR-CUS-06, with the customer app's quote preparation",
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
			assert.False(t, policy.PlatformAdmin, "%s is public and admin-only at once", method)
		}

		if policy.Garage != nil {
			assert.False(t, policy.PlatformAdmin, "%s is garage-scoped and admin-only at once", method)
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
