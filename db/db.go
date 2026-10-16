package db

import (
	"os"

	"cv-evaluator/internal/config"
	"cv-evaluator/internal/telemetry"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Open connects to Postgres with the given settings. Every statement is
// traced (see telemetry.NewGormPlugin).
func Open(cfg config.Database) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err := db.Use(telemetry.NewGormPlugin(telemetry.WithDBStatement(cfg.TraceStatements))); err != nil {
		return nil, err
	}
	return db, nil
}

// SetupDatabase connects to Postgres using the DB_* environment variables.
func SetupDatabase() (*gorm.DB, error) {
	return Open(config.Database{
		Host:     os.Getenv("DB_HOST"),
		Port:     os.Getenv("DB_PORT"),
		Name:     os.Getenv("DB_NAME"),
		User:     os.Getenv("DB_USER"),
		Password: os.Getenv("DB_PASSWORD"),
	})
}
