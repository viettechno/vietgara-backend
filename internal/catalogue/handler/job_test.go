package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/internal/catalogue/handler"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
)

const jobID = "77777777-7777-7777-7777-777777777777"

func sampleJob() *domain.Job {
	return &domain.Job{
		ID: jobID, GarageID: garageID, Name: "Oil change", Category: "Maintenance", LaborPrice: 200_000, DurationMinutes: 45, IsActive: true,
		Parts: []domain.JobPart{{PartID: partID, Quantity: "4", NonBillable: true}},
	}
}

func TestJobPolicies(t *testing.T) {
	t.Parallel()

	p := handler.JobPolicies()
	assert.Len(t, p, 6)
	assert.NotNil(t, p[cataloguev1.JobService_CreateJob_FullMethodName].Garage)
}

func TestJobCRUD(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := signedIn(t)

	e.repo.EXPECT().FindPartsByIDs(anyArg, garageID, anyArg).Return([]*domain.Part{samplePart()}, nil)
	e.repo.EXPECT().CreateJob(anyArg, anyArg).Return(nil).Once()
	e.repo.EXPECT().FindJob(anyArg, garageID, anyArg).RunAndReturn(
		func(context.Context, string, string) (*domain.Job, error) { return sampleJob(), nil })

	created, err := e.jobs.CreateJob(ctx, &cataloguev1.CreateJobRequest{GarageId: garageID, Job: &cataloguev1.Job{
		Name: "Oil change", Category: "Maintenance", LaborPrice: 200_000, DurationMinutes: 45,
		Parts: []*cataloguev1.JobPart{{PartId: partID, Quantity: "4", NonBillable: true}},
	}})
	require.NoError(t, err)
	assert.Equal(t, "Oil change", created.GetJob().GetName())
	assert.True(t, created.GetJob().GetParts()[0].GetNonBillable())
	assert.Equal(t, "BEER-330", created.GetJob().GetParts()[0].GetPartSku())

	got, err := e.jobs.GetJob(ctx, &cataloguev1.GetJobRequest{GarageId: garageID, JobId: jobID})
	require.NoError(t, err)
	assert.Equal(t, int32(45), got.GetJob().GetDurationMinutes())

	e.repo.EXPECT().ListJobs(anyArg, garageID, anyArg).Return([]*domain.Job{sampleJob()}, 1, nil).Once()

	list, err := e.jobs.ListJobs(ctx, &cataloguev1.ListJobsRequest{GarageId: garageID, Query: "oil"})
	require.NoError(t, err)
	assert.Len(t, list.GetData(), 1)
	assert.Equal(t, int32(1), list.GetPagination().GetTotal())

	e.repo.EXPECT().UpdateJob(anyArg, anyArg, true).Return(nil).Once()

	_, err = e.jobs.UpdateJob(ctx, &cataloguev1.UpdateJobRequest{
		GarageId: garageID, JobId: jobID, Job: &cataloguev1.Job{Name: "Oil change", Category: "Care", LaborPrice: 1, Parts: nil},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name", "category", "labor_price", "duration_minutes", "parts", "is_active"}},
	})
	require.NoError(t, err)

	_, err = e.jobs.UpdateJob(ctx, &cataloguev1.UpdateJobRequest{
		GarageId: garageID, JobId: jobID, Job: &cataloguev1.Job{}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"id"}},
	})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	e.repo.EXPECT().SetJobActive(anyArg, garageID, jobID, true, actorID).Return(nil).Once()

	activated, err := e.jobs.ActivateJob(ctx, &cataloguev1.ActivateJobRequest{GarageId: garageID, JobId: jobID})
	require.NoError(t, err)
	assert.True(t, activated.GetJob().GetIsActive())
}

func TestJobDelete(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repo.EXPECT().FindJob(anyArg, garageID, jobID).Return(sampleJob(), nil).Twice()
	e.usage.EXPECT().JobInUse(anyArg, garageID, jobID).Return(true, nil).Twice()
	e.repo.EXPECT().SetJobActive(anyArg, garageID, jobID, false, actorID).Return(nil).Once()

	resp, err := e.jobs.DeleteJob(signedIn(t), &cataloguev1.DeleteJobRequest{GarageId: garageID, JobId: jobID})
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, resp.GetOutcome())

	dry, err := e.jobs.DeleteJob(signedIn(t), &cataloguev1.DeleteJobRequest{GarageId: garageID, JobId: jobID, DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, dry.GetOutcome())
}

func TestJobErrors(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := signedIn(t)

	e.repo.EXPECT().FindJob(anyArg, garageID, "missing").Return(nil, domain.ErrJobNotFound).Once()

	_, err := e.jobs.GetJob(ctx, &cataloguev1.GetJobRequest{GarageId: garageID, JobId: "missing"})
	assert.Equal(t, codes.NotFound, status.Code(err))

	e.repo.EXPECT().ListJobs(anyArg, garageID, anyArg).Return(nil, 0, errBoom).Once()

	_, err = e.jobs.ListJobs(ctx, &cataloguev1.ListJobsRequest{GarageId: garageID})
	assert.Equal(t, codes.Internal, status.Code(err))

	_, err = e.jobs.CreateJob(ctx, &cataloguev1.CreateJobRequest{GarageId: garageID, Job: &cataloguev1.Job{}})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	e.repo.EXPECT().SetJobActive(anyArg, garageID, "missing", true, actorID).Return(domain.ErrJobNotFound).Once()

	_, err = e.jobs.ActivateJob(ctx, &cataloguev1.ActivateJobRequest{GarageId: garageID, JobId: "missing"})
	assert.Equal(t, codes.NotFound, status.Code(err))

	e.repo.EXPECT().FindJob(anyArg, garageID, "missing").Return(nil, domain.ErrJobNotFound).Once()

	_, err = e.jobs.DeleteJob(ctx, &cataloguev1.DeleteJobRequest{GarageId: garageID, JobId: "missing"})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// TestJobRESTRoutes sends the job requests through the REST gateway: the
// activate verb and the response bodies are shaped as a client sees them.
func TestJobRESTRoutes(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	mux := httpapi.NewMux()
	require.NoError(t, cataloguev1.RegisterJobServiceHandlerServer(t.Context(), mux, e.jobs))

	send := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(signedIn(t), method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		return rec
	}

	e.repo.EXPECT().FindPartsByIDs(anyArg, garageID, anyArg).Return([]*domain.Part{samplePart()}, nil)
	e.repo.EXPECT().FindJob(anyArg, garageID, jobID).Return(sampleJob(), nil)
	e.repo.EXPECT().SetJobActive(anyArg, garageID, jobID, true, actorID).Return(nil).Once()
	e.repo.EXPECT().UpdateJob(anyArg, anyArg, false).Return(nil).Once()
	e.repo.EXPECT().ListJobs(anyArg, garageID, anyArg).Return([]*domain.Job{sampleJob()}, 1, nil).Once()

	rec := send(http.MethodGet, "/api/v1/garages/"+garageID+"/jobs/"+jobID, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"name":"Oil change"`, "the response is the job, not a wrapper")
	assert.Contains(t, rec.Body.String(), `"nonBillable":true`)

	rec = send(http.MethodPost, "/api/v1/garages/"+garageID+"/jobs/"+jobID+":activate", `{}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = send(http.MethodPatch, "/api/v1/garages/"+garageID+"/jobs/"+jobID, `{"laborPrice":250000}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = send(http.MethodGet, "/api/v1/garages/"+garageID+"/jobs?query=oil&includeInactive=true", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"pagination"`)
}
