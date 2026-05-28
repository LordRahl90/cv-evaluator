package migrator

import (
	"cv-solution/internal/models"
	"fmt"
	"log/slog"

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

	for _, model := range appModels {
		slog.Info(fmt.Sprintf("migrating model %T", model))
		if err := db.AutoMigrate(model); err != nil {
			slog.Warn(fmt.Sprintf("could not migrate %T: %v", model, err))
		}
		slog.Info(fmt.Sprintf("migrated model %T", model))
	}

	slog.Info("migrated database, starting the seeder")
	return nil
}
