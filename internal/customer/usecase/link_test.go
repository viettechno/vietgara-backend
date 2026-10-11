package usecase_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const userID = "user-binh"

// linkStore is an in-memory LinkRepository that enforces one current link
// per customer like the unique index does.
type linkStore struct {
	links     []*domain.UserLink
	updateErr error
}

func (s *linkStore) Create(_ context.Context, l *domain.UserLink) error {
	for _, e := range s.links {
		if e.CustomerID == l.CustomerID && e.Status.Open() && l.Status.Open() {
			return domain.ErrAlreadyLinked
		}
	}

	s.links = append(s.links, l)

	return nil
}

func (s *linkStore) FindCurrent(_ context.Context, garage, customer string) (*domain.UserLink, error) {
	for _, l := range s.links {
		if l.GarageID == garage && l.CustomerID == customer && l.Status.Open() {
			return l, nil
		}
	}

	return nil, domain.ErrLinkNotFound
}

func (s *linkStore) FindForUser(_ context.Context, user, id string) (*domain.UserLink, error) {
	for _, l := range s.links {
		if l.ID == id && l.UserID == user {
			return l, nil
		}
	}

	return nil, domain.ErrLinkNotFound
}

func (s *linkStore) ListOpenForUser(_ context.Context, user string) ([]*domain.UserLink, error) {
	var out []*domain.UserLink

	for _, l := range s.links {
		if l.UserID == user && l.Status.Open() {
			out = append(out, l)
		}
	}

	return out, nil
}

func (s *linkStore) HasAny(_ context.Context, customer, user string) (bool, error) {
	for _, l := range s.links {
		if l.CustomerID == customer && l.UserID == user {
			return true, nil
		}
	}

	return false, nil
}

func (s *linkStore) Update(context.Context, *domain.UserLink) error { return s.updateErr }

type directory struct {
	byEmail map[string]string
	err     error
}

func (d directory) FindVerifiedByEmail(_ context.Context, email string) (string, bool, error) {
	id, ok := d.byEmail[email]

	return id, ok, d.err
}

type codes struct {
	sent    []string
	sendErr error
	valid   string
	checked string
}

func (c *codes) Send(_ context.Context, user, link, garage string) error {
	c.sent = append(c.sent, user+"/"+link+"/"+garage)

	return c.sendErr
}

func (c *codes) Check(_ context.Context, link, code string) error {
	c.checked = link

	if code != c.valid {
		return apperror.New(apperror.CodeInvalidArgument, "wrong code")
	}

	return nil
}

type garageNames struct{ err error }

func (g garageNames) Name(context.Context, string) (string, error) { return "Gara Zeta", g.err }

type auditor struct {
	entries []audit.Entry
	err     error
}

func (a *auditor) Record(_ context.Context, e audit.Entry) error {
	a.entries = append(a.entries, e)

	return a.err
}

type linkEnv struct {
	uc        *usecase.LinkUseCase
	store     *linkStore
	codes     *codes
	audit     *auditor
	customers *mocks.MockCustomerRepository
}

func newLinkEnv(t *testing.T, email *string) *linkEnv {
	t.Helper()

	e := &linkEnv{
		store: &linkStore{}, codes: &codes{valid: "123456"}, audit: &auditor{},
		customers: mocks.NewMockCustomerRepository(t),
	}
	e.customers.EXPECT().FindByID(mock.Anything, garageID, customerID).
		Return(&domain.Customer{ID: customerID, GarageID: garageID, FullName: "Tran Binh", Email: email}, nil).Maybe()
	e.uc = usecase.NewLinkUseCase(usecase.LinkDeps{
		Links: e.store, Customers: e.customers, Users: directory{byEmail: map[string]string{"binh@example.com": userID}},
		Codes: e.codes, Garages: garageNames{}, Audit: e.audit,
	}, clock())

	return e
}

