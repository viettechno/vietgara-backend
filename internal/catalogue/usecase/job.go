package usecase

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

// JobPartInput is one suggested part of a job as the caller sends it.
type JobPartInput struct {
	PartID      string
	UnitID      *string
	Quantity    string
	NonBillable bool
}

// JobFields are the settable fields of a job; nil fields are left unchanged
// (on create: defaulted). Parts replaces the whole list when set.
type JobFields struct {
	Name            *string
	Category        *string
	LaborPrice      *int64
	DurationMinutes *int
	Parts           *[]JobPartInput
	IsActive        *bool
}

// CreateJobRequest is the input for UseCase.CreateJob.
type CreateJobRequest struct {
	GarageID string
	ActorID  string
	Fields   JobFields
}

// UpdateJobRequest is the input for UseCase.UpdateJob.
type UpdateJobRequest struct {
	GarageID string
	JobID    string
	ActorID  string
	Fields   JobFields
}

// ListJobsRequest is the input for UseCase.ListJobs.
type ListJobsRequest struct {
	PageRequest

	GarageID        string
	Query           string
	Category        string
	IncludeInactive bool
}

// CreateJob adds a job to the garage's catalogue (FR-CAT-05).
func (uc *UseCase) CreateJob(ctx context.Context, req CreateJobRequest) (*domain.Job, error) {
	job := &domain.Job{
		ID: uuid.NewString(), GarageID: req.GarageID, IsActive: true,
		CreatedBy: actorRef(req.ActorID), UpdatedBy: actorRef(req.ActorID),
	}

	if req.Fields.Name == nil {
		return nil, toAppError("create job", domain.ErrJobNameRequired)
	}

	if _, err := uc.applyJobFields(ctx, job, req.Fields); err != nil {
		return nil, toAppError("create job", err)
	}

	if err := uc.repo.CreateJob(ctx, job); err != nil {
		return nil, toAppError("create job", err)
	}

	return uc.GetJob(ctx, req.GarageID, job.ID)
}

// UpdateJob applies the partial updates in req to a job. Sending parts
// replaces the whole suggested list.
func (uc *UseCase) UpdateJob(ctx context.Context, req UpdateJobRequest) (*domain.Job, error) {
	job, err := uc.repo.FindJob(ctx, req.GarageID, req.JobID)
	if err != nil {
		return nil, toAppError("update job", err)
	}

	replaceParts, err := uc.applyJobFields(ctx, job, req.Fields)
	if err != nil {
		return nil, toAppError("update job", err)
	}

	job.UpdatedBy = actorRef(req.ActorID)

	if err := uc.repo.UpdateJob(ctx, job, replaceParts); err != nil {
		return nil, toAppError("update job", err)
	}

	return uc.GetJob(ctx, req.GarageID, req.JobID)
}

// GetJob returns a job with its suggested parts described.
func (uc *UseCase) GetJob(ctx context.Context, garageID, jobID string) (*domain.Job, error) {
	job, err := uc.repo.FindJob(ctx, garageID, jobID)
	if err != nil {
		return nil, toAppError("get job", err)
	}

	if err := uc.describeJobs(ctx, garageID, []*domain.Job{job}); err != nil {
		return nil, toAppError("get job", err)
	}

	return job, nil
}

