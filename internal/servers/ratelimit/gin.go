package ratelimit

import (
	"fmt"
	"math"
	"time"

	"cv-evaluator/internal/servers/apierror"
	"cv-evaluator/internal/servers/middleware"

	"github.com/gin-gonic/gin"
)

// Rule limits one route: Method and Path as registered with gin (for
// example "POST" and "/job-searches/:id/run"). What names the action in the
// error message, for example "matches". Message, when set, replaces the
// whole error message.
type Rule struct {
	Method  string
	Path    string
	Limiter *Limiter
	What    string
	Message string
}

// ByUser enforces rules per authenticated user. Pass it to
// middleware.WithAfterAuth so it runs once the user is known.
func ByUser(rules ...Rule) gin.HandlerFunc {
	return enforce(rules, func(c *gin.Context) string {
		id, ok := middleware.UserID(c)
		if !ok {
			return ""
		}
		return id.String()
	})
}

// ByClientIP enforces rules per client IP. The IP is only as good as the
// server's trusted proxy setting; see servers.Config.TrustedProxies.
func ByClientIP(rules ...Rule) gin.HandlerFunc {
	return enforce(rules, func(c *gin.Context) string { return c.ClientIP() })
}

// globalKey is the one key Global counts under.
const globalKey = "all"

// Global enforces rules across all callers together: every request to a
// rule's route counts against the same limit. It bounds the total cost of a
// public route whatever the callers' IPs. Register it after ByClientIP, so
// requests one caller is refused do not use up everyone's allowance.
func Global(rules ...Rule) gin.HandlerFunc {
	return enforce(rules, func(*gin.Context) string { return globalKey })
}

func enforce(rules []Rule, keyOf func(*gin.Context) string) gin.HandlerFunc {
	byRoute := make(map[string][]Rule, len(rules))
	for _, r := range rules {
		byRoute[r.Method+" "+r.Path] = append(byRoute[r.Method+" "+r.Path], r)
	}
	return func(c *gin.Context) {
		matching := byRoute[c.Request.Method+" "+c.FullPath()]
		if len(matching) == 0 {
			return
		}
		key := keyOf(c)
		if key == "" {
			return
		}
		for _, r := range matching {
			if ok, retryAfter := r.Limiter.AllowContext(c.Request.Context(), key); !ok {
				if r.Message != "" {
					apierror.TooManyRequests(c, r.Message, retryAfter)
					c.Abort()
					return
				}
				Reject(c, r.Limiter, r.What, retryAfter)
				return
			}
		}
	}
}

// Reject aborts the request with a 429 that says how long to wait.
func Reject(c *gin.Context, l *Limiter, what string, retryAfter time.Duration) {
	apierror.TooManyRequests(c, fmt.Sprintf("Too many %s in a short time (at most %d per %s). Try again in %d seconds.",
		what, l.Limit(), WindowText(l.Window()), int(math.Ceil(retryAfter.Seconds()))), retryAfter)
	c.Abort()
}

// WindowText describes a window for people: "minute", "hour", "15m0s".
func WindowText(d time.Duration) string {
	switch d {
	case time.Minute:
		return "minute"
	case time.Hour:
		return "hour"
	case 24 * time.Hour:
		return "day"
	}
	return d.String()
}
