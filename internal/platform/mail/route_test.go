package mail_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/mail"
)

type recordingSender struct {
	got []string
	err error
}

func (r *recordingSender) Send(_ context.Context, msg mail.Message) error {
	r.got = append(r.got, msg.ToEmail)

	return r.err
}

func TestRoute(t *testing.T) {
	t.Parallel()

	testInbox, provider := &recordingSender{}, &recordingSender{err: errors.New("provider down")}
	sender := mail.Route(func(email string) bool { return strings.HasSuffix(email, "@blackbox.vietgara.test") }, testInbox, provider)

	toTest, toReal := validMessage(), validMessage()
	toTest.ToEmail, toReal.ToEmail = "bb.owner@blackbox.vietgara.test", "owner@example.com"

	require.NoError(t, sender.Send(t.Context(), toTest))
	require.EqualError(t, sender.Send(t.Context(), toReal), "provider down", "the other sender's error is returned as is")

	assert.Equal(t, []string{"bb.owner@blackbox.vietgara.test"}, testInbox.got)
	assert.Equal(t, []string{"owner@example.com"}, provider.got)
}