func TestLink_RequestThenConfirmWithCode(t *testing.T) {
	t.Parallel()

	e := newLinkEnv(t, new("binh@example.com"))

	link, err := e.uc.Request(t.Context(), garageID, customerID, actorID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkRequested, link.Status)
	assert.Equal(t, []string{userID + "/" + link.ID + "/Gara Zeta"}, e.codes.sent)
	require.Len(t, e.audit.entries, 1)
	assert.Equal(t, usecase.ActionLinkRequested, e.audit.entries[0].Action)
	assert.NotContains(t, e.audit.entries[0].After, "user_id", "the audit log never names the user")

	_, err = e.uc.Confirm(t.Context(), garageID, customerID, actorID, "000000")
	assertAppError(t, err, apperror.CodeInvalidArgument)

	confirmed, err := e.uc.Confirm(t.Context(), garageID, customerID, actorID, " 123456 ")
	require.NoError(t, err)
	assert.Equal(t, domain.LinkLinked, confirmed.Status)
	assert.Equal(t, domain.ChannelOTP, confirmed.Channel)
	assert.Equal(t, link.ID, e.codes.checked)
	require.NotNil(t, confirmed.LinkedAt)
	assert.Equal(t, fixedNow, *confirmed.LinkedAt)

	_, err = e.uc.Request(t.Context(), garageID, customerID, actorID)
	assertAppError(t, err, apperror.CodeAlreadyExists)

	ended, err := e.uc.Unlink(t.Context(), garageID, customerID, actorID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkUnlinked, ended.Status)

	_, err = e.uc.Unlink(t.Context(), garageID, customerID, actorID)
	assertAppError(t, err, apperror.CodeNotFound)
}

