package models

import (
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

type SectionEmbedding struct {
	UserID         int `gorm:"index"`
	CVID           int `gorm:"index"`
	SectionHeading string
	Section        string          `gorm:"type:text"`
	Embedding      pgvector.Vector `gorm:"type:vector(768)"`

	gorm.Model
}
