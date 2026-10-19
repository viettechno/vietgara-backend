package handler

import (
	"context"

	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

// JobServer implements cataloguev1.JobServiceServer (FR-CAT-05). Jobs are
// quoting data, so they follow the Quote permissions.
type JobServer struct {
	cataloguev1.UnimplementedJobServiceServer

	catalogue *usecase.UseCase
}

// NewJobServer wires the job handler to the Catalogue use case.
func NewJobServer(catalogue *usecase.UseCase) *JobServer {
	return &JobServer{catalogue: catalogue}
}

// JobPolicies declares the permission every JobService RPC requires.
func JobPolicies() middleware.Policies {
	read, write := access.Requires(access.QuoteRead), access.Requires(access.QuoteWrite)

	return middleware.Policies{
		cataloguev1.JobService_ListJobs_FullMethodName:    middleware.GarageMethod(read),
		cataloguev1.JobService_GetJob_FullMethodName:      middleware.GarageMethod(read),
		cataloguev1.JobService_CreateJob_FullMethodName:   middleware.GarageMethod(write),
		cataloguev1.JobService_UpdateJob_FullMethodName:   middleware.GarageMethod(write),
		cataloguev1.JobService_DeleteJob_FullMethodName:   middleware.GarageMethod(write),
		cataloguev1.JobService_ActivateJob_FullMethodName: middleware.GarageMethod(write),
	}
}

func jobToProto(j *domain.Job) *cataloguev1.Job {
	out := &cataloguev1.Job{
		Id: j.ID, GarageId: j.GarageID, Name: j.Name, Category: j.Category, LaborPrice: j.LaborPrice,
		DurationMinutes: toInt32(j.DurationMinutes), IsActive: j.IsActive,
		Parts:     make([]*cataloguev1.JobPart, 0, len(j.Parts)),
		CreatedAt: converter.TimeToProto(j.CreatedAt), UpdatedAt: converter.TimeToProto(j.UpdatedAt),
	}

	for _, p := range j.Parts {
		out.Parts = append(out.Parts, &cataloguev1.JobPart{
			PartId: p.PartID, Quantity: p.Quantity, UnitId: p.UnitID, NonBillable: p.NonBillable,
			PartSku: p.PartSKU, PartName: p.PartName, UnitAbbreviation: p.UnitAbbreviation,
			UnitPrice: p.UnitPrice, PartIsActive: p.PartIsActive,
		})
	}

	return out
}

func jobPartInputs(in []*cataloguev1.JobPart) *[]usecase.JobPartInput {
	out := make([]usecase.JobPartInput, 0, len(in))

	for _, p := range in {
		out = append(out, usecase.JobPartInput{
			PartID: p.GetPartId(), UnitID: p.UnitId, Quantity: p.GetQuantity(), NonBillable: p.GetNonBillable(),
		})
	}

	return &out
}

// jobFields reads the fields named by the update mask from job; an empty
// mask names every editable field.
func jobFields(job *cataloguev1.Job, paths []string) (usecase.JobFields, error) {
	var f usecase.JobFields

	if len(paths) == 0 {
		paths = []string{pathName, "category", "labor_price", "duration_minutes", "parts"}
	}

	for _, path := range paths {
		switch path {
		case pathName:
			f.Name = new(job.GetName())
		case "category":
			f.Category = new(job.GetCategory())
		case "labor_price":
			f.LaborPrice = new(job.GetLaborPrice())
		case "duration_minutes":
			f.DurationMinutes = new(int(job.GetDurationMinutes()))
		case "parts":
			f.Parts = jobPartInputs(job.GetParts())
		case pathIsActive:
			f.IsActive = new(job.GetIsActive())
		default:
			return f, badMask(path)
		}
	}

	return f, nil
}

// ListJobs lists a garage's jobs.
func (s *JobServer) ListJobs(ctx context.Context, req *cataloguev1.ListJobsRequest) (*cataloguev1.ListJobsResponse, error) {
	return run(ctx, func(string) (*cataloguev1.ListJobsResponse, error) {
		page, err := s.catalogue.ListJobs(ctx, usecase.ListJobsRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()), GarageID: req.GetGarageId(),
			Query: req.GetQuery(), Category: req.GetCategory(), IncludeInactive: req.GetIncludeInactive(),
		})
		if err != nil {
			return nil, err
		}

		resp := &cataloguev1.ListJobsResponse{Data: make([]*cataloguev1.Job, 0, len(page.Items)), Pagination: paginationToProto(page)}
		for _, j := range page.Items {
			resp.Data = append(resp.Data, jobToProto(j))
		}

		return resp, nil
	})
}

// GetJob returns one job.
func (s *JobServer) GetJob(ctx context.Context, req *cataloguev1.GetJobRequest) (*cataloguev1.GetJobResponse, error) {
	return run(ctx, func(string) (*cataloguev1.GetJobResponse, error) {
		job, err := s.catalogue.GetJob(ctx, req.GetGarageId(), req.GetJobId())
		if err != nil {
			return nil, err
		}

		return &cataloguev1.GetJobResponse{Job: jobToProto(job)}, nil
	})
}

// CreateJob adds a job.
func (s *JobServer) CreateJob(ctx context.Context, req *cataloguev1.CreateJobRequest) (*cataloguev1.CreateJobResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.CreateJobResponse, error) {
		in := req.GetJob()

		fields, err := jobFields(in, []string{pathName, "category", "labor_price", "duration_minutes", "parts"})
		if err != nil {
			return nil, err
		}

		job, err := s.catalogue.CreateJob(ctx, usecase.CreateJobRequest{GarageID: req.GetGarageId(), ActorID: actorID, Fields: fields})
		if err != nil {
			return nil, err
		}

		return &cataloguev1.CreateJobResponse{Job: jobToProto(job)}, nil
	})
}

// UpdateJob changes the fields named by the update mask.
func (s *JobServer) UpdateJob(ctx context.Context, req *cataloguev1.UpdateJobRequest) (*cataloguev1.UpdateJobResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.UpdateJobResponse, error) {
		fields, err := jobFields(req.GetJob(), req.GetUpdateMask().GetPaths())
		if err != nil {
			return nil, err
		}

		job, err := s.catalogue.UpdateJob(ctx, usecase.UpdateJobRequest{
			GarageID: req.GetGarageId(), JobID: req.GetJobId(), ActorID: actorID, Fields: fields,
		})
		if err != nil {
			return nil, err
		}

		return &cataloguev1.UpdateJobResponse{Job: jobToProto(job)}, nil
	})
}

// DeleteJob deletes or deactivates a job (FR-CAT-07).
func (s *JobServer) DeleteJob(ctx context.Context, req *cataloguev1.DeleteJobRequest) (*cataloguev1.DeleteJobResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.DeleteJobResponse, error) {
		outcome, err := deleteOutcome(ctx, s.catalogue.DeleteJob, req.GetGarageId(), req.GetJobId(), actorID, req.GetDryRun())
		if err != nil {
			return nil, err
		}

		return &cataloguev1.DeleteJobResponse{Outcome: outcome}, nil
	})
}

// ActivateJob reactivates a deactivated job.
func (s *JobServer) ActivateJob(ctx context.Context, req *cataloguev1.ActivateJobRequest) (*cataloguev1.ActivateJobResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.ActivateJobResponse, error) {
		job, err := s.catalogue.ActivateJob(ctx, req.GetGarageId(), req.GetJobId(), actorID)
		if err != nil {
			return nil, err
		}

		return &cataloguev1.ActivateJobResponse{Job: jobToProto(job)}, nil
	})
}
