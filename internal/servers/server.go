package servers

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Config struct {
	DB   *gorm.DB
	Port string
}

type Server struct {
	config *Config
	router *gin.Engine
}

func New(config *Config) *Server {
	router := gin.New()
	return &Server{
		config: config,
		router: router,
	}
}

func (s *Server) Start(ctx context.Context) error {
	slog.InfoContext(ctx, "starting server", "port", s.config.Port)
	return s.router.Run(":" + s.config.Port)
}
