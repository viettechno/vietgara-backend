package fixture

import (
	"testing"
	"time"

	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"
)

// WaitForSubscriptionStatus polls the owner's subscription until it has
// want, failing tb after timeout. The stubbed stack runs the expiry job
// every few seconds, so a time-shifted subscription changes status soon.
func WaitForSubscriptionStatus(tb testing.TB, owner *Actor, want licensev1.SubscriptionStatus, timeout time.Duration) *licensev1.Subscription {
	tb.Helper()

	deadline := time.Now().Add(timeout)

	var last *licensev1.Subscription

	for {
		sub, err := owner.API.MySubscription(tb.Context())
		if err != nil {
			tb.Fatalf("read subscription of %s: %v", owner.Email, err)
		}

		last = sub
		if sub.GetStatus() == want {
			return sub
		}

		if time.Now().After(deadline) {
			tb.Fatalf("subscription of %s is %s after %s, want %s", owner.Email, last.GetStatus(), timeout, want)
		}

		time.Sleep(time.Second)
	}
}
