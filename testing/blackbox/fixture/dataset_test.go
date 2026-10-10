package fixture_test

import (
	"testing"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func TestDatasetLoadsPinnedBaseline(t *testing.T) {
	dataset := fixture.Dataset(t)

	if dataset.Version != 1 {
		t.Fatalf("dataset version = %d, want 1", dataset.Version)
	}

	if dataset.EmailDomain == "" {
		t.Fatal("dataset email domain is empty")
	}

	foundOwner := false

	for _, user := range dataset.Users {
		if user.Key == "owner1" {
			foundOwner = true
			break
		}
	}

	if !foundOwner {
		t.Fatal("dataset does not contain baseline user owner1")
	}

	foundGarage := false

	for _, garage := range dataset.Garages {
		if garage.Key == "gara1-1" {
			foundGarage = true
			break
		}
	}

	if !foundGarage {
		t.Fatal("dataset does not contain baseline garage gara1-1")
	}
}
