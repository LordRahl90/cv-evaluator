package models

import (
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

type SectionEmbedding struct {
	ID             ulid.ULID `gorm:"type:bytea;primaryKey"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      gorm.DeletedAt `gorm:"index"`
	UserID         ulid.ULID      `gorm:"type:bytea;index"`
	CVID           ulid.ULID      `gorm:"type:bytea;index"`
	SectionHeading string
	Section        string          `gorm:"type:text"`
	Embedding      pgvector.Vector `gorm:"type:vector(768)"`
}

func (s *SectionEmbedding) BeforeCreate(*gorm.DB) error {
	if s.ID == (ulid.ULID{}) {
		s.ID = ulid.Make()
	}

	return nil
}
