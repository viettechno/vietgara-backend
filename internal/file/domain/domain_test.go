package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viettechno/vietgara-backend/internal/file/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
)

func TestKind(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.KindLogo.IsValid())
	assert.False(t, domain.Kind("repair_photo").IsValid())
	assert.False(t, domain.Kind("").IsValid())
}

func TestLogoRule(t *testing.T) {
	t.Parallel()

	rule := domain.Rules[domain.KindLogo]
	assert.True(t, rule.Public)
	assert.True(t, rule.AttachedByModule)
	assert.Equal(t, access.GarageWrite, rule.Write)
	assert.Equal(t, 1<<20, rule.MaxBytes)
	assert.Equal(t, "jpg", rule.ContentTypes["image/jpeg"])
}

func TestObjectKey(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "garages/g1/logo/g1/f1.png", domain.ObjectKey("g1", domain.KindLogo, "g1", "f1", "png"))
}
