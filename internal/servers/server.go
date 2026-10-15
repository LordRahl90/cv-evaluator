package servers

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const defaultShutdownTimeout = 10 * time.Second

// HealthCheck reports whether a dependency the server needs is reachable.
type HealthCheck func(ctx context.Context) error

type Config struct {
	Port            string
	HealthCheck     HealthCheck
	ShutdownTimeout time.Duration
}

type Server struct {
	config *Config
	router *gin.Engine
}

// New creates a server with recovery, request logging and a /healthz route.
// Feature handlers register their routes on Router().
func New(config *Config) *Server {
	if config.ShutdownTimeout <= 0 {
		config.ShutdownTimeout = defaultShutdownTimeout
	}

	router := gin.New()
	router.Use(gin.Recovery(), requestLogger())

	s := &Server{
		config: config,
		router: router,
	}
	router.GET("/healthz", s.healthz)

	return s
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
		if err := s.config.HealthCheck(c.Request.Context()); err != nil {
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
		slog.InfoContext(c.Request.Context(), "request",
			"method", c.Request.Method,
			"path", c.FullPath(),
			"status", c.Writer.Status(),
			"duration", time.Since(start),
		)
	}
}
