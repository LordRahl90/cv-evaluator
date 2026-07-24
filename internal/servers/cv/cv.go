package cv

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
	cv := router.Group("cv")
	{
		// we need to use authenticated middleware here, but for now we will just return a dummy CV
		cv.GET("/", h.getUserCV)
		cv.GET("/download/:id", h.downloadCV)
		cv.PATCH("/", h.updateCV)
		cv.POST("/", h.createCV)
		cv.DELETE("/", h.deleteCV)
	}
}

func (h *Handler) getUserCV(c *gin.Context) {

}

func (h *Handler) downloadCV(c *gin.Context) {}

func (h *Handler) updateCV(c *gin.Context) {}

func (h *Handler) createCV(c *gin.Context) {}

func (h *Handler) deleteCV(c *gin.Context) {}
