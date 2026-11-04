package fixture_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func TestUniquePhoneIsAMobileNumberAndNeverRepeats(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}

	for range 5000 {
		phone := fixture.UniquePhone()
		assert.Regexp(t, `^09\d{8}$`, phone)
		assert.False(t, seen[phone], "repeated: %s", phone)

		seen[phone] = true
	}
}

func TestUniquePlanCodeNeverRepeats(t *testing.T) {
	t.Parallel()

	first, second := fixture.UniquePlanCode("qa"), fixture.UniquePlanCode("qa")
	assert.NotEqual(t, first, second)
	assert.Regexp(t, `^qa-hidden-\d+-\d+$`, fixture.UniquePlanCode("qa-hidden"))
}
