package models

import (
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

type CV struct {
	ID               int `gorm:"primaryKey"`
	UserID           int
	ExtractedContent string          `gorm:"type:text"`
	FullEmbedding    pgvector.Vector `gorm:"type:vector(768)"`

	gorm.Model
}
