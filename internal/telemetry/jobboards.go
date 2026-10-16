package telemetry

import (
	"context"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// PeerJobBoard is the fallback peer.service for scraper requests to a host
// that is not a known job board.
const PeerJobBoard = "job_board"

// NewJobBoardTransport returns one instrumented transport for all job board
// scrapers. Scrapers built on colly do not pass a request context, so
// peer.service comes from the host (linkedin, jobindex, else job_board) and
// the operation from the path: details for a single posting, search for
// anything else.
func NewJobBoardTransport(base http.RoundTripper) http.RoundTripper {
	return NewTransport(PeerJobBoard, base,
		WithPeerFunc(jobBoardPeer),
		WithOperationFunc(jobBoardOperation),
	)
}

func jobBoardPeer(r *http.Request) string {
	host := strings.ToLower(r.URL.Hostname())
	switch {
	case host == "linkedin.com" || strings.HasSuffix(host, ".linkedin.com"):
		return PeerLinkedIn
	case host == "jobindex.dk" || strings.HasSuffix(host, ".jobindex.dk"):
		return PeerJobindex
	default:
		return ""
	}
}

func jobBoardOperation(r *http.Request) string {
	path := r.URL.Path
	switch jobBoardPeer(r) {
	case PeerLinkedIn:
		if strings.Contains(path, "/jobs/view/") || strings.Contains(path, "/jobPosting/") {
			return OpDetails
		}
	case PeerJobindex:
		if strings.HasPrefix(path, "/jobannonce/") || strings.HasPrefix(path, "/job/") {
			return OpDetails
		}
	}
	return OpSearch
}

// WithParentSpan returns a transport that sends each request through rt with
// the span in ctx as its parent. Clients that build requests without a
// context, such as colly, otherwise start a new trace per request:
//
//	c.WithTransport(telemetry.WithParentSpan(ctx, transport))
//
// Only the span is carried over; cancellation and deadlines of ctx are not.
func WithParentSpan(ctx context.Context, rt http.RoundTripper) http.RoundTripper {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return rt
	}
	return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return rt.RoundTrip(r.WithContext(trace.ContextWithSpan(r.Context(), span)))
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
