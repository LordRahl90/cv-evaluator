package match

import "github.com/gin-gonic/gin"

type Handler struct {
	router *gin.Engine
}

func New(router *gin.Engine) *Handler {
	return &Handler{
		router: router,
	}
}

func (h *Handler) registerRoutes(router *gin.Engine) {
	match := router.Group("match")
	{
		match.POST("/job-description", h.matchByJobDescription)
		match.POST("job/:id", h.matchByJobId)
		match.POST("job-link", h.matchByJobLink)
	}
}

func (h *Handler) matchByJobDescription(c *gin.Context) {}

func (h *Handler) matchByJobId(c *gin.Context) {}

func (h *Handler) matchByJobLink(c *gin.Context) {}
