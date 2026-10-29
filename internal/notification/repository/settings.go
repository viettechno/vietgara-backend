package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// PostgresSettings implements usecase.SettingsRepository. The table is
// garage-scoped by row-level security.
type PostgresSettings struct {
	pool *platformdb.Pool
}

// NewPostgresSettings returns a PostgresSettings on pool.
func NewPostgresSettings(pool *platformdb.Pool) *PostgresSettings {
	return &PostgresSettings{pool: pool}
}

// Get returns the garage's settings, the defaults when it has none.
func (s *PostgresSettings) Get(ctx context.Context, garageID string) (domain.Settings, error) {
	var (
		name  *string
		types []byte
	)

	err := platformdb.QuerierFromContext(ctx, s.pool).QueryRow(ctx,
		`SELECT sender_name, types FROM garage_notification_settings WHERE garage_id = $1`, garageID).Scan(&name, &types)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Settings{}, nil
	}

	if err != nil {
		return domain.Settings{}, fmt.Errorf("read notification settings: %w", err)
	}

	var flags map[string]bool
	if err := json.Unmarshal(types, &flags); err != nil {
		return domain.Settings{}, fmt.Errorf("decode notification settings: %w", err)
	}

	settings := domain.Settings{Off: map[domain.Type]bool{}}
	if name != nil {
		settings.SenderName = *name
	}

	for t, on := range flags {
		if !on {
			settings.Off[domain.Type(t)] = true
		}
	}

	return settings, nil
}

// Save inserts or replaces the garage's settings.
func (s *PostgresSettings) Save(ctx context.Context, garageID string, settings domain.Settings) error {
	flags := map[string]bool{}
	for t := range settings.Off {
		flags[string(t)] = false
	}

	types, err := json.Marshal(flags)
	if err != nil {
		return fmt.Errorf("encode notification settings: %w", err)
	}

	_, err = platformdb.QuerierFromContext(ctx, s.pool).Exec(ctx, `
		INSERT INTO garage_notification_settings (garage_id, sender_name, types)
		VALUES ($1, $2, $3)
		ON CONFLICT (garage_id) DO UPDATE SET sender_name = EXCLUDED.sender_name, types = EXCLUDED.types`,
		garageID, nullableString(settings.SenderName), types)
	if err != nil {
		return fmt.Errorf("save notification settings: %w", err)
	}

	return nil
}

// PostgresSuppressions implements usecase.Suppressions. The table holds
// addresses across garages and has no row-level security.
type PostgresSuppressions struct {
	pool *platformdb.Pool
}

// NewPostgresSuppressions returns a PostgresSuppressions on pool.
func NewPostgresSuppressions(pool *platformdb.Pool) *PostgresSuppressions {
	return &PostgresSuppressions{pool: pool}
}

// Suppressed reports whether the address bounced for good or complained.
func (s *PostgresSuppressions) Suppressed(ctx context.Context, email string) (bool, error) {
	var found bool

	err := platformdb.QuerierFromContext(ctx, s.pool).QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM email_suppressions WHERE email = lower($1))`, email).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("read email suppression: %w", err)
	}

	return found, nil
}

// Suppress records the address; the first reason is kept.
func (s *PostgresSuppressions) Suppress(ctx context.Context, email, reason string) error {
	_, err := platformdb.QuerierFromContext(ctx, s.pool).Exec(ctx, `
		INSERT INTO email_suppressions (email, reason) VALUES (lower($1), $2) ON CONFLICT (email) DO NOTHING`, email, reason)
	if err != nil {
		return fmt.Errorf("suppress email: %w", err)
	}

	return nil
}
