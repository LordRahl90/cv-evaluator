package db

import (
	"os"

	"cv-evaluator/internal/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Open connects to Postgres with the given settings.
func Open(cfg config.Database) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{})
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
