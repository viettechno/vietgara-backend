package middleware_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viettechno/vietgara-backend/internal/middleware"
)

func TestLicenseModes(t *testing.T) {
	t.Parallel()

	assert.NotEqual(t, middleware.LicenseFull, middleware.LicenseReadOnly)
	assert.NotEqual(t, middleware.LicenseReadOnly, middleware.LicenseLocked)
}
