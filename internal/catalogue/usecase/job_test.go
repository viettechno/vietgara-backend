package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

const jobID = "88888888-8888-8888-8888-888888888888"

func sampleJob() *domain.Job {
	return &domain.Job{
		ID: jobID, GarageID: garageID, Name: "Oil change", Category: "Maintenance", LaborPrice: 200_000, DurationMinutes: 45, IsActive: true,
		Parts: []domain.JobPart{{PartID: partID, Quantity: "4"}},
	}
}

// pricedCasePart is a part that also comes in a case of 24 with a case price.
func pricedCasePart() *domain.Part {
	p := samplePart()
	p.PackagingUnits = []domain.PackagingUnit{{UnitID: caseID, Factor: "24", UnitAbbreviation: "ctn", EffectivePrice: 330_000}}

	return p
}

func jobFields(parts ...usecase.JobPartInput) usecase.JobFields {
	return usecase.JobFields{
		Name: new("  Oil change "), Category: new("Maintenance"), LaborPrice: new(int64(200_000)), DurationMinutes: new(45),
		Parts: &parts,
	}
}

func partsFound(d *deps, parts ...*domain.Part) {
	d.Repo.EXPECT().FindPartsByIDs(mock.Anything, garageID, mock.Anything).Return(parts, nil)
}

