// Package numbering hands out the next document sequence number for a
// garage, document type and period (see pkg/numbering for rendering that
// number into a garage's numbering template). It is platform
// infrastructure, like the outbox: Quote & Pricing (Release 1.2) and,
// later, Repair Order (Release 1.3) both depend on it without depending on
// each other.
package numbering

import (
	"context"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// Sequencer hands out document sequence numbers from the document_sequences
// table.
type Sequencer struct {
	pool *platformdb.Pool
}

// NewSequencer returns a Sequencer backed by pool.
func NewSequencer(pool *platformdb.Pool) *Sequencer {
	return &Sequencer{pool: pool}
}

// Next atomically reserves and returns the next sequence number for
// (garageID, docType, period), starting at 1. It runs on the querier
// carried in ctx, so the reservation is part of the caller's transaction
// and rolls back with it (no gap is left on failure).
func (s *Sequencer) Next(ctx context.Context, garageID, docType, period string) (int, error) {
	q := platformdb.QuerierFromContext(ctx, s.pool)

	var seq int

	err := q.QueryRow(ctx, `
		INSERT INTO document_sequences (garage_id, doc_type, period, next_seq)
		VALUES ($1, $2, $3, 1)
		ON CONFLICT (garage_id, doc_type, period)
		DO UPDATE SET next_seq = document_sequences.next_seq + 1
		RETURNING next_seq`,
		garageID, docType, period,
	).Scan(&seq)

	return seq, err
}