// ListJobs returns one page of the garage's jobs.
func (uc *UseCase) ListJobs(ctx context.Context, req ListJobsRequest) (*Page[*domain.Job], error) {
	page := req.normalize()

	jobs, total, err := uc.repo.ListJobs(ctx, req.GarageID, JobFilter{
		Query: strings.TrimSpace(req.Query), Category: strings.TrimSpace(req.Category),
		IncludeInactive: req.IncludeInactive, Limit: page.PageSize, Offset: page.offset(),
	})
	if err != nil {
		return nil, toAppError("list jobs", err)
	}

	if err := uc.describeJobs(ctx, req.GarageID, jobs); err != nil {
		return nil, toAppError("list jobs", err)
	}

	return &Page[*domain.Job]{Items: jobs, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// DeleteJob follows the delete-or-deactivate rule (FR-CAT-07): a job that a
// quote line came from is deactivated, any other is deleted.
func (uc *UseCase) DeleteJob(ctx context.Context, req DeleteRequest) (lifecycle.Outcome, error) {
	if _, err := uc.repo.FindJob(ctx, req.GarageID, req.ID); err != nil {
		return "", toAppError("delete job", err)
	}

	outcome, err := lifecycle.Apply(ctx, req.DryRun, lifecycle.Rule{
		InUse: func(ctx context.Context) (bool, error) { return uc.usage.JobInUse(ctx, req.GarageID, req.ID) },
		Delete: func(ctx context.Context) error {
			return uc.repo.SoftDeleteJob(ctx, req.GarageID, req.ID, req.ActorID)
		},
		Deactivate: func(ctx context.Context) error {
			return uc.repo.SetJobActive(ctx, req.GarageID, req.ID, false, req.ActorID)
		},
	})
	if err != nil {
		return "", toAppError("delete job", err)
	}

	return outcome, nil
}

// ActivateJob reactivates a deactivated job.
func (uc *UseCase) ActivateJob(ctx context.Context, garageID, jobID, actorID string) (*domain.Job, error) {
	if err := uc.repo.SetJobActive(ctx, garageID, jobID, true, actorID); err != nil {
		return nil, toAppError("activate job", err)
	}

	return uc.GetJob(ctx, garageID, jobID)
}

// applyJobFields validates the fields and applies them to job. It reports
// whether the suggested parts were replaced.
func (uc *UseCase) applyJobFields(ctx context.Context, job *domain.Job, f JobFields) (bool, error) {
	if err := applyJobScalars(job, f); err != nil {
		return false, err
	}

	if f.IsActive != nil {
		job.IsActive = *f.IsActive
	}

	if f.Parts == nil {
		return false, nil
	}

	parts, err := uc.cleanJobParts(ctx, job.GarageID, *f.Parts)
	if err != nil {
		return false, err
	}

	job.Parts = parts

	return true, nil
}

func applyJobScalars(job *domain.Job, f JobFields) error {
	if f.Name != nil {
		name := strings.TrimSpace(*f.Name)
		if name == "" {
			return domain.ErrJobNameRequired
		}

		if err := domain.CheckLength(name, domain.MaxJobNameLen); err != nil {
			return err
		}

		job.Name = name
	}

	if f.Category != nil {
		category := strings.TrimSpace(*f.Category)
		if err := domain.CheckLength(category, domain.MaxJobCategoryLen); err != nil {
			return err
		}

		job.Category = category
	}

	if f.LaborPrice != nil {
		if *f.LaborPrice < 0 {
			return domain.ErrInvalidLaborPrice
		}

		job.LaborPrice = *f.LaborPrice
	}

	if f.DurationMinutes != nil {
		if *f.DurationMinutes < 0 {
			return domain.ErrInvalidDuration
		}

		job.DurationMinutes = *f.DurationMinutes
	}

	return nil
}

// cleanJobParts validates the suggested parts: each part once, active, in a
// unit it has, with an amount above zero.
func (uc *UseCase) cleanJobParts(ctx context.Context, garageID string, in []JobPartInput) ([]domain.JobPart, error) {
	if len(in) > domain.MaxJobParts {
		return nil, domain.ErrTooManyJobParts
	}

	if len(in) == 0 {
		return nil, nil
	}

	ids := make([]string, 0, len(in))
	for _, p := range in {
		ids = append(ids, p.PartID)
	}

	found, err := uc.repo.FindPartsByIDs(ctx, garageID, ids)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]*domain.Part, len(found))
	for _, p := range found {
		byID[p.ID] = p
	}

	seen := make(map[string]struct{}, len(in))
	out := make([]domain.JobPart, 0, len(in))

	for _, p := range in {
		part, ok := byID[p.PartID]
		if !ok {
			return nil, domain.ErrPartNotFound
		}

		if !part.IsActive {
			return nil, domain.ErrPartInactive
		}

		if _, dup := seen[p.PartID]; dup {
			return nil, domain.ErrDuplicateJobPart
		}

		seen[p.PartID] = struct{}{}

		clean, err := cleanJobPart(part, p)
		if err != nil {
			return nil, err
		}

		out = append(out, clean)
	}

	return out, nil
}

func cleanJobPart(part *domain.Part, in JobPartInput) (domain.JobPart, error) {
	rat, ok := domain.ParseDecimal(in.Quantity)
	if !ok || rat.Sign() <= 0 {
		return domain.JobPart{}, domain.ErrInvalidQuantity
	}

	quantity, ok := domain.FormatDecimal(rat)
	if !ok {
		return domain.JobPart{}, domain.ErrQuantityPrecision
	}

	var unitID *string

	if in.UnitID != nil && *in.UnitID != "" && *in.UnitID != part.BaseUnitID {
		found := false

		for _, u := range part.PackagingUnits {
			if u.UnitID == *in.UnitID {
				found = true

				break
			}
		}

		if !found {
			return domain.JobPart{}, domain.ErrUnitNotForPart
		}

		unitID = in.UnitID
	}

	return domain.JobPart{PartID: part.ID, UnitID: unitID, Quantity: quantity, NonBillable: in.NonBillable}, nil
}

// describeJobs fills the derived fields of the jobs' suggested parts: the
// part's code, name and state, the unit's abbreviation and the current price.
func (uc *UseCase) describeJobs(ctx context.Context, garageID string, jobs []*domain.Job) error {
	var ids []string

	for _, j := range jobs {
		for _, p := range j.Parts {
			ids = append(ids, p.PartID)
		}
	}

	if len(ids) == 0 {
		return nil
	}

	found, err := uc.repo.FindPartsByIDs(ctx, garageID, ids)
	if err != nil {
		return err
	}

	byID := make(map[string]*domain.Part, len(found))
	for _, p := range found {
		byID[p.ID] = p
	}

	for _, j := range jobs {
		for i := range j.Parts {
			describeJobPart(&j.Parts[i], byID[j.Parts[i].PartID])
		}
	}

	return nil
}

func describeJobPart(jp *domain.JobPart, part *domain.Part) {
	if part == nil {
		return
	}

	jp.PartSKU, jp.PartName, jp.PartIsActive = part.SKU, part.Name, part.IsActive
	jp.UnitAbbreviation, jp.UnitPrice = part.BaseUnitAbbreviation, part.SalePrice

	if jp.UnitID == nil {
		return
	}

	for _, u := range part.PackagingUnits {
		if u.UnitID == *jp.UnitID {
			jp.UnitAbbreviation, jp.UnitPrice = u.UnitAbbreviation, u.EffectivePrice

			return
		}
	}
}