func TestLink_RequestPreconditions(t *testing.T) {
	t.Parallel()

	t.Run("a customer without e-mail has nobody to ask", func(t *testing.T) {
		t.Parallel()

		_, err := newLinkEnv(t, nil).uc.Request(t.Context(), garageID, customerID, actorID)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("no verified user owns the e-mail", func(t *testing.T) {
		t.Parallel()

		_, err := newLinkEnv(t, new("nobody@example.com")).uc.Request(t.Context(), garageID, customerID, actorID)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("the directory fails", func(t *testing.T) {
		t.Parallel()

		e := newLinkEnv(t, new("binh@example.com"))
		e.uc.Users = directory{err: errBoom}

		_, err := e.uc.Request(t.Context(), garageID, customerID, actorID)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("a code cannot be confirmed without a request", func(t *testing.T) {
		t.Parallel()

		_, err := newLinkEnv(t, new("binh@example.com")).uc.Confirm(t.Context(), garageID, customerID, actorID, "123456")
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})
}

func TestLink_RequestAgain(t *testing.T) {
	t.Parallel()

	e := newLinkEnv(t, new("binh@example.com"))

	first, err := e.uc.Request(t.Context(), garageID, customerID, actorID)
	require.NoError(t, err)

	again, err := e.uc.Request(t.Context(), garageID, customerID, actorID)
	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID, "the waiting request is kept")
	assert.Len(t, e.codes.sent, 2, "and the code is sent again")

	// The e-mail now belongs to another user: the old request ends.
	e.uc.Users = directory{byEmail: map[string]string{"binh@example.com": "user-other"}}

	other, err := e.uc.Request(t.Context(), garageID, customerID, actorID)
	require.NoError(t, err)
	assert.NotEqual(t, first.ID, other.ID)
	assert.Equal(t, domain.LinkUnlinked, first.Status)

	// A failed send is reported.
	e.codes.sendErr = apperror.New(apperror.CodeResourceExhausted, "too soon")
	_, err = e.uc.Request(t.Context(), garageID, customerID, actorID)
	assertAppError(t, err, apperror.CodeResourceExhausted)
}

func TestLink_RequestFailures(t *testing.T) {
	t.Parallel()

	e := newLinkEnv(t, new("binh@example.com"))
	e.codes.sendErr = errBoom

	_, err := e.uc.Request(t.Context(), garageID, customerID, actorID)
	assertAppError(t, err, apperror.CodeInternal)

	e = newLinkEnv(t, new("binh@example.com"))
	e.uc.Garages = garageNames{err: errBoom}

	_, err = e.uc.Request(t.Context(), garageID, customerID, actorID)
	assertAppError(t, err, apperror.CodeInternal)

	e = newLinkEnv(t, new("binh@example.com"))
	e.audit.err = errBoom

	_, err = e.uc.Request(t.Context(), garageID, customerID, actorID)
	assertAppError(t, err, apperror.CodeInternal)

	e = newLinkEnv(t, new("binh@example.com"))
	e.store.updateErr = errBoom

	_, err = e.uc.Request(t.Context(), garageID, customerID, actorID)
	require.NoError(t, err)

	_, err = e.uc.Confirm(t.Context(), garageID, customerID, actorID, "123456")
	assertAppError(t, err, apperror.CodeInternal)

	_, err = e.uc.Unlink(t.Context(), garageID, customerID, actorID)
	assertAppError(t, err, apperror.CodeInternal)
}

func TestLink_UserAnswers(t *testing.T) {
	t.Parallel()

	e := newLinkEnv(t, new("binh@example.com"))
	link, err := e.uc.Request(t.Context(), garageID, customerID, actorID)
	require.NoError(t, err)

	mine, err := e.uc.ListForUser(t.Context(), userID)
	require.NoError(t, err)
	require.Len(t, mine, 1)

	found, err := e.uc.FindForUser(t.Context(), userID, link.ID)
	require.NoError(t, err)
	assert.Equal(t, link.ID, found.ID)

	_, err = e.uc.FindForUser(t.Context(), "someone-else", link.ID)
	assertAppError(t, err, apperror.CodeNotFound)

	_, err = e.uc.UnlinkByUser(t.Context(), userID, link.ID)
	assertAppError(t, err, apperror.CodeFailedPrecondition) // not linked yet

	accepted, err := e.uc.Accept(t.Context(), userID, link.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChannelApp, accepted.Channel)

	_, err = e.uc.Accept(t.Context(), userID, link.ID)
	assertAppError(t, err, apperror.CodeFailedPrecondition) // already answered

	_, err = e.uc.Decline(t.Context(), userID, link.ID)
	assertAppError(t, err, apperror.CodeFailedPrecondition)

	ended, err := e.uc.UnlinkByUser(t.Context(), userID, link.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkUnlinked, ended.Status)

	_, err = e.uc.UnlinkByUser(t.Context(), userID, "missing")
	assertAppError(t, err, apperror.CodeNotFound)
}

func TestLink_Decline(t *testing.T) {
	t.Parallel()

	e := newLinkEnv(t, new("binh@example.com"))
	link, err := e.uc.Request(t.Context(), garageID, customerID, actorID)
	require.NoError(t, err)

	declined, err := e.uc.Decline(t.Context(), userID, link.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkDeclined, declined.Status)
	assert.NotNil(t, declined.EndedAt)

	// A declined profile is not linked again by the e-mail match.
	require.NoError(t, e.uc.AutoLink(t.Context(), userID, garageID, customerID))

	current, err := e.store.FindCurrent(t.Context(), garageID, customerID)
	require.ErrorIs(t, err, domain.ErrLinkNotFound)
	assert.Nil(t, current)
}

func TestLink_AutoLink(t *testing.T) {
	t.Parallel()

	e := newLinkEnv(t, new("binh@example.com"))

	require.NoError(t, e.uc.AutoLink(t.Context(), userID, garageID, customerID))

	link, err := e.store.FindCurrent(t.Context(), garageID, customerID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkLinked, link.Status)
	assert.Equal(t, domain.ChannelAutoEmail, link.Channel)

	// Once is enough, and a profile linked to someone else stays theirs.
	require.NoError(t, e.uc.AutoLink(t.Context(), userID, garageID, customerID))
	require.NoError(t, e.uc.AutoLink(t.Context(), "user-other", garageID, customerID))
	assert.Len(t, e.store.links, 1)
}

func TestLink_AutoLinkFailure(t *testing.T) {
	t.Parallel()

	e := newLinkEnv(t, new("binh@example.com"))
	e.uc.Links = failingLinks{linkStore: &linkStore{}}

	err := e.uc.AutoLink(t.Context(), userID, garageID, customerID)
	assertAppError(t, err, apperror.CodeInternal)
}

// failingLinks fails every write after a clean HasAny.
type failingLinks struct{ *linkStore }

func (failingLinks) HasAny(context.Context, string, string) (bool, error) { return false, nil }

func (failingLinks) Create(context.Context, *domain.UserLink) error { return errBoom }
