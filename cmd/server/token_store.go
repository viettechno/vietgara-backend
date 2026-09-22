package main

import (
	"context"
	"errors"
	"sync"
)

// inMemoryRefreshTokenStore is an ephemeral RefreshTokenStore.
// TODO(Phase 1 hardening, before staging): replace with a Redis
// (Memorystore) implementation -- see Security Architecture Section 2.2.
// An in-memory store does not survive a process restart and does not work
// across multiple replicas, which is only acceptable for local
// development.
type inMemoryRefreshTokenStore struct {
	mu     sync.Mutex
	tokens map[string]string // refreshToken -> accountID
}

func newInMemoryRefreshTokenStore() *inMemoryRefreshTokenStore {
	return &inMemoryRefreshTokenStore{tokens: map[string]string{}}
}

func (s *inMemoryRefreshTokenStore) Store(_ context.Context, refreshToken, accountID string, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tokens[refreshToken] = accountID

	return nil
}

func (s *inMemoryRefreshTokenStore) GetAccountID(_ context.Context, refreshToken string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	accountID, ok := s.tokens[refreshToken]
	if !ok {
		return "", errors.New("refresh token not found or revoked")
	}

	return accountID, nil
}

func (s *inMemoryRefreshTokenStore) Revoke(_ context.Context, refreshToken string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.tokens, refreshToken)

	return nil
}
