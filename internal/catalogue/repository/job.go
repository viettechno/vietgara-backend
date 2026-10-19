package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
)

const (
	// pgRestrictViolation is raised by a foreign key with ON DELETE RESTRICT.
	pgRestrictViolation = "23001"

	jobColumns = `j.id, j.garage_id, j.name, j.category, j.labor_price, j.duration_minutes, j.is_active,
		j.created_by, j.updated_by, j.created_at, j.updated_at`
)

func scanJob(row pgx.Row) (*domain.Job, error) {
	var j domain.Job

	err := row.Scan(&j.ID, &j.GarageID, &j.Name, &j.Category, &j.LaborPrice, &j.DurationMinutes, &j.IsActive,
		&j.CreatedBy, &j.UpdatedBy, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return nil, err
	}

	return &j, nil
}

// translateJob maps a foreign key violation on the suggested packaging unit to
// its domain error; every other error goes through translate.
func translateJob(err error) error {
	var pgErr *pgconn.PgError

	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == "idx_jobs_garage_name_unique" {
		return domain.ErrJobNameTaken
	}

	return translate(err)
}

// CreateJob inserts j with its suggested parts.
func (r *PostgresRepository) CreateJob(ctx context.Context, j *domain.Job) error {
	err := r.q(ctx).QueryRow(ctx, `
		INSERT INTO jobs (id, garage_id, name, category, labor_price, duration_minutes, is_active, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING created_at, updated_at`,
		j.ID, j.GarageID, j.Name, j.Category, j.LaborPrice, j.DurationMinutes, j.IsActive, j.CreatedBy, j.UpdatedBy,
	).Scan(&j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return translateJob(err)
	}

	return r.insertJobParts(ctx, j)
}

func (r *PostgresRepository) insertJobParts(ctx context.Context, j *domain.Job) error {
	for i, p := range j.Parts {
		if _, err := r.q(ctx).Exec(ctx, `
			INSERT INTO job_parts (garage_id, job_id, position, part_id, unit_id, quantity, non_billable)
			VALUES ($1, $2, $3, $4, $5, $6::numeric, $7)`,
			j.GarageID, j.ID, i+1, p.PartID, p.UnitID, p.Quantity, p.NonBillable); err != nil {
			return err
		}
	}

	return nil
}

// FindJob returns the live job with its suggested parts in order.
func (r *PostgresRepository) FindJob(ctx context.Context, garageID, jobID string) (*domain.Job, error) {
	if !validID(jobID) {
		return nil, domain.ErrJobNotFound
	}

	j, err := scanJob(r.q(ctx).QueryRow(ctx,
		`SELECT `+jobColumns+` FROM jobs j WHERE j.garage_id = $1 AND j.id = $2 AND j.deleted_at IS NULL`,
		garageID, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrJobNotFound
	}

	if err != nil {
		return nil, err
	}

	byJob, err := r.jobParts(ctx, garageID, []string{j.ID})
	if err != nil {
		return nil, err
	}

	j.Parts = byJob[j.ID]

	return j, nil
}

// jobParts loads the suggested parts of the jobs, in order.
func (r *PostgresRepository) jobParts(ctx context.Context, garageID string, jobIDs []string) (map[string][]domain.JobPart, error) {
	rows, err := r.q(ctx).Query(ctx, `
		SELECT job_id, part_id, unit_id, quantity::text, non_billable
		FROM job_parts WHERE garage_id = $1 AND job_id = ANY($2::uuid[]) ORDER BY job_id, position`,
		garageID, jobIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]domain.JobPart, len(jobIDs))

	for rows.Next() {
		var (
			jobID string
			p     domain.JobPart
		)

		if err := rows.Scan(&jobID, &p.PartID, &p.UnitID, &p.Quantity, &p.NonBillable); err != nil {
			return nil, err
		}

		p.Quantity = trimNumeric(p.Quantity)
		out[jobID] = append(out[jobID], p)
	}

	return out, rows.Err()
}

// UpdateJob persists the job's fields and, when asked, replaces its parts.
func (r *PostgresRepository) UpdateJob(ctx context.Context, j *domain.Job, replaceParts bool) error {
	err := r.q(ctx).QueryRow(ctx, `
		UPDATE jobs SET name = $3, category = $4, labor_price = $5, duration_minutes = $6, is_active = $7, updated_by = $8
		WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL
		RETURNING updated_at`,
		j.GarageID, j.ID, j.Name, j.Category, j.LaborPrice, j.DurationMinutes, j.IsActive, j.UpdatedBy).Scan(&j.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrJobNotFound
	}

	if err != nil {
		return translateJob(err)
	}

	if !replaceParts {
		return nil
	}

	if _, err := r.q(ctx).Exec(ctx, `DELETE FROM job_parts WHERE garage_id = $1 AND job_id = $2`, j.GarageID, j.ID); err != nil {
		return err
	}

	return r.insertJobParts(ctx, j)
}

// ListJobs returns one page of live jobs ordered by category and name.
func (r *PostgresRepository) ListJobs(ctx context.Context, garageID string, f usecase.JobFilter) ([]*domain.Job, int, error) {
	const where = ` FROM jobs j WHERE j.garage_id = $1 AND j.deleted_at IS NULL AND ($2::bool OR j.is_active)
		AND ($3::text = '' OR j.name ILIKE $3 OR j.category ILIKE $3)
		AND ($4::text = '' OR lower(j.category) = lower($4))`

	pattern := containsPattern(f.Query)

	var total int
	if err := r.q(ctx).QueryRow(ctx, `SELECT count(*)`+where, garageID, f.IncludeInactive, pattern, f.Category).Scan(&total); err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := r.q(ctx).Query(ctx,
		`SELECT `+jobColumns+where+` ORDER BY lower(j.category), lower(j.name), j.id LIMIT $5 OFFSET $6`,
		garageID, f.IncludeInactive, pattern, f.Category, f.Limit, f.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	jobs := make([]*domain.Job, 0, f.Limit)
	ids := make([]string, 0, f.Limit)

	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, 0, err
		}

		jobs = append(jobs, j)
		ids = append(ids, j.ID)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	rows.Close()

	byJob, err := r.jobParts(ctx, garageID, ids)
	if err != nil {
		return nil, 0, err
	}

	for _, j := range jobs {
		j.Parts = byJob[j.ID]
	}

	return jobs, total, nil
}

// SoftDeleteJob marks the job deleted, which frees its name.
func (r *PostgresRepository) SoftDeleteJob(ctx context.Context, garageID, jobID, actorID string) error {
	return r.touch(ctx, `UPDATE jobs SET deleted_at = now(), updated_by = $3 WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL`,
		garageID, jobID, actorID, domain.ErrJobNotFound)
}

// SetJobActive deactivates or reactivates the job.
func (r *PostgresRepository) SetJobActive(ctx context.Context, garageID, jobID string, active bool, actorID string) error {
	return r.setActive(ctx, "jobs", garageID, jobID, active, actorID, domain.ErrJobNotFound)
}

// PartUsedByJobs reports whether a live job suggests the part.
func (r *PostgresRepository) PartUsedByJobs(ctx context.Context, garageID, partID string) (bool, error) {
	var used bool

	err := r.q(ctx).QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM job_parts jp JOIN jobs j ON j.garage_id = jp.garage_id AND j.id = jp.job_id
		               WHERE jp.garage_id = $1 AND jp.part_id = $2 AND j.deleted_at IS NULL)`,
		garageID, partID).Scan(&used)

	return used, err
}
