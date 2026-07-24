package match

import (
	"net/http/httptest"
	"strings"

	"cv-evaluator/internal/servers/middleware"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"
)

func newTestRouter(svc JobMatcher, userID ulid.ULID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if userID != (ulid.ULID{}) {
			c.Set(middleware.UserIDKey, userID)
		}
		c.Next()
	})
	New(router, svc)
	return router
}

func noAuthRouter(svc JobMatcher) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	New(router, svc)
	return router
}

func performRequest(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}
