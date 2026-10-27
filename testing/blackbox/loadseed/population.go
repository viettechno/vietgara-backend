// Package loadseed creates the population of the Release 9.0 load test through
// the public API: one owner and garage per generated number, each with a
// catalogue, stock, jobs, customers and vehicles. The shape and the pools come
// from the load section of the shared dataset; the output is the Population
// file the k6 script reads.
package loadseed

import (
	"fmt"
	"strings"

	testdata "github.com/viettechno/vietgara-testdata/go"
)

// Population is what the k6 script needs to drive the garages: ids only, no
// credentials (the password comes from the environment, as in the dataset).
type Population struct {
	Profile string  `json:"profile"`
	Owners  []Owner `json:"owners"`
}

// Owner is one load-test owner with the single garage they run.
type Owner struct {
	Email     string     `json:"email"`
	GarageID  string     `json:"garageId"`
	Customers []Customer `json:"customers"`
	Parts     []Part     `json:"parts"`
	Jobs      []Job      `json:"jobs"`
}

// Customer is a customer with the ids of their vehicles.
type Customer struct {
	ID         string   `json:"id"`
	VehicleIDs []string `json:"vehicleIds"`
}

// Part is a stocked part with its selling price (VND per base unit).
type Part struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SalePrice int64  `json:"salePrice"`
}

// Job is a catalogue job with its standard labor price (VND).
type Job struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	LaborPrice int64  `json:"laborPrice"`
}

// ownerKey, garageName, ownerName and ownerPhone generate the identity of the
// n-th owner (n starts at 1); the same n always gives the same values.
func ownerKey(n int) string   { return fmt.Sprintf("loadowner%d", n) }
func garageName(n int) string { return fmt.Sprintf("QA Load Gara %d", n) }
func ownerName(n int) string  { return fmt.Sprintf("QA Load Owner %d", n) }
func ownerPhone(n int) string { return fmt.Sprintf("0902%06d", n) }

// customerName builds the i-th (from 0) customer name of garage n from the
// pools. Within a garage the pair (i mod len(family), i / len(family)) is
// unique, so names do not repeat until the pools are exhausted.
func customerName(pool *testdata.LoadTest, n, i int) string {
	family := pool.FamilyNames[(i+n)%len(pool.FamilyNames)]
	given := pool.GivenNames[(i/len(pool.FamilyNames)+n)%len(pool.GivenNames)]

	return family + " " + given
}

// plate builds the j-th vehicle plate (from 0) of the i-th customer of garage n.
func plate(profile testdata.LoadProfile, n, i, j int) string {
	return fmt.Sprintf("5%dA-%05d", n%10, i*profile.VehiclesPerCustomer+j+1)
}

// customerPhone is a mobile number unique within garage n.
func customerPhone(n, i int) string { return fmt.Sprintf("09%02d%06d", n%100, i+1) }

// profileByKey returns the profile named key, or the only one when key is empty.
func profileByKey(load *testdata.LoadTest, key string) (testdata.LoadProfile, error) {
	if load == nil {
		return testdata.LoadProfile{}, errNoLoadSection
	}

	if key == "" && len(load.Profiles) == 1 {
		return load.Profiles[0], nil
	}

	for _, profile := range load.Profiles {
		if profile.Key == key {
			return profile, nil
		}
	}

	keys := make([]string, 0, len(load.Profiles))
	for _, profile := range load.Profiles {
		keys = append(keys, profile.Key)
	}

	return testdata.LoadProfile{}, fmt.Errorf("%w %q (have %s)", errUnknownProfile, key, strings.Join(keys, ", "))
}
