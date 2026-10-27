//nolint:testpackage // the generators under test are unexported
package loadseed

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	testdata "github.com/viettechno/vietgara-testdata/go"
)

func testPool() *testdata.LoadTest {
	return &testdata.LoadTest{
		Profiles: []testdata.LoadProfile{
			{Key: "garages50", Garages: 50, CustomersPerGarage: 30, VehiclesPerCustomer: 2},
			{Key: "tiny", Garages: 2, CustomersPerGarage: 3, VehiclesPerCustomer: 1},
		},
		FamilyNames: []string{"Nguyen", "Tran", "Le", "Pham", "Hoang", "Huynh", "Phan", "Vu", "Vo", "Dang", "Bui", "Do", "Ho", "Ngo", "Duong", "Ly"},
		GivenNames:  []string{"An", "Binh", "Chi", "Dung", "Giang", "Hanh", "Hung", "Khanh", "Lan", "Minh", "Nam", "Phuc", "Quang", "Thao", "Trang", "Tuan"},
	}
}

// Names, plates and phones must be unique inside a garage (the seeder matches
// existing records by name and plate), and the same inputs must give the same
// values (so a second run finds what the first created).
func TestGeneratedValuesAreUniqueWithinAGarageAndStable(t *testing.T) {
	t.Parallel()

	pool := testPool()
	profile := pool.Profiles[0]

	for _, garage := range []int{1, 2, 11, 50} {
		names, plates, phones := map[string]bool{}, map[string]bool{}, map[string]bool{}

		for i := range profile.CustomersPerGarage {
			name := customerName(pool, garage, i)
			assert.False(t, names[name], "garage %d customer %d repeats name %q", garage, i, name)
			names[name] = true
			assert.Equal(t, name, customerName(pool, garage, i))

			phone := customerPhone(garage, i)
			assert.False(t, phones[phone], "garage %d repeats phone %s", garage, phone)
			phones[phone] = true

			for j := range profile.VehiclesPerCustomer {
				licensePlate := plate(profile, garage, i, j)
				assert.False(t, plates[licensePlate], "garage %d repeats plate %s", garage, licensePlate)
				plates[licensePlate] = true
			}
		}
	}
}

func TestOwnerIdentityIsDerivedFromItsNumber(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "loadowner7", ownerKey(7))
	assert.Equal(t, "QA Load Gara 7", garageName(7))
	assert.NotEqual(t, ownerPhone(7), ownerPhone(8))
	assert.Len(t, ownerPhone(50), 10)
}

func TestProfileByKey(t *testing.T) {
	t.Parallel()

	pool := testPool()

	profile, err := profileByKey(pool, "tiny")
	require.NoError(t, err)
	assert.Equal(t, 2, profile.Garages)

	_, err = profileByKey(pool, "")
	require.ErrorIs(t, err, errUnknownProfile, "an empty key is ambiguous with two profiles")

	_, err = profileByKey(pool, "nope")
	require.ErrorIs(t, err, errUnknownProfile)

	pool.Profiles = pool.Profiles[:1]
	profile, err = profileByKey(pool, "")
	require.NoError(t, err)
	assert.Equal(t, "garages50", profile.Key, "the only profile is the default")

	_, err = profileByKey(nil, "")
	require.ErrorIs(t, err, errNoLoadSection)
}
