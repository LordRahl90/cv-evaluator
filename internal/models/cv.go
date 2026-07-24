package models

import (
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

type CV struct {
	ID               ulid.ULID `gorm:"type:bytea;primaryKey"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        gorm.DeletedAt  `gorm:"index"`
	UserID           ulid.ULID       `gorm:"type:bytea;index"`
	ExtractedContent string          `gorm:"type:text"`
	FullEmbedding    pgvector.Vector `gorm:"type:vector(768)"`
}

func (c *CV) BeforeCreate(*gorm.DB) error {
	if c.ID == (ulid.ULID{}) {
		c.ID = ulid.Make()
	}

	return nil
}
