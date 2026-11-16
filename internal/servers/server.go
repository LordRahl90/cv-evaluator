package servers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"cv-evaluator/internal/servers/apierror"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultShutdownTimeout = 10 * time.Second

	// DefaultMaxBodyBytes caps every request body. It leaves room for a CV
	// upload (see cv.MaxUploadBytes) plus multipart overhead; JSON routes
	// cap their own text fields far below this.
	DefaultMaxBodyBytes = 6 << 20

	// The HTTP server timeouts. Writes allow for the slowest handler: a CV
	// upload or match waits on the LLM for up to about two minutes.
	readTimeout   = time.Minute
	writeTimeout  = 5 * time.Minute
	idleTimeout   = 2 * time.Minute
	healthTimeout = 2 * time.Second

	// serviceName is the server name otelgin reports on request spans.
	serviceName = "cv-evaluator"
	healthPath  = "/healthz"
)

// HealthCheck reports whether a dependency the server needs is reachable.
type HealthCheck func(ctx context.Context) error

type Config struct {
	Port            string
	HealthCheck     HealthCheck
	ShutdownTimeout time.Duration
	// MaxBodyBytes caps request bodies; DefaultMaxBodyBytes when zero.
	MaxBodyBytes int64
	// TrustedProxies are the proxy IPs or CIDRs whose X-Forwarded-For is
	// believed when working out the client IP. Empty trusts none, so the
	// client IP is the connection's peer address.
	TrustedProxies []string
}

type Server struct {
	config *Config
	router *gin.Engine
}

// New creates a server with tracing, recovery, request logging, security
// headers, a request body limit and a /healthz route. Tracing runs outermost
// so that a recovered panic is recorded as a 500 on the request span;
// /healthz is not traced. Feature handlers register their routes on Router().
func New(config *Config) (*Server, error) {
	if config.ShutdownTimeout <= 0 {
		config.ShutdownTimeout = defaultShutdownTimeout
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = DefaultMaxBodyBytes
	}

	router := gin.New()
	if err := router.SetTrustedProxies(config.TrustedProxies); err != nil {
		return nil, err
	}
	// Multipart uploads stay in memory: the body limit already bounds them,
	// and nothing spills into temp files on disk.
	router.MaxMultipartMemory = config.MaxBodyBytes
	router.Use(
		otelgin.Middleware(serviceName, otelgin.WithFilter(func(r *http.Request) bool {
			return r.URL.Path != healthPath
		})),
		gin.Recovery(),
		requestLogger(),
		securityHeaders(),
		limitBody(config.MaxBodyBytes),
	)

	s := &Server{
		config: config,
		router: router,
	}
	router.GET(healthPath, s.healthz)

	return s, nil
}

// Router returns the engine that feature handlers register routes on.
func (s *Server) Router() *gin.Engine {
	return s.router
}

// Start serves HTTP until ctx is cancelled, then shuts down gracefully,
// letting in-flight requests finish within the shutdown timeout.
func (s *Server) Start(ctx context.Context) error {
	srv := &http.Server{
		Addr:              ":" + s.config.Port,
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}

	return s.serve(ctx, srv, ln)
}

func (s *Server) serve(ctx context.Context, srv *http.Server, ln net.Listener) error {
	errCh := make(chan error, 1)
	go func() {
		slog.InfoContext(ctx, "starting server", "addr", ln.Addr().String())
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return <-errCh
}

func (s *Server) healthz(c *gin.Context) {
	if s.config.HealthCheck != nil {
		// A hung database must fail the probe, not hang it.
		ctx, cancel := context.WithTimeout(c.Request.Context(), healthTimeout)
		defer cancel()
		if err := s.config.HealthCheck(ctx); err != nil {
			slog.ErrorContext(c.Request.Context(), "health check failed", "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		attrs := []any{
			"method", c.Request.Method,
			"path", c.FullPath(),
			"status", c.Writer.Status(),
			"duration", time.Since(start),
		}
		if sc := trace.SpanContextFromContext(c.Request.Context()); sc.IsValid() {
			attrs = append(attrs, "trace_id", sc.TraceID().String())
		}
		slog.InfoContext(c.Request.Context(), "request", attrs...)
	}
}

// securityHeaders sets headers for API responses, which are JSON or file
// downloads and are never rendered as pages or framed.
func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		c.Next()
	}
}

// limitBody rejects bodies larger than maxBytes. A declared Content-Length
// over the limit is refused up front; otherwise reads past the limit fail,
// which handlers report as a bad request.
func limitBody(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			apierror.Abort(c, http.StatusRequestEntityTooLarge, apierror.InvalidRequest(
				apierror.CodeBodyTooLarge, fmt.Sprintf("The request body must be at most %d MB.", maxBytes>>20), ""))
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
