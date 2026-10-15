// Package providers defines the contract job portals implement and a
// registry to look them up by name.
//
// A provider turns search Criteria into job posts (Search) and fetches the
// full description of a single post (Details). Every saved job search runs
// against all registered providers; users do not choose one.
package providers

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"cv-evaluator/internal/entities"
)

// ErrEmptyCriteria is returned by Search when no query is given.
var ErrEmptyCriteria = errors.New("providers: a query is required")

// Criteria describes what to search for. Location is optional. MaxPages
// limits how many result pages are fetched; zero means the provider's default.
type Criteria struct {
	Query, Location string
	MaxPages        int
}

// Provider is a job portal.
type Provider interface {
	// Name is the stable identifier of the provider, e.g. "linkedin". It is
	// stored as the provider of scraped jobs.
	Name() string
	// Search returns the job posts matching c. The posts do not carry details.
	Search(ctx context.Context, c Criteria) ([]entities.JobPost, error)
	// Details fetches the full description of a post returned by Search.
	Details(ctx context.Context, job *entities.JobPost) (*entities.JobDetails, error)
}

// Info describes a provider for people: what it is called, where it lives
// and which market it covers.
type Info struct {
	DisplayName string
	Website     string
	// Coverage is the country or region the portal lists jobs for, e.g.
	// "Denmark" or "Worldwide".
	Coverage string
}

// Describer is implemented by providers that describe themselves for the
// GET /providers endpoint.
type Describer interface {
	Info() Info
}

// Registry looks providers up by name. It is safe for concurrent use once
// built, because it is never modified after NewRegistry returns.
type Registry struct {
	byName map[string]Provider
	names  []string
}

// NewRegistry builds a registry from ps. It panics if two providers share a
// name or a name is empty, since that is a programming error.
func NewRegistry(ps ...Provider) *Registry {
	r := &Registry{byName: make(map[string]Provider, len(ps))}
	for _, p := range ps {
		name := p.Name()
		if name == "" {
			panic("providers: provider with empty name")
		}
		if _, dup := r.byName[name]; dup {
			panic(fmt.Sprintf("providers: duplicate provider %q", name))
		}
		r.byName[name] = p
		r.names = append(r.names, name)
	}
	sort.Strings(r.names)
	return r
}

// Default returns a registry with every built-in provider, each configured
// with opts (for example WithTransport).
func Default(opts ...Option) *Registry {
	return NewRegistry(NewLinkedIn(opts...), NewJobindex(opts...))
}

// Get returns the provider registered under name.
func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.byName[name]
	return p, ok
}

// Names returns the registered provider names in sorted order.
func (r *Registry) Names() []string {
	return append([]string(nil), r.names...)
}
