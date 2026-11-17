package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"cv-evaluator/internal/config"
	"cv-evaluator/internal/servers/ratelimit"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// limits holds every rate limit the API enforces.
type limits struct {
	// perIP applies to the public routes, keyed by client IP.
	perIP []ratelimit.Rule
	// global applies to public routes across all callers together, to bound
	// the total cost of routes anyone may call.
	global []ratelimit.Rule
	// perUser applies to authenticated routes, keyed by user.
	perUser []ratelimit.Rule
	// signinPerEmail caps sign-in attempts per account.
	signinPerEmail *ratelimit.Limiter
	// coverLetters caps cover letters per user.
	coverLetters *ratelimit.Limiter
}

// newLimits builds the API's rate limits. With REDIS_URL they are shared by
// every replica; without it each process counts on its own.
func newLimits(ctx context.Context, cfg *config.Config) (*limits, func(), error) {
	newLimiter := func(name string, limit int, window time.Duration) *ratelimit.Limiter {
		return ratelimit.New(limit, window)
	}
	closeStore := func() {}

	if cfg.RedisURL != "" {
		opts, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			return nil, nil, err
		}
		client := redis.NewClient(opts)
		closeStore = func() { _ = client.Close() }
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := client.Ping(pingCtx).Err(); err != nil {
			// Limits fail open, so the API can still serve; make it loud.
			slog.Error("rate limit store unreachable; limits are not enforced until it is back", "error", err)
		}
		store := ratelimit.NewRedisStore(client)
		newLimiter = func(name string, limit int, window time.Duration) *ratelimit.Limiter {
			return ratelimit.NewShared(store, name, limit, window)
		}
	} else if !cfg.IsDevelopment() {
		slog.Warn("REDIS_URL not set; rate limits are per process and reset on restart")
	}

	// Routes that call the LLM or the embeddings API cost money per call;
	// the rest guard against password guessing.
	matches := newLimiter("matches", 30, time.Hour)
	passwordChecks := newLimiter("password-checks", 5, 15*time.Minute)

	l := &limits{
		perIP: []ratelimit.Rule{
			{Method: http.MethodPost, Path: "/auth/signin", Limiter: newLimiter("signin-ip", 60, time.Minute), What: "sign-in attempts"},
			{Method: http.MethodPost, Path: "/auth/signup", Limiter: newLimiter("signup-ip", 20, time.Hour), What: "sign-ups"},
			{Method: http.MethodPost, Path: "/match/guest", Limiter: newLimiter("guest-matches-ip", 5, time.Hour), What: "guest matches",
				Message: "You have used your guest matches for now (at most 5 per hour). Create an account to keep matching."},
		},
		// Each guest match is a paid LLM call from someone without an
		// account. Behind a proxy that hides client IPs the per-IP limit acts
		// almost globally too; this cap bounds the daily cost either way.
		global: []ratelimit.Rule{
			{Method: http.MethodPost, Path: "/match/guest", Limiter: newLimiter("guest-matches", 300, 24*time.Hour), What: "guest matches",
				Message: "Guest matching is busy right now. Try again later, or create an account to keep matching."},
		},
		perUser: []ratelimit.Rule{
			{Method: http.MethodPost, Path: "/cv/", Limiter: newLimiter("cv-uploads", 10, time.Hour), What: "CV uploads"},
			{Method: http.MethodPost, Path: "/match/job-description", Limiter: matches, What: "matches"},
			{Method: http.MethodPost, Path: "/match/job/:id", Limiter: matches, What: "matches"},
			{Method: http.MethodPost, Path: "/results/:id/re-evaluate", Limiter: newLimiter("re-evaluations", 30, time.Hour), What: "re-evaluations"},
			{Method: http.MethodPost, Path: "/job-searches", Limiter: newLimiter("new-searches", 20, 24*time.Hour), What: "new job searches"},
			{Method: http.MethodPost, Path: "/job-searches/:id/run", Limiter: newLimiter("search-runs", 10, time.Hour), What: "search runs"},
			{Method: http.MethodPost, Path: "/portal-suggestions", Limiter: newLimiter("portal-suggestions", 10, 24*time.Hour), What: "portal suggestions"},
			{Method: http.MethodPost, Path: "/me/password", Limiter: passwordChecks, What: "password attempts"},
			{Method: http.MethodDelete, Path: "/me", Limiter: passwordChecks, What: "password attempts"},
		},
		signinPerEmail: newLimiter("signin-email", 10, 15*time.Minute),
		coverLetters:   newLimiter("cover-letters", 5, time.Minute),
	}
	return l, closeStore, nil
}

// checkRoutes fails when a rule names a route that is not registered, so a
// renamed route cannot silently lose its limit.
func (l *limits) checkRoutes(routes gin.RoutesInfo) error {
	registered := make(map[string]bool, len(routes))
	for _, r := range routes {
		registered[r.Method+" "+r.Path] = true
	}
	var errs []error
	for _, r := range slices.Concat(l.perIP, l.global, l.perUser) {
		if !registered[r.Method+" "+r.Path] {
			errs = append(errs, fmt.Errorf("rate limit for %s %s: no such route", r.Method, r.Path))
		}
	}
	return errors.Join(errs...)
}