//nolint:funlen // one table of scenarios
func TestUseCase_CreateJob(t *testing.T) {
	t.Parallel()

	create := func(t *testing.T, uc *usecase.UseCase, f usecase.JobFields) (*domain.Job, error) {
		t.Helper()

		return uc.CreateJob(t.Context(), usecase.CreateJobRequest{GarageID: garageID, ActorID: actorID, Fields: f})
	}

	t.Run("creates the job and answers it with its parts described", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		partsFound(d, pricedCasePart())
		d.Repo.EXPECT().CreateJob(mock.Anything, mock.MatchedBy(func(j *domain.Job) bool {
			return j.Name == "Oil change" && j.IsActive && len(j.Parts) == 1 && j.Parts[0].Quantity == "4" &&
				j.Parts[0].UnitID == nil && *j.CreatedBy == actorID
		})).Return(nil).Once()
		d.Repo.EXPECT().FindJob(mock.Anything, garageID, mock.Anything).Return(sampleJob(), nil).Once()

		job, err := create(t, uc, jobFields(usecase.JobPartInput{PartID: partID, Quantity: "4.000"}))
		require.NoError(t, err)
		assert.Equal(t, "BEER-330", job.Parts[0].PartSKU)
		assert.Equal(t, "can", job.Parts[0].UnitAbbreviation)
		assert.Equal(t, int64(15_000), job.Parts[0].UnitPrice)
		assert.True(t, job.Parts[0].PartIsActive)
	})

	t.Run("a packaging unit prices the line per case", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		partsFound(d, pricedCasePart())
		d.Repo.EXPECT().CreateJob(mock.Anything, mock.MatchedBy(func(j *domain.Job) bool {
			return j.Parts[0].UnitID != nil && *j.Parts[0].UnitID == caseID && j.Parts[0].NonBillable
		})).Return(nil).Once()

		withCase := sampleJob()
		withCase.Parts = []domain.JobPart{{PartID: partID, Quantity: "1", UnitID: new(caseID), NonBillable: true}}
		d.Repo.EXPECT().FindJob(mock.Anything, garageID, mock.Anything).Return(withCase, nil).Once()

		job, err := create(t, uc, jobFields(usecase.JobPartInput{PartID: partID, Quantity: "1", UnitID: new(caseID), NonBillable: true}))
		require.NoError(t, err)
		assert.Equal(t, "ctn", job.Parts[0].UnitAbbreviation)
		assert.Equal(t, int64(330_000), job.Parts[0].UnitPrice)
	})

	t.Run("the base unit named as a unit means no unit", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		partsFound(d, pricedCasePart())
		d.Repo.EXPECT().CreateJob(mock.Anything, mock.MatchedBy(func(j *domain.Job) bool { return j.Parts[0].UnitID == nil })).Return(nil).Once()
		d.Repo.EXPECT().FindJob(mock.Anything, garageID, mock.Anything).Return(sampleJob(), nil).Once()

		_, err := create(t, uc, jobFields(usecase.JobPartInput{PartID: partID, Quantity: "1", UnitID: new(unitID)}))
		require.NoError(t, err)
	})

	t.Run("a name is required", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		_, err := create(t, uc, usecase.JobFields{})
		assertAppError(t, err, apperror.CodeInvalidArgument)

		_, err = create(t, uc, usecase.JobFields{Name: new("   ")})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("field rules", func(t *testing.T) {
		t.Parallel()

		long := string(make([]byte, domain.MaxJobNameLen+1))
		for _, f := range []usecase.JobFields{
			{Name: new(long)},
			{Name: new("x"), Category: new(string(make([]byte, domain.MaxJobCategoryLen+1)))},
			{Name: new("x"), LaborPrice: new(int64(-1))},
			{Name: new("x"), DurationMinutes: new(-5)},
		} {
			uc, _ := newUseCase(t)

			_, err := create(t, uc, f)
			assertAppError(t, err, apperror.CodeInvalidArgument)
		}
	})

	t.Run("part rules", func(t *testing.T) {
		t.Parallel()

		inactive := samplePart()
		inactive.IsActive = false

		many := make([]usecase.JobPartInput, domain.MaxJobParts+1)

		tests := []struct {
			name  string
			found []*domain.Part
			parts []usecase.JobPartInput
			code  apperror.Code
		}{
			{"too many parts", nil, many, apperror.CodeInvalidArgument},
			{"unknown part", nil, []usecase.JobPartInput{{PartID: partID, Quantity: "1"}}, apperror.CodeNotFound},
			{"inactive part", []*domain.Part{inactive}, []usecase.JobPartInput{{PartID: partID, Quantity: "1"}}, apperror.CodeFailedPrecondition},
			{
				"the same part twice",
				[]*domain.Part{samplePart()},
				[]usecase.JobPartInput{{PartID: partID, Quantity: "1"}, {PartID: partID, Quantity: "2"}},
				apperror.CodeInvalidArgument,
			},
			{"zero quantity", []*domain.Part{samplePart()}, []usecase.JobPartInput{{PartID: partID, Quantity: "0"}}, apperror.CodeInvalidArgument},
			{"bad quantity", []*domain.Part{samplePart()}, []usecase.JobPartInput{{PartID: partID, Quantity: "x"}}, apperror.CodeInvalidArgument},
			{
				"too many decimals",
				[]*domain.Part{samplePart()},
				[]usecase.JobPartInput{{PartID: partID, Quantity: "1.0005"}},
				apperror.CodeInvalidArgument,
			},
			{
				"a unit the part does not have",
				[]*domain.Part{samplePart()},
				[]usecase.JobPartInput{{PartID: partID, Quantity: "1", UnitID: new(caseID)}},
				apperror.CodeInvalidArgument,
			},
		}

		for _, tt := range tests {
			uc, d := newUseCase(t)
			if len(tt.parts) <= domain.MaxJobParts {
				partsFound(d, tt.found...)
			}

			_, err := create(t, uc, jobFields(tt.parts...))
			assertAppError(t, err, tt.code)
		}
	})

	t.Run("persistence failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().CreateJob(mock.Anything, mock.Anything).Return(domain.ErrJobNameTaken).Once()

		_, err := create(t, uc, jobFields())
		assertAppError(t, err, apperror.CodeAlreadyExists)

		d.Repo.EXPECT().CreateJob(mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().FindJob(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		_, err = create(t, uc, jobFields())
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestUseCase_UpdateJob(t *testing.T) {
	t.Parallel()

	update := func(t *testing.T, uc *usecase.UseCase, f usecase.JobFields) (*domain.Job, error) {
		t.Helper()

		return uc.UpdateJob(t.Context(), usecase.UpdateJobRequest{GarageID: garageID, JobID: jobID, ActorID: actorID, Fields: f})
	}

	t.Run("a partial update keeps the suggested parts", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		partsFound(d, samplePart())
		d.Repo.EXPECT().FindJob(mock.Anything, garageID, jobID).Return(sampleJob(), nil).Twice()
		d.Repo.EXPECT().UpdateJob(mock.Anything, mock.MatchedBy(func(j *domain.Job) bool {
			return j.LaborPrice == 250_000 && !j.IsActive && j.Name == "Oil change"
		}), false).Return(nil).Once()

		_, err := update(t, uc, usecase.JobFields{LaborPrice: new(int64(250_000)), IsActive: new(false)})
		require.NoError(t, err)
	})

	t.Run("sending parts replaces the list", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		partsFound(d, samplePart())
		d.Repo.EXPECT().FindJob(mock.Anything, garageID, jobID).Return(sampleJob(), nil).Twice()
		d.Repo.EXPECT().UpdateJob(mock.Anything, mock.Anything, true).Return(nil).Once()

		_, err := update(t, uc, usecase.JobFields{Parts: &[]usecase.JobPartInput{{PartID: partID, Quantity: "5"}}})
		require.NoError(t, err)
	})

	t.Run("sending no parts clears the list", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindJob(mock.Anything, garageID, jobID).Return(sampleJob(), nil).Twice()
		d.Repo.EXPECT().FindPartsByIDs(mock.Anything, garageID, mock.Anything).Return(nil, nil).Maybe()
		d.Repo.EXPECT().UpdateJob(mock.Anything, mock.MatchedBy(func(j *domain.Job) bool { return len(j.Parts) == 0 }), true).Return(nil).Once()

		_, err := update(t, uc, usecase.JobFields{Parts: &[]usecase.JobPartInput{}})
		require.NoError(t, err)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindJob(mock.Anything, garageID, jobID).Return(nil, domain.ErrJobNotFound).Once()

		_, err := update(t, uc, usecase.JobFields{})
		assertAppError(t, err, apperror.CodeNotFound)

		d.Repo.EXPECT().FindJob(mock.Anything, garageID, jobID).Return(sampleJob(), nil).Once()

		_, err = update(t, uc, usecase.JobFields{Name: new(" ")})
		assertAppError(t, err, apperror.CodeInvalidArgument)

		d.Repo.EXPECT().FindJob(mock.Anything, garageID, jobID).Return(sampleJob(), nil).Once()
		d.Repo.EXPECT().UpdateJob(mock.Anything, mock.Anything, false).Return(domain.ErrPackagingUnitUsedByJob).Once()

		_, err = update(t, uc, usecase.JobFields{})
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})
}

func TestUseCase_ReadJobs(t *testing.T) {
	t.Parallel()

	t.Run("list describes the parts of every job", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		partsFound(d, samplePart())
		d.Repo.EXPECT().ListJobs(mock.Anything, garageID, usecase.JobFilter{Query: "oil", Category: "Maintenance", Limit: 20}).
			Return([]*domain.Job{sampleJob()}, 1, nil).Once()

		page, err := uc.ListJobs(t.Context(), usecase.ListJobsRequest{GarageID: garageID, Query: " oil ", Category: "Maintenance"})
		require.NoError(t, err)
		assert.Equal(t, 1, page.Total)
		assert.Equal(t, "Beer 330 ml", page.Items[0].Parts[0].PartName)
	})

	t.Run("an empty list needs no part lookup", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().ListJobs(mock.Anything, garageID, mock.Anything).Return(nil, 0, nil).Once()

		page, err := uc.ListJobs(t.Context(), usecase.ListJobsRequest{GarageID: garageID})
		require.NoError(t, err)
		assert.Empty(t, page.Items)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().ListJobs(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()
		_, err := uc.ListJobs(t.Context(), usecase.ListJobsRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)

		d.Repo.EXPECT().ListJobs(mock.Anything, garageID, mock.Anything).Return([]*domain.Job{sampleJob()}, 1, nil).Once()
		d.Repo.EXPECT().FindPartsByIDs(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
		_, err = uc.ListJobs(t.Context(), usecase.ListJobsRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)

		d.Repo.EXPECT().FindJob(mock.Anything, garageID, jobID).Return(nil, domain.ErrJobNotFound).Once()
		_, err = uc.GetJob(t.Context(), garageID, jobID)
		assertAppError(t, err, apperror.CodeNotFound)

		d.Repo.EXPECT().FindJob(mock.Anything, garageID, jobID).Return(sampleJob(), nil).Once()
		d.Repo.EXPECT().FindPartsByIDs(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
		_, err = uc.GetJob(t.Context(), garageID, jobID)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("a part that is gone leaves its suggestion undescribed", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindJob(mock.Anything, garageID, jobID).Return(sampleJob(), nil).Once()
		partsFound(d)

		job, err := uc.GetJob(t.Context(), garageID, jobID)
		require.NoError(t, err)
		assert.False(t, job.Parts[0].PartIsActive)
	})
}

func TestUseCase_DeleteJob(t *testing.T) {
	t.Parallel()

	req := usecase.DeleteRequest{GarageID: garageID, ID: jobID, ActorID: actorID}

	t.Run("an unused job is deleted", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindJob(mock.Anything, garageID, jobID).Return(sampleJob(), nil).Once()
		d.Usage.EXPECT().JobInUse(mock.Anything, garageID, jobID).Return(false, nil).Once()
		d.Repo.EXPECT().SoftDeleteJob(mock.Anything, garageID, jobID, actorID).Return(nil).Once()

		outcome, err := uc.DeleteJob(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deleted, outcome)
	})

	t.Run("a job on a quote is deactivated", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindJob(mock.Anything, garageID, jobID).Return(sampleJob(), nil).Once()
		d.Usage.EXPECT().JobInUse(mock.Anything, garageID, jobID).Return(true, nil).Once()
		d.Repo.EXPECT().SetJobActive(mock.Anything, garageID, jobID, false, actorID).Return(nil).Once()

		outcome, err := uc.DeleteJob(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deactivated, outcome)
	})

	t.Run("a dry run changes nothing", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindJob(mock.Anything, garageID, jobID).Return(sampleJob(), nil).Once()
		d.Usage.EXPECT().JobInUse(mock.Anything, garageID, jobID).Return(true, nil).Once()

		dry := req
		dry.DryRun = true

		outcome, err := uc.DeleteJob(t.Context(), dry)
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deactivated, outcome)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindJob(mock.Anything, garageID, jobID).Return(nil, domain.ErrJobNotFound).Once()

		_, err := uc.DeleteJob(t.Context(), req)
		assertAppError(t, err, apperror.CodeNotFound)

		d.Repo.EXPECT().FindJob(mock.Anything, garageID, jobID).Return(sampleJob(), nil).Once()
		d.Usage.EXPECT().JobInUse(mock.Anything, garageID, jobID).Return(false, errBoom).Once()

		_, err = uc.DeleteJob(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestUseCase_ActivateJob(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Repo.EXPECT().SetJobActive(mock.Anything, garageID, jobID, true, actorID).Return(nil).Once()
	d.Repo.EXPECT().FindJob(mock.Anything, garageID, jobID).Return(sampleJob(), nil).Once()
	partsFound(d, samplePart())

	job, err := uc.ActivateJob(t.Context(), garageID, jobID, actorID)
	require.NoError(t, err)
	assert.True(t, job.IsActive)

	d.Repo.EXPECT().SetJobActive(mock.Anything, garageID, jobID, true, actorID).Return(domain.ErrJobNotFound).Once()
	_, err = uc.ActivateJob(t.Context(), garageID, jobID, actorID)
	assertAppError(t, err, apperror.CodeNotFound)
}

func TestUseCase_PartsUsedByJobsAreKept(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()
	d.Usage.EXPECT().PartInUse(mock.Anything, garageID, partID).Return(false, nil).Once()
	d.Repo.EXPECT().PartUsedByJobs(mock.Anything, garageID, partID).Return(true, nil).Once()
	d.Repo.EXPECT().SetPartActive(mock.Anything, garageID, partID, false, actorID).Return(nil).Once()

	outcome, err := uc.DeletePart(t.Context(), usecase.DeleteRequest{GarageID: garageID, ID: partID, ActorID: actorID})
	require.NoError(t, err)
	assert.Equal(t, lifecycle.Deactivated, outcome)
}

func TestUseCase_RemovingAPackagingUnitAJobUsesIsRefused(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()
	d.Repo.EXPECT().ReplacePackaging(mock.Anything, garageID, partID, actorID, mock.Anything).Return(domain.ErrPackagingUnitUsedByJob).Once()

	_, err := uc.SetPartUnits(t.Context(), garageID, partID, actorID, nil)
	assertAppError(t, err, apperror.CodeFailedPrecondition)

	var appErr *apperror.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, usecase.ReasonPackagingUnitUsedByJob, appErr.Reason)
}

func TestToAppError_InactiveJob(t *testing.T) {
	t.Parallel()

	err := usecase.ToAppError("add quote job", domain.ErrJobInactive)

	var appErr *apperror.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, apperror.CodeFailedPrecondition, appErr.Code)
	assert.Equal(t, usecase.ReasonJobInactive, appErr.Reason)
}
