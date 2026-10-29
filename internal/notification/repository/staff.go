package repository

import (
	"context"
	"fmt"

	"github.com/viettechno/vietgara-backend/internal/notification/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// PostgresStaff implements usecase.StaffReader. It reads the tenant tables
// the access check reads, in the garage's transaction.
type PostgresStaff struct {
	pool *platformdb.Pool
}

// NewPostgresStaff returns a PostgresStaff on pool.
func NewPostgresStaff(pool *platformdb.Pool) *PostgresStaff { return &PostgresStaff{pool: pool} }

// Recipients returns the garage's owner, the given users who are active
// staff of the garage and, when permission is set, the active staff whose
// role grants it, each once and only while their account is active.
func (s *PostgresStaff) Recipients(ctx context.Context, garageID string, userIDs []string, permission string) ([]usecase.UserInfo, error) {
	if userIDs == nil {
		userIDs = []string{}
	}

	rows, err := platformdb.QuerierFromContext(ctx, s.pool).Query(ctx, `
		SELECT u.id, u.email, u.full_name, u.locale::text
		FROM users u
		WHERE u.status = 'active' AND u.id IN (
			SELECT g.user_id FROM garages g WHERE g.id = $1
			UNION
			SELECT st.user_id FROM garage_staff st
			WHERE st.garage_id = $1 AND st.status = 'active' AND st.user_id = ANY($2::uuid[])
			UNION
			SELECT st.user_id FROM garage_staff st
			JOIN staff_group_members m ON m.staff_id = st.id
			JOIN staff_groups sg ON sg.id = m.group_id
			JOIN role_permissions rp ON rp.role_id = sg.role_id
			WHERE st.garage_id = $1 AND st.status = 'active' AND $3::text <> '' AND rp.permission::text = $3
		)
		ORDER BY u.email`, garageID, userIDs, permission)
	if err != nil {
		return nil, fmt.Errorf("query notification recipients: %w", err)
	}
	defer rows.Close()

	var users []usecase.UserInfo

	for rows.Next() {
		var u usecase.UserInfo

		if err := rows.Scan(&u.ID, &u.Email, &u.FullName, &u.Locale); err != nil {
			return nil, fmt.Errorf("scan notification recipient: %w", err)
		}

		users = append(users, u)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read notification recipients: %w", err)
	}

	return users, nil
}
