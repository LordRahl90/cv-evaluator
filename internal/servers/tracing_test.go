package servers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"cv-evaluator/internal/servers"
	authhttp "cv-evaluator/internal/servers/auth"
	"cv-evaluator/internal/servers/cv"
	"cv-evaluator/internal/servers/middleware"
	"cv-evaluator/internal/services/users"
	"cv-evaluator/internal/telemetry/telemetrytest"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

const (
	tracingSecret = "tracing-test-secret"
	tracingEmail  = "trace.me@example.com"
)

type fakeEmbedder struct{}

func (fakeEmbedder) CreateEmbedding(context.Context, string) ([]float32, error) {
	return make([]float32, 768), nil
}

type fakeCleaner struct{}

func (fakeCleaner) CleanupCV(context.Context, string) (string, error) {
	return `{"summary":"s","skills":["go"],"title":"engineer"}`, nil
}

// tracedServer wires the real server (otelgin), handlers, users service and
// a Postgres database with the GORM plugin, as cmd/api does.
func tracedServer(t *testing.T) (*telemetrytest.Providers, http.Handler, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	p := telemetrytest.Install(t)

	db, err := postgres.MustRun(t).OpenGorm(nil)
	require.NoError(t, err)

	svc := users.NewWithAuth(db, fakeCleaner{}, fakeEmbedder{}, tracingSecret, 0)
	server := servers.New(&servers.Config{})
	opts := []middleware.Option{middleware.WithSessionValidator(authhttp.NewSessionValidator(svc))}
	authhttp.New(server.Router(), svc, []byte(tracingSecret), opts...)
	cv.New(server.Router(), svc, []byte(tracingSecret), opts...)
	return p, server.Router(), db
}

func signup(t *testing.T, h http.Handler) string {
	t.Helper()
	body := `{"first_name":"Trace","last_name":"Me","email":"` + tracingEmail + `","password":"correct-horse-battery"}`
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/signup", strings.NewReader(body)))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var session struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &session))
	return session.Token
}

// serverSpan returns the one server span whose name contains route.
func serverSpan(t *testing.T, p *telemetrytest.Providers, route string) sdktrace.ReadOnlySpan {
	t.Helper()
	var found []sdktrace.ReadOnlySpan
	for _, s := range p.SpansOfKind(trace.SpanKindServer) {
		if strings.Contains(s.Name(), route) {
			found = append(found, s)
		}
	}
	require.Len(t, found, 1, "server spans for %s: %v", route, p.Names())
	return found[0]
}

func childrenOf(p *telemetrytest.Providers, parent sdktrace.ReadOnlySpan) []string {
	var names []string
	for _, s := range p.Spans() {
		if s.Parent().SpanID() == parent.SpanContext().SpanID() {
			names = append(names, s.Name())
		}
	}
	return names
}

// noPII fails when any span attribute or event carries the given value.
func noPII(t *testing.T, p *telemetrytest.Providers, values ...string) {
	t.Helper()
	for _, s := range p.Spans() {
		kvs := s.Attributes()
		for _, e := range s.Events() {
			kvs = append(kvs, e.Attributes...)
		}
		for _, kv := range kvs {
			for _, v := range values {
				assert.NotContains(t, kv.Value.String(), v, "span %q attribute %s", s.Name(), kv.Key)
			}
		}
	}
}

func TestTracing_SignupNestsServiceAndDatabaseUnderServerSpan(t *testing.T) {
	p, h, _ := tracedServer(t)
	signup(t, h)

	server := serverSpan(t, p, "/auth/signup")
	svc := p.Span(t, "users.Service.Signup")
	telemetrytest.RequireChild(t, server, svc)
	assert.Equal(t, []string{"gorm.query users", "gorm.create users"}, childrenOf(p, svc))

	insert := p.Span(t, "gorm.create users")
	attrs := telemetrytest.KeyValues(insert.Attributes())
	assert.Equal(t, "postgresql", attrs["db.system"])
	assert.Equal(t, "create", attrs["db.operation"])
	assert.Equal(t, "users", attrs["db.sql.table"])
	assert.Equal(t, "1", attrs["db.rows_affected"])
	assert.NotContains(t, attrs, "db.statement", "statements are off by default")
	assert.Equal(t, trace.SpanKindClient, insert.SpanKind())

	assert.NotEmpty(t, telemetrytest.KeyValues(svc.Attributes())["user.id"])
	noPII(t, p, tracingEmail, "correct-horse-battery", "Trace")
}

func TestTracing_UploadCV(t *testing.T) {
	p, h, _ := tracedServer(t)
	token := signup(t, h)

	pdf, err := os.ReadFile("../services/users/testdata/dummy-resume.pdf")
	require.NoError(t, err)
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, err := mw.CreateFormFile("cv", "cv.pdf")
	require.NoError(t, err)
	_, err = io.Copy(part, bytes.NewReader(pdf))
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/cv/", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())

	server := serverSpan(t, p, "/cv")
	serverAttrs := telemetrytest.KeyValues(server.Attributes())
	assert.Equal(t, "jwt", serverAttrs["auth.method"])
	assert.NotEmpty(t, serverAttrs["user.id"])

	// Auth middleware: the session check runs under the server span.
	telemetrytest.RequireChild(t, server, p.Span(t, "users.Service.ValidateSession"))

	upload := p.Span(t, "users.Service.UploadCV")
	telemetrytest.RequireChild(t, server, upload)
	process := p.Span(t, "users.Service.ProcessCV")
	telemetrytest.RequireChild(t, upload, process)
	assert.Equal(t, []string{"extractor.ExtractFromFile", "gorm.create cvs", "gorm.create section_embeddings"}, childrenOf(p, process))

	attrs := telemetrytest.KeyValues(process.Attributes())
	assert.NotEmpty(t, attrs["cv.id"])
	assert.Equal(t, "3", attrs["cv.sections"])
	noPII(t, p, tracingEmail, "engineer")
}

func TestTracing_DatabaseErrorIsRecordedOnceAndMarksAncestors(t *testing.T) {
	p, h, db := tracedServer(t)
	token := signup(t, h)
	require.NoError(t, db.Exec("ALTER TABLE users RENAME TO users_gone").Error)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)

	server := serverSpan(t, p, "/me")
	validate := p.Span(t, "users.Service.ValidateSession")
	telemetrytest.RequireChild(t, server, validate)
	var query sdktrace.ReadOnlySpan
	for _, s := range p.Spans() {
		if s.Parent().SpanID() == validate.SpanContext().SpanID() {
			query = s
		}
	}
	require.NotNil(t, query)
	assert.Equal(t, "gorm.query users", query.Name())

	assert.Equal(t, 1, telemetrytest.Exceptions(query), "the failing statement records the error")
	assert.Equal(t, codes.Error, query.Status().Code)
	for _, s := range []sdktrace.ReadOnlySpan{validate, server} {
		assert.Equal(t, codes.Error, s.Status().Code, "%s status", s.Name())
		assert.Zero(t, telemetrytest.Exceptions(s), "%s must not record the error again", s.Name())
	}
	assert.Equal(t, "*pgconn.PgError", validate.Status().Description, "the status carries a label, not the message")

	total := 0
	for _, s := range p.Spans() {
		if s.SpanContext().TraceID() == server.SpanContext().TraceID() {
			total += telemetrytest.Exceptions(s)
		}
	}
	assert.Equal(t, 1, total, "one error, one exception event per trace")
}
