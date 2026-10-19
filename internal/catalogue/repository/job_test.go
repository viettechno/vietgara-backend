package repository_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

const jobID = "66666666-6666-6666-6666-666666666666"

func jobRow() []any {
	return []any{jobID, garageID, "Oil change", "Maintenance", int64(200_000), 45, true, nil, nil, now, now}
}

func jobPartRow() []any {
	return []any{jobID, partID, nil, "4.000", true}
}

func newJob() *domain.Job {
	return &domain.Job{
		ID: jobID, GarageID: garageID, Name: "Oil change", Category: "Maintenance", LaborPrice: 200_000, DurationMinutes: 45, IsActive: true,
		Parts: []domain.JobPart{{PartID: partID, Quantity: "4", NonBillable: true}, {PartID: caseID, UnitID: new(unitID), Quantity: "1"}},
	}
}

func TestPostgresRepository_JobCreateAndUpdate(t *testing.T) {
	t.Parallel()

	t.Run("create inserts the job and each suggested part", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO jobs", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO job_parts", dbtest.Tag(1), nil).
			ExpectExec("INSERT INTO job_parts", dbtest.Tag(1), nil)

		job := newJob()
		require.NoError(t, newRepo().CreateJob(tx.Context(t.Context()), job))
		assert.Equal(t, now, job.CreatedAt)
	})

	t.Run("create failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO jobs", dbtest.ErrRow(dbtest.UniqueViolation("idx_jobs_garage_name_unique"))).
			ExpectQueryRow("INSERT INTO jobs", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("INSERT INTO jobs", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO job_parts", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, newRepo().CreateJob(ctx, newJob()), domain.ErrJobNameTaken)
		require.ErrorIs(t, newRepo().CreateJob(ctx, newJob()), errBoom)
		require.ErrorIs(t, newRepo().CreateJob(ctx, newJob()), errBoom)
	})

	t.Run("update replaces the parts only when asked", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("UPDATE jobs SET", dbtest.Row(now)).
			ExpectQueryRow("UPDATE jobs SET", dbtest.Row(now)).
			ExpectExec("DELETE FROM job_parts", dbtest.Tag(2), nil).
			ExpectExec("INSERT INTO job_parts", dbtest.Tag(1), nil).
			ExpectExec("INSERT INTO job_parts", dbtest.Tag(1), nil)
		ctx := tx.Context(t.Context())

		require.NoError(t, newRepo().UpdateJob(ctx, newJob(), false))
		require.NoError(t, newRepo().UpdateJob(ctx, newJob(), true))
	})

	t.Run("update failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("UPDATE jobs SET", dbtest.ErrRow(pgx.ErrNoRows)).
			ExpectQueryRow("UPDATE jobs SET", dbtest.ErrRow(dbtest.UniqueViolation("idx_jobs_garage_name_unique"))).
			ExpectQueryRow("UPDATE jobs SET", dbtest.Row(now)).
			ExpectExec("DELETE FROM job_parts", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, newRepo().UpdateJob(ctx, newJob(), true), domain.ErrJobNotFound)
		require.ErrorIs(t, newRepo().UpdateJob(ctx, newJob(), true), domain.ErrJobNameTaken)
		require.ErrorIs(t, newRepo().UpdateJob(ctx, newJob(), true), errBoom)
	})
}

