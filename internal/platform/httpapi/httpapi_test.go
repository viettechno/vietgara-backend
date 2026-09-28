package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

type accountServer struct {
	identityv1.UnimplementedAccountServiceServer

	gotAuth, gotLang string
}

func (s *accountServer) GetMe(ctx context.Context, _ *identityv1.GetMeRequest) (*identityv1.GetMeResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	s.gotAuth, s.gotLang = first(md, "authorization"), first(md, "accept-language")

	return &identityv1.GetMeResponse{Account: &identityv1.Account{Id: "acc-1", FullName: "Minh"}}, nil
}

func (s *accountServer) UpdateMe(ctx context.Context, req *identityv1.UpdateMeRequest) (*identityv1.UpdateMeResponse, error) {
	switch req.GetAccount().GetFullName() {
	case "created":
		httpapi.Created(ctx)

		return &identityv1.UpdateMeResponse{Account: req.GetAccount()}, nil
	case "invalid":
		return nil, apperror.ToGRPCError(apperror.Validation("fullName", "required", "name is required"))
	case "otp":
		return nil, apperror.ToGRPCError(apperror.New(apperror.CodeInvalidArgument, "bad code").WithReason("OTP_INVALID"))
	case "limit":
		return nil, apperror.ToGRPCError(apperror.New(apperror.CodeFailedPrecondition, "upgrade").WithReason("PLAN_LIMIT_REACHED"))
	case "crash":
		return nil, errors.New("database password is hunter2")
	default:
		return nil, status.Error(codes.PermissionDenied, "no")
	}
}

type roleServer struct {
	tenantv1.UnimplementedRoleServiceServer
}

func (roleServer) DeleteRole(_ context.Context, req *tenantv1.DeleteRoleRequest) (*tenantv1.DeleteRoleResponse, error) {
	if req.GetRoleId() == "in-use" {
		return nil, apperror.ToGRPCError(apperror.New(apperror.CodeFailedPrecondition, "in use").WithReason("ROLE_IN_USE"))
	}

	return &tenantv1.DeleteRoleResponse{}, nil
}

func first(md metadata.MD, key string) string {
	if v := md.Get(key); len(v) > 0 {
		return v[0]
	}

	return ""
}

func newGateway(t *testing.T) (http.Handler, *accountServer) {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	accounts := &accountServer{}
	identityv1.RegisterAccountServiceServer(srv, accounts)
	tenantv1.RegisterRoleServiceServer(srv, roleServer{})

	go func() { _ = srv.Serve(lis) }()

	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	mux := httpapi.NewMux()
	require.NoError(t, identityv1.RegisterAccountServiceHandler(t.Context(), mux, conn))
	require.NoError(t, tenantv1.RegisterRoleServiceHandler(t.Context(), mux, conn))

	return httpapi.NoContentForDelete(mux), accounts
}

func do(t *testing.T, h http.Handler, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Accept-Language", "vi")
	req.Header.Set("X-Ignored", "x")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var parsed map[string]any
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &parsed), rec.Body.String())
	}

	return rec, parsed
}

func errorOf(body map[string]any) (code, message string, details []any) {
	e, _ := body["error"].(map[string]any)
	code, _ = e["code"].(string)
	message, _ = e["message"].(string)
	details, _ = e["details"].([]any)

	return code, message, details
}

func TestGateway(t *testing.T) {
	t.Parallel()

	h, accounts := newGateway(t)

	rec, body := do(t, h, http.MethodGet, "/api/v1/me", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "acc-1", body["id"])
	assert.Equal(t, false, body["emailVerified"], "zero values are emitted")
	assert.Equal(t, "Bearer tok", accounts.gotAuth)
	assert.Equal(t, "vi", accounts.gotLang)

	rec, _ = do(t, h, http.MethodPatch, "/api/v1/me", `{"fullName":"created"}`)
	assert.Equal(t, http.StatusCreated, rec.Code)

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantCode   string
		wantMsg    string
		wantFields int
	}{
		{name: "validation", method: http.MethodPatch, path: "/api/v1/me", body: `{"fullName":"invalid"}`, wantStatus: 422, wantCode: "VALIDATION_ERROR", wantMsg: "name is required", wantFields: 1},
		{name: "reasoned invalid argument", method: http.MethodPatch, path: "/api/v1/me", body: `{"fullName":"otp"}`, wantStatus: 422, wantCode: "OTP_INVALID", wantMsg: "bad code"},
		{name: "plan limit is 402", method: http.MethodPatch, path: "/api/v1/me", body: `{"fullName":"limit"}`, wantStatus: 402, wantCode: "PLAN_LIMIT_REACHED", wantMsg: "upgrade"},
		{name: "internal errors are hidden", method: http.MethodPatch, path: "/api/v1/me", body: `{"fullName":"crash"}`, wantStatus: 500, wantCode: "INTERNAL", wantMsg: "internal error"},
		{name: "plain status", method: http.MethodPatch, path: "/api/v1/me", body: `{"fullName":"x"}`, wantStatus: 403, wantCode: "PERMISSION_DENIED", wantMsg: "no"},
		{name: "malformed JSON is 400", method: http.MethodPatch, path: "/api/v1/me", body: `{`, wantStatus: 400, wantCode: "BAD_REQUEST"},
		{name: "unknown route", method: http.MethodGet, path: "/api/v1/nope", wantStatus: 404, wantCode: "NOT_FOUND", wantMsg: "Not Found"},
		{name: "wrong method", method: http.MethodPut, path: "/api/v1/me", wantStatus: 405, wantCode: "METHOD_NOT_ALLOWED", wantMsg: "Method Not Allowed"},
		{name: "delete conflict keeps its body", method: http.MethodDelete, path: "/api/v1/roles/in-use", wantStatus: 409, wantCode: "ROLE_IN_USE", wantMsg: "in use"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec, body := do(t, h, tt.method, tt.path, tt.body)
			require.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())

			code, msg, details := errorOf(body)
			assert.Equal(t, tt.wantCode, code)

			if tt.wantMsg != "" {
				assert.Equal(t, tt.wantMsg, msg)
			}

			assert.Len(t, details, tt.wantFields)
		})
	}

	rec, _ = do(t, h, http.MethodDelete, "/api/v1/roles/r-1", "")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Empty(t, rec.Header().Get("Content-Type"))
}

func TestWriteHelpers(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	httpapi.WriteError(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil),
		apperror.New(apperror.CodeNotFound, "missing"))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"NOT_FOUND","message":"missing","details":[]}}`, rec.Body.String())

	codes := map[codes.Code]string{
		codes.InvalidArgument: "BAD_REQUEST", codes.Unauthenticated: "UNAUTHENTICATED", codes.AlreadyExists: "ALREADY_EXISTS",
		codes.FailedPrecondition: "FAILED_PRECONDITION", codes.ResourceExhausted: "RATE_LIMITED", codes.Unavailable: "INTERNAL",
	}
	for code, want := range codes {
		rec := httptest.NewRecorder()
		httpapi.WriteError(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), status.Error(code, "x"))
		assert.Contains(t, rec.Body.String(), `"code":"`+want+`"`, code)
	}

	// A non-empty DELETE answer passes through untouched.
	h := httpapi.NoContentForDelete(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(`{"a":1}`))
		_, _ = w.Write([]byte(`,`))
	}))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/", nil))
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, `{"a":1},`, rec.Body.String())

	// An empty-body DELETE with no explicit status becomes 204.
	h = httpapi.NoContentForDelete(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
		_, _ = w.Write([]byte("\n"))
	}))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/", nil))
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
}
