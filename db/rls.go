package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const (
	userIDKey = "user_id"
	statusKey = "status"
)

var (
	errGarageNotFound              = errors.New("garage not found")
	errNoGarageUpdated             = errors.New("no garage updated (RLS may have blocked access)")
	errStaffMemberNotFoundInGarage = errors.New("staff member not found in garage (RLS may have blocked access)")
)

// RLSContext wraps a database connection with RLS context.
type RLSContext struct {
	DB       *sql.DB
	GarageID string
}

// NewRLSContext creates a new RLS context with a garage ID.
func NewRLSContext(db *sql.DB, garageID string) *RLSContext {
	return &RLSContext{
		DB:       db,
		GarageID: garageID,
	}
}

// WithRLSSession executes a callback with RLS session variable set
// This is a helper function to ensure proper RLS handling.
func (rc *RLSContext) WithRLSSession(ctx context.Context, callback func(context.Context, *sql.DB) error) error {
	// Set RLS session variable
	_, err := rc.DB.ExecContext(ctx,
		"SELECT set_config('app.current_garage_id', $1::text, false)",
		rc.GarageID,
	)
	if err != nil {
		return fmt.Errorf("failed to set RLS session variable: %w", err)
	}

	// Execute callback with RLS session active
	if err := callback(ctx, rc.DB); err != nil {
		return err
	}

	return nil
}

// GetGarage retrieves a garage by ID (with RLS).
func (rc *RLSContext) GetGarage(ctx context.Context) (map[string]any, error) {
	var garage map[string]any

	err := rc.WithRLSSession(ctx, func(ctx context.Context, db *sql.DB) error {
		rows, err := db.QueryContext(ctx,
			`SELECT id, user_id, name, timezone, currency, status
			 FROM garages`)
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}
		defer func() { _ = rows.Close() }()

		if rows.Next() {
			var id, userID, name, timezone, currency, status string
			if err := rows.Scan(&id, &userID, &name, &timezone, &currency, &status); err != nil {
				return fmt.Errorf("scan failed: %w", err)
			}

			garage = map[string]any{
				"id":       id,
				userIDKey:  userID,
				"name":     name,
				"timezone": timezone,
				"currency": currency,
				statusKey:  status,
			}
		}

		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate garages: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	if garage == nil {
		return nil, errGarageNotFound
	}

	return garage, nil
}

// GetGarageStaff retrieves all staff for a garage (with RLS).
func (rc *RLSContext) GetGarageStaff(ctx context.Context) ([]map[string]any, error) {
	var staff []map[string]any

	err := rc.WithRLSSession(ctx, func(ctx context.Context, db *sql.DB) error {
		rows, err := db.QueryContext(ctx,
			`SELECT id, garage_id, user_id, role, status, created_at
			 FROM garage_staff 
			 ORDER BY created_at DESC`)
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var id, garageID, userID, role, status, createdAt string
			if err := rows.Scan(&id, &garageID, &userID, &role, &status, &createdAt); err != nil {
				return fmt.Errorf("scan failed: %w", err)
			}

			staff = append(staff, map[string]any{
				"id":         id,
				"garage_id":  garageID,
				userIDKey:    userID,
				"role":       role,
				statusKey:    status,
				"created_at": createdAt,
			})
		}

		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate garage staff: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return staff, nil
}

// UpdateGarageStatus updates garage status (with RLS).
func (rc *RLSContext) UpdateGarageStatus(ctx context.Context, newStatus string) error {
	return rc.WithRLSSession(ctx, func(ctx context.Context, db *sql.DB) error {
		result, err := db.ExecContext(ctx,
			`UPDATE garages 
			 SET status = $1, updated_at = NOW() 
			 WHERE id = $2`,
			newStatus,
			rc.GarageID,
		)
		if err != nil {
			return fmt.Errorf("update failed: %w", err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
		}

		if rowsAffected == 0 {
			return errNoGarageUpdated
		}

		return nil
	})
}

// AssignStaffToGarage assigns a staff member to a garage (with RLS).
func (rc *RLSContext) AssignStaffToGarage(ctx context.Context, userID, role string) error {
	return rc.WithRLSSession(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := db.ExecContext(ctx,
			`INSERT INTO garage_staff (id, garage_id, user_id, role, status)
			 VALUES (gen_random_uuid(), $1, $2, $3, 'active') 
			 ON CONFLICT (garage_id, user_id) DO UPDATE
			 SET role = $3, status = 'active'`,
			rc.GarageID,
			userID,
			role,
		)
		if err != nil {
			return fmt.Errorf("insert/update failed: %w", err)
		}

		return nil
	})
}

// RemoveStaffFromGarage removes a staff member from a garage (with RLS).
func (rc *RLSContext) RemoveStaffFromGarage(ctx context.Context, userID string) error {
	return rc.WithRLSSession(ctx, func(ctx context.Context, db *sql.DB) error {
		result, err := db.ExecContext(ctx,
			`UPDATE garage_staff 
			 SET status = 'inactive', updated_at = NOW() 
			 WHERE garage_id = $1 AND user_id = $2`,
			rc.GarageID,
			userID,
		)
		if err != nil {
			return fmt.Errorf("update failed: %w", err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
		}

		if rowsAffected == 0 {
			return errStaffMemberNotFoundInGarage
		}

		return nil
	})
}

// Example usage in middleware or handler:
/*
func GarageHandler(w http.ResponseWriter, r *http.Request) {
	// Get garage ID from URL parameter or JWT token
	garageID := r.URL.Query().Get("garage_id")

	// Create RLS context
	rlsCtx := NewRLSContext(db, garageID)

	// Use RLS context for all database operations
	garage, err := rlsCtx.GetGarage(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(garage)
}
*/
