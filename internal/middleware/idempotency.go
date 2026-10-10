package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/internal/platform/idempotency"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Idempotency error reasons (API Specification Section 1.2).
const (
	ReasonIdempotencyKeyRequired = "IDEMPOTENCY_KEY_REQUIRED"
	ReasonIdempotencyKeyReused   = "IDEMPOTENCY_KEY_REUSED"
)

// IdempotencyKeyHeader is the request header (gRPC metadata key) carrying
// the client's key.
const IdempotencyKeyHeader = "idempotency-key"

const maxIdempotencyKeyLength = 255

// IdempotencyStore keeps the requests made with a key and their responses,
// in the request's transaction (idempotency.Store).
type IdempotencyStore interface {
	Claim(ctx context.Context, k idempotency.Key, method string, requestHash []byte) (*idempotency.Record, error)
	Complete(ctx context.Context, k idempotency.Key, rec idempotency.Record) error
}

// Idempotency makes the RPCs whose policy is Idempotent safe to retry
// (API Specification Section 1.1). It runs after Garage, inside the
// request's transaction:
//
//   - without a valid Idempotency-Key the request is refused
//     (IDEMPOTENCY_KEY_REQUIRED);
//   - the first request with a key runs, and its response is stored with
//     the key in the same transaction, so both commit or neither does;
//   - a later request with the same key and the same body gets the stored
//     response and status back without running again; a concurrent one
//     waits for the first to commit, then does the same;
//   - the same key with another body or method is refused
//     (IDEMPOTENCY_KEY_REUSED);
//   - a request that fails stores nothing, so it can be retried as is.
//
// Keys are scoped to the garage and the calling user, and kept for
// idempotency.Retention.
func Idempotency(store IdempotencyStore) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !policyFromContext(ctx).Idempotent {
			return handler(ctx, req)
		}

		key, err := idempotencyKey(ctx)
		if err != nil {
			return nil, apperror.ToGRPCError(err)
		}

		msg, ok := req.(proto.Message)
		if !ok {
			return nil, apperror.ToGRPCError(apperror.New(apperror.CodeInternal, "idempotent request is not a proto message"))
		}

		hash, err := requestHash(msg)
		if err != nil {
			return nil, apperror.ToGRPCError(apperror.Internal("hash idempotent request", err))
		}

		// Keys live under the garage's RLS: only garage-scoped RPCs, which
		// Garage has validated and runs in its transaction, are idempotent.
		scoped, ok := req.(GarageScoped)
		if !ok {
			return nil, apperror.ToGRPCError(apperror.New(apperror.CodeInternal, "idempotent RPC is not garage-scoped"))
		}

		k := idempotency.Key{GarageID: scoped.GetGarageId(), UserID: UserID(ctx), Key: key}

		stored, err := store.Claim(ctx, k, info.FullMethod, hash)
		if err != nil {
			return nil, apperror.ToGRPCError(apperror.Internal("claim idempotency key", err))
		}

		if stored != nil {
			resp, err := replay(ctx, stored, info.FullMethod, hash)
			if err != nil {
				return nil, apperror.ToGRPCError(err)
			}

			return resp, nil
		}

		recorder := &statusRecorder{ServerTransportStream: grpc.ServerTransportStreamFromContext(ctx)}

		resp, err := handler(grpc.NewContextWithServerTransportStream(ctx, recorder), req)
		if err != nil {
			return nil, err
		}

		if err := complete(ctx, store, k, resp, recorder.status); err != nil {
			return nil, apperror.ToGRPCError(err)
		}

		return resp, nil
	}
}

func idempotencyKey(ctx context.Context) (string, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	values := md.Get(IdempotencyKeyHeader)
	if len(values) == 0 || values[0] == "" {
		return "", apperror.New(apperror.CodeInvalidArgument, "this request needs an Idempotency-Key header").
			WithReason(ReasonIdempotencyKeyRequired).WithField("Idempotency-Key", apperror.IssueRequired)
	}

	key := values[0]
	if len(key) > maxIdempotencyKeyLength || !printableASCII(key) {
		return "", apperror.New(apperror.CodeInvalidArgument, "the Idempotency-Key must be 1 to 255 printable ASCII characters").
			WithReason(ReasonIdempotencyKeyRequired).WithField("Idempotency-Key", apperror.IssueInvalid)
	}

	return key, nil
}

func printableASCII(s string) bool {
	for i := range len(s) {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}

	return true
}

func requestHash(msg proto.Message) ([]byte, error) {
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg)
	if err != nil {
		return nil, err
	}

	sum := sha256.Sum256(raw)

	return sum[:], nil
}

func replay(ctx context.Context, stored *idempotency.Record, method string, hash []byte) (proto.Message, error) {
	if stored.Method != method || !bytes.Equal(stored.RequestHash, hash) {
		return nil, apperror.New(apperror.CodeInvalidArgument, "this Idempotency-Key was already used for a different request").
			WithReason(ReasonIdempotencyKeyReused).WithField("Idempotency-Key", apperror.IssueInvalid)
	}

	if stored.ResponseType == "" {
		// Claim and Complete share one transaction, so a committed key
		// always has its response.
		return nil, apperror.New(apperror.CodeInternal, "idempotency key has no stored response")
	}

	mt, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(stored.ResponseType))
	if err != nil {
		return nil, apperror.Internal("find idempotent response type", err)
	}

	resp := mt.New().Interface()
	if err := proto.Unmarshal(stored.Response, resp); err != nil {
		return nil, apperror.Internal("decode idempotent response", err)
	}

	if stored.HTTPStatus != 0 {
		_ = grpc.SetHeader(ctx, metadata.Pairs(httpapi.HTTPCodeHeader, strconv.Itoa(stored.HTTPStatus)))
	}

	return resp, nil
}

func complete(ctx context.Context, store IdempotencyStore, k idempotency.Key, resp any, status int) error {
	msg, ok := resp.(proto.Message)
	if !ok {
		return apperror.New(apperror.CodeInternal, "idempotent response is not a proto message")
	}

	raw, err := proto.Marshal(msg)
	if err != nil {
		return apperror.Internal("encode idempotent response", err)
	}

	rec := idempotency.Record{ResponseType: string(msg.ProtoReflect().Descriptor().FullName()), Response: raw, HTTPStatus: status}
	if err := store.Complete(ctx, k, rec); err != nil {
		return apperror.Internal("store idempotent response", err)
	}

	return nil
}

// statusRecorder notes the HTTP status a handler asks for (httpapi.Created)
// so a replay answers with the same one.
type statusRecorder struct {
	grpc.ServerTransportStream

	status int
}

func (r *statusRecorder) SetHeader(md metadata.MD) error {
	if values := md.Get(httpapi.HTTPCodeHeader); len(values) > 0 {
		if code, err := strconv.Atoi(values[0]); err == nil {
			r.status = code
		}
	}

	if r.ServerTransportStream == nil {
		return nil
	}

	return r.ServerTransportStream.SetHeader(md)
}

func (r *statusRecorder) Method() string {
	if r.ServerTransportStream == nil {
		return ""
	}

	return r.ServerTransportStream.Method()
}

func (r *statusRecorder) SendHeader(md metadata.MD) error {
	if r.ServerTransportStream == nil {
		return nil
	}

	return r.ServerTransportStream.SendHeader(md)
}

func (r *statusRecorder) SetTrailer(md metadata.MD) error {
	if r.ServerTransportStream == nil {
		return nil
	}

	return r.ServerTransportStream.SetTrailer(md)
}
