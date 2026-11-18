package main

import (
	"context"
	"net/http"
	"testing"

	"cv-evaluator/internal/config"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckRoutes_CoversTheGuestTotalCap(t *testing.T) {
	lim, closeStore, err := newLimits(context.Background(), &config.Config{Environment: config.EnvDevelopment})
	require.NoError(t, err)
	defer closeStore()

	var routes gin.RoutesInfo
	for _, r := range append(lim.perIP, lim.perUser...) {
		routes = append(routes, gin.RouteInfo{Method: r.Method, Path: r.Path})
	}
	assert.NoError(t, lim.checkRoutes(routes), "the guest route is among the per-IP rules")

	lim.guestTotal.Path = "/match/renamed"
	err = lim.checkRoutes(routes)
	require.Error(t, err, "the global cap is no longer middleware but its route is still checked")
	assert.Contains(t, err.Error(), http.MethodPost+" /match/renamed")
}
