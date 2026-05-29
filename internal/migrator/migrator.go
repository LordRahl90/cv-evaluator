package migrator

import (
	"fmt"
	"log/slog"

	"cv-solution/internal/models"

	"gorm.io/gorm"
)

var appModels = []interface{}{
	&models.User{},
	&models.CV{},
	&models.Job{},
	&models.SectionEmbedding{},
}

func Migrate(db *gorm.DB) error {
	slog.Info("migrating database")

	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
		slog.Warn("could not create vector extension", "error", err)
	}

	for _, model := range appModels {
		slog.Info(fmt.Sprintf("migrating model %T", model))
		if err := db.AutoMigrate(model); err != nil {
			slog.Error("failed to migrate model", "model", fmt.Sprintf("%T", model), "error", err)
			return err
		}
	}

	slog.Info("migrated database")
	return nil
}
