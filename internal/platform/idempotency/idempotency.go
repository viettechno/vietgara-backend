// Package idempotency stores the responses of requests sent with an
// Idempotency-Key header (API Specification Section 1.1), so a client that
// retries a settlement or payment creation gets the first response back
// instead of creating it twice. See middleware.Idempotency for the flow.
//
// Records live in idempotency_keys, under the garage's
// RLS, and are written in the request's own transaction: a key, and the
// change made with it, commit or roll back together.
package idempotency

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// Retention is how long a key is remembered; any client retry happens
// well within it.
const Retention = 24 * time.Hour

// PurgeInterval is how often expired keys are deleted.
const PurgeInterval = time.Hour

var errExpiredKeyRead = errors.New("idempotency key expired while being read; retry")

// Key identifies one request: the caller's key, scoped to the garage and
// the calling user.
type Key struct {
	GarageID string
	UserID   string
	Key      string
}

// Record is a stored request and, once it succeeded, its response.
type Record struct {
	Method      string
	RequestHash []byte
	// ResponseType is the full name of the response message.
	ResponseType string
	Response     []byte
	// HTTPStatus is the status the handler asked for; 0 means the default.
	HTTPStatus int
}

// Store persists records in idempotency_keys.
type Store struct {
	pool *platformdb.Pool
	now  func() time.Time
}

// NewStore returns a Store on pool.
func NewStore(pool *platformdb.Pool) *Store {
	return &Store{pool: pool, now: time.Now}
}

// Claim reserves k for a new request with method and requestHash, in the
// transaction carried in ctx. It returns (nil, nil) when k is new: the
// caller runs the request and then calls Complete. Otherwise it returns
// the record already stored under k -- after waiting for a concurrent
// request with the same key to commit.
func (s *Store) Claim(ctx context.Context, k Key, method string, requestHash []byte) (*Record, error) {
	q := platformdb.QuerierFromContext(ctx, s.pool)

	tag, err := q.Exec(ctx, `
		INSERT INTO idempotency_keys (garage_id, user_id, idempotency_key, method, request_hash)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (garage_id, user_id, idempotency_key) DO NOTHING`,
		k.GarageID, k.UserID, k.Key, method, requestHash)
	if err != nil {
		return nil, fmt.Errorf("claim idempotency key: %w", err)
	}

	if tag.RowsAffected() == 1 {
		return nil, nil //nolint:nilnil // a new key has no stored record
	}

	var (
		rec          Record
		responseType *string
		status       *int
	)

	err = q.QueryRow(ctx, `
		SELECT method, request_hash, response_type, response, http_status
		FROM idempotency_keys
		WHERE garage_id = $1 AND user_id = $2 AND idempotency_key = $3`,
		k.GarageID, k.UserID, k.Key,
	).Scan(&rec.Method, &rec.RequestHash, &responseType, &rec.Response, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		// Deleted by the purge between the two statements: expired anyway.
		return nil, errExpiredKeyRead
	}

	if err != nil {
		return nil, fmt.Errorf("read idempotency key: %w", err)
	}

	if responseType != nil {
		rec.ResponseType = *responseType
	}

	if status != nil {
		rec.HTTPStatus = *status
	}

	return &rec, nil
}

// Complete stores the response of the request claimed under k.
func (s *Store) Complete(ctx context.Context, k Key, rec Record) error {
	q := platformdb.QuerierFromContext(ctx, s.pool)

	var status *int
	if rec.HTTPStatus != 0 {
		status = &rec.HTTPStatus
	}

	_, err := q.Exec(ctx, `
		UPDATE idempotency_keys SET response_type = $4, response = $5, http_status = $6
		WHERE garage_id = $1 AND user_id = $2 AND idempotency_key = $3`,
		k.GarageID, k.UserID, k.Key, rec.ResponseType, rec.Response, status)
	if err != nil {
		return fmt.Errorf("store idempotent response: %w", err)
	}

	return nil
}

// Purge deletes the keys older than Retention, across every garage, and
// returns how many it deleted.
func (s *Store) Purge(ctx context.Context) (int64, error) {
	var deleted int64

	err := platformdb.QuerierFromContext(ctx, s.pool).
		QueryRow(ctx, `SELECT purge_idempotency_keys($1)`, s.now().Add(-Retention)).Scan(&deleted)
	if err != nil {
		return 0, fmt.Errorf("purge idempotency keys: %w", err)
	}

	return deleted, nil
}
