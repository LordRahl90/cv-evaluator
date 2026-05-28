package models

import "gorm.io/gorm"

type SectionEmbedding struct {
	ID             int `gorm:"primaryKey"`
	CVID           int `gorm:"index"`
	SectionHeading string
	Section        string `gorm:"type:text"`
	Embedding      []float64

	gorm.Model
}
