package middleware_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/internal/platform/idempotency"
)

const (
	methodCreate = "/test.v1.Svc/CreateThing"
	methodPay    = "/test.v1.Svc/PayThing"
	userID       = "22222222-2222-4222-8222-222222222222"
)

// memStore is an in-memory IdempotencyStore; the real one is covered by
// the idempotency package's integration test.
type memStore struct {
	records   map[idempotency.Key]*idempotency.Record
	claimErr  error
	completed int
}

func newMemStore() *memStore { return &memStore{records: map[idempotency.Key]*idempotency.Record{}} }

func (m *memStore) Claim(_ context.Context, k idempotency.Key, method string, hash []byte) (*idempotency.Record, error) {
	if m.claimErr != nil {
		return nil, m.claimErr
	}

	if rec, ok := m.records[k]; ok {
		cp := *rec

		return &cp, nil
	}

	m.records[k] = &idempotency.Record{Method: method, RequestHash: hash}

	return nil, nil //nolint:nilnil // a new key, as the real store answers
}

func (m *memStore) Complete(_ context.Context, k idempotency.Key, rec idempotency.Record) error {
	stored := m.records[k]
	stored.ResponseType, stored.Response, stored.HTTPStatus = rec.ResponseType, rec.Response, rec.HTTPStatus
	m.completed++

	return nil
}

// headerStream captures the headers an RPC sets, as the gRPC server would.
type headerStream struct {
	grpc.ServerTransportStream

	header metadata.MD
}

func (h *headerStream) SetHeader(md metadata.MD) error {
	h.header = metadata.Join(h.header, md)

	return nil
}

type pipeline struct {
	store *memStore
	runs  int
	fail  error
}

// call sends req to method through Auth and Idempotency, with key unless
// empty, and returns the response and the HTTP status header set.
func (p *pipeline) call(t *testing.T, method, key string, req proto.Message) (any, string, error) {
	t.Helper()

	rule := access.Requires(access.BillingWrite)
	policies := middleware.Policies{
		methodCreate: middleware.IdempotentGarageMethod(rule),
		methodPay:    middleware.IdempotentGarageMethod(rule),
		methodGarage: middleware.GarageMethod(rule),
	}

	pairs := []string{"authorization", "Bearer " + token(t, auth.Identity{UserID: userID, EmailVerified: true})}
	if key != "" {
		pairs = append(pairs, "idempotency-key", key)
	}

	stream := &headerStream{}
	ctx := grpc.NewContextWithServerTransportStream(metadata.NewIncomingContext(t.Context(), metadata.Pairs(pairs...)), stream)

	handler := func(ctx context.Context, _ any) (any, error) {
		p.runs++
		if p.fail != nil {
			return nil, p.fail
		}

		httpapi.Created(ctx)

		return &billingv1.CreateSettlementResponse{Settlement: &billingv1.Settlement{Id: "s-" + strings.Repeat("1", p.runs)}}, nil
	}

	chain := middleware.ChainUnaryInterceptors(middleware.Auth(issuer, policies), middleware.Idempotency(p.store))
	resp, err := chain(ctx, req, info(method), handler)

	return resp, strings.Join(stream.header.Get(httpapi.HTTPCodeHeader), ","), err
}

func createReq(repairOrderID string) *billingv1.CreateSettlementRequest {
	return &billingv1.CreateSettlementRequest{GarageId: garageID, RepairOrderId: repairOrderID}
}

func TestIdempotency_ReplaysTheFirstResponse(t *testing.T) {
	t.Parallel()

	p := &pipeline{store: newMemStore()}

	first, firstStatus, err := p.call(t, methodCreate, "key-1", createReq("ro-1"))
	require.NoError(t, err)
	assert.Equal(t, "201", firstStatus)

	again, againStatus, err := p.call(t, methodCreate, "key-1", createReq("ro-1"))
	require.NoError(t, err)
	assert.Equal(t, 1, p.runs, "a repeated key does not run the RPC again")

	firstMsg, ok := first.(proto.Message)
	require.True(t, ok)
	againMsg, ok := again.(proto.Message)
	require.True(t, ok)
	assert.True(t, proto.Equal(firstMsg, againMsg), "the stored response comes back")
	assert.Equal(t, "201", againStatus, "with the status the RPC chose")

	_, _, err = p.call(t, methodCreate, "key-2", createReq("ro-1"))
	require.NoError(t, err)
	assert.Equal(t, 2, p.runs, "a new key is a new request")
}

func TestIdempotency_RefusesReuseForAnotherRequest(t *testing.T) {
	t.Parallel()

	p := &pipeline{store: newMemStore()}

	_, _, err := p.call(t, methodCreate, "key-1", createReq("ro-1"))
	require.NoError(t, err)

	_, _, err = p.call(t, methodCreate, "key-1", createReq("ro-2"))
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, middleware.ReasonIdempotencyKeyReused, reasonOf(err), "another body")

	_, _, err = p.call(t, methodPay, "key-1", createReq("ro-1"))
	assert.Equal(t, middleware.ReasonIdempotencyKeyReused, reasonOf(err), "another RPC")
	assert.Equal(t, 1, p.runs)
}

func TestIdempotency_RequiresAValidKey(t *testing.T) {
	t.Parallel()

	for name, key := range map[string]string{
		"missing":   "",
		"space":     "has space",
		"non-ascii": "khóa",
		"too long":  strings.Repeat("k", 256),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := &pipeline{store: newMemStore()}

			_, _, err := p.call(t, methodCreate, key, createReq("ro-1"))
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
			assert.Equal(t, middleware.ReasonIdempotencyKeyRequired, reasonOf(err))
			assert.Zero(t, p.runs)
		})
	}
}

func TestIdempotency_FailureStoresNoResponse(t *testing.T) {
	t.Parallel()

	p := &pipeline{store: newMemStore(), fail: status.Error(codes.FailedPrecondition, "repair order not closed")}

	_, _, err := p.call(t, methodCreate, "key-1", createReq("ro-1"))
	assert.Equal(t, codes.FailedPrecondition, status.Code(err), "the RPC's own error is returned unchanged")
	assert.Zero(t, p.store.completed, "nothing is stored; the transaction rolls the key back")
}

func TestIdempotency_OnlyIdempotentPolicies(t *testing.T) {
	t.Parallel()

	p := &pipeline{store: newMemStore()}

	_, _, err := p.call(t, methodGarage, "", createReq("ro-1"))
	require.NoError(t, err, "other RPCs need no key")
	assert.Empty(t, p.store.records)
}

func TestIdempotency_StoreFailure(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	store.claimErr = errors.New("db down")
	p := &pipeline{store: store}

	_, _, err := p.call(t, methodCreate, "key-1", createReq("ro-1"))
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Zero(t, p.runs)
}

func TestIdempotency_NotGarageScoped(t *testing.T) {
	t.Parallel()

	p := &pipeline{store: newMemStore()}

	_, _, err := p.call(t, methodCreate, "key-1", &billingv1.ListSettlementsResponse{})
	assert.Equal(t, codes.Internal, status.Code(err), "a misconfigured policy fails closed")
	assert.Zero(t, p.runs)
}