func TestPostgresRepository_JobReads(t *testing.T) {
	t.Parallel()

	t.Run("find returns the job with its parts", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("FROM jobs j WHERE", dbtest.Row(jobRow()...)).
			ExpectQuery("FROM job_parts", dbtest.NewRows(jobPartRow()), nil)

		job, err := newRepo().FindJob(tx.Context(t.Context()), garageID, jobID)
		require.NoError(t, err)
		require.Len(t, job.Parts, 1)
		assert.Equal(t, "4", job.Parts[0].Quantity)
		assert.True(t, job.Parts[0].NonBillable)
	})

	t.Run("find failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("FROM jobs j WHERE", dbtest.ErrRow(pgx.ErrNoRows)).
			ExpectQueryRow("FROM jobs j WHERE", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("FROM jobs j WHERE", dbtest.Row(jobRow()...)).
			ExpectQuery("FROM job_parts", nil, errBoom).
			ExpectQueryRow("FROM jobs j WHERE", dbtest.Row(jobRow()...)).
			ExpectQuery("FROM job_parts", dbtest.NewRows([]any{jobID}), nil)
		ctx := tx.Context(t.Context())

		_, err := newRepo().FindJob(ctx, garageID, jobID)
		require.ErrorIs(t, err, domain.ErrJobNotFound)

		_, err = newRepo().FindJob(ctx, garageID, "bad")
		require.ErrorIs(t, err, domain.ErrJobNotFound)

		_, err = newRepo().FindJob(ctx, garageID, jobID)
		require.ErrorIs(t, err, errBoom)

		_, err = newRepo().FindJob(ctx, garageID, jobID)
		require.ErrorIs(t, err, errBoom)

		_, err = newRepo().FindJob(ctx, garageID, jobID)
		require.Error(t, err) // a part row that cannot be scanned
	})

	t.Run("list pages the jobs and loads their parts", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*) FROM jobs", dbtest.Row(1)).
			ExpectQuery("FROM jobs j WHERE", dbtest.NewRows(jobRow()), nil).
			ExpectQuery("FROM job_parts", dbtest.NewRows(jobPartRow()), nil).
			ExpectQueryRow("SELECT count(*) FROM jobs", dbtest.Row(0))
		ctx := tx.Context(t.Context())

		jobs, total, err := newRepo().ListJobs(ctx, garageID, usecase.JobFilter{Query: "oil%", Category: "Maintenance", Limit: 20})
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		require.Len(t, jobs[0].Parts, 1)

		jobs, total, err = newRepo().ListJobs(ctx, garageID, usecase.JobFilter{Limit: 20})
		require.NoError(t, err)
		assert.Zero(t, total)
		assert.Empty(t, jobs)
	})

	t.Run("list failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*) FROM jobs", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("SELECT count(*) FROM jobs", dbtest.Row(1)).
			ExpectQuery("FROM jobs j WHERE", nil, errBoom).
			ExpectQueryRow("SELECT count(*) FROM jobs", dbtest.Row(1)).
			ExpectQuery("FROM jobs j WHERE", dbtest.NewRows([]any{jobID}), nil).
			ExpectQueryRow("SELECT count(*) FROM jobs", dbtest.Row(1)).
			ExpectQuery("FROM jobs j WHERE", dbtest.NewRows(jobRow()).WithErr(errBoom), nil).
			ExpectQueryRow("SELECT count(*) FROM jobs", dbtest.Row(1)).
			ExpectQuery("FROM jobs j WHERE", dbtest.NewRows(jobRow()), nil).
			ExpectQuery("FROM job_parts", nil, errBoom)
		ctx := tx.Context(t.Context())

		for range 5 {
			_, _, err := newRepo().ListJobs(ctx, garageID, usecase.JobFilter{Limit: 20})
			require.Error(t, err)
		}
	})
}

func TestPostgresRepository_JobLifecycle(t *testing.T) {
	t.Parallel()

	tx := dbtest.New(t).
		ExpectExec("UPDATE jobs SET deleted_at", dbtest.Tag(1), nil).
		ExpectExec("UPDATE jobs SET is_active", dbtest.Tag(1), nil).
		ExpectQueryRow("FROM job_parts jp", dbtest.Row(true)).
		ExpectQueryRow("FROM job_parts jp", dbtest.ErrRow(errBoom))
	ctx := tx.Context(t.Context())

	require.NoError(t, newRepo().SoftDeleteJob(ctx, garageID, jobID, "actor"))
	require.ErrorIs(t, newRepo().SoftDeleteJob(ctx, garageID, "bad", "actor"), domain.ErrJobNotFound)
	require.NoError(t, newRepo().SetJobActive(ctx, garageID, jobID, true, "actor"))

	used, err := newRepo().PartUsedByJobs(ctx, garageID, partID)
	require.NoError(t, err)
	assert.True(t, used)

	_, err = newRepo().PartUsedByJobs(ctx, garageID, partID)
	require.ErrorIs(t, err, errBoom)
}

func TestPostgresRepository_RemovingAJobsPackagingUnit(t *testing.T) {
	t.Parallel()

	tx := dbtest.New(t).
		ExpectExec("DELETE FROM part_units", dbtest.Tag(0), dbtest.RestrictViolationOn("job_parts_garage_part_unit_fkey")).
		ExpectExec("DELETE FROM part_units", dbtest.Tag(0), dbtest.RestrictViolationOn("something_else"))
	ctx := tx.Context(t.Context())

	err := newRepo().ReplacePackaging(ctx, garageID, partID, "actor", nil)
	require.ErrorIs(t, err, domain.ErrPackagingUnitUsedByJob)

	err = newRepo().ReplacePackaging(ctx, garageID, partID, "actor", nil)
	require.Error(t, err)
	require.NotErrorIs(t, err, domain.ErrPackagingUnitUsedByJob)
}
