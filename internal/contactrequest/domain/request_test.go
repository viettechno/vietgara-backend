package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/contactrequest/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func valid() domain.Input {
	return domain.Input{Name: "  Nguyễn Văn A ", GarageName: "Gara Minh Phát", Phone: "0938 038 621", Email: "a@example.com", Message: "Cho mình xin demo.\nCảm ơn.", Consent: true}
}

func TestNormalizeAcceptsAndTrims(t *testing.T) {
	t.Parallel()

	got, err := valid().Normalize()
	require.NoError(t, err)
	assert.Equal(t, "Nguyễn Văn A", got.Name)
	assert.Equal(t, domain.LocaleVI, got.Locale, "the locale defaults to Vietnamese")

	least := domain.Input{Name: "A", Phone: "+84 938-038-621", Consent: true, Locale: domain.LocaleEN}
	got, err = least.Normalize()
	require.NoError(t, err)
	assert.Equal(t, domain.LocaleEN, got.Locale)
}

func TestNormalizeRefuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*domain.Input)
		field  string
	}{
		{"no consent", func(in *domain.Input) { in.Consent = false }, "consent"},
		{"no name", func(in *domain.Input) { in.Name = "  " }, "name"},
		{"name over a line", func(in *domain.Input) { in.Name = "A\r\nBcc: x@example.com" }, "name"},
		{"long name", func(in *domain.Input) { in.Name = strings.Repeat("a", 121) }, "name"},
		{"garage with a newline", func(in *domain.Input) { in.GarageName = "a\nb" }, "garageName"},
		{"no phone", func(in *domain.Input) { in.Phone = "" }, "phone"},
		{"letters in phone", func(in *domain.Input) { in.Phone = "09380abc621" }, "phone"},
		{"short phone", func(in *domain.Input) { in.Phone = "12345" }, "phone"},
		{"bad e-mail", func(in *domain.Input) { in.Email = "not-an-email" }, "email"},
		{"e-mail with a name", func(in *domain.Input) { in.Email = "A <a@example.com>" }, "email"},
		{"long message", func(in *domain.Input) { in.Message = strings.Repeat("a", 2001) }, "message"},
		{"control character in message", func(in *domain.Input) { in.Message = "a\x00b" }, "message"},
		{"unknown locale", func(in *domain.Input) { in.Locale = "fr" }, "locale"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := valid()
			tt.change(&in)

			_, err := in.Normalize()
			require.Error(t, err)

			appErr, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, apperror.CodeInvalidArgument, appErr.Code)
			require.NotEmpty(t, appErr.Details)
			assert.Equal(t, tt.field, appErr.Details[0].Field)
		})
	}
}

func TestNormalizeCountsCharactersNotBytes(t *testing.T) {
	t.Parallel()

	in := valid()
	in.Name = strings.Repeat("ệ", 120)
	_, err := in.Normalize()
	assert.NoError(t, err, "120 Vietnamese characters fit although they take more bytes")
}
