package telemetry

import (
	"net/http"
	"strings"
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
