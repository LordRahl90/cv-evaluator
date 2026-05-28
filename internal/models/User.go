package models

import "gorm.io/gorm"

type User struct {
	ID        int `gorm:"primaryKey"`
	FirstName string
	LastName  string
	Email     string
	Phone     string
	Password  string
	gorm.Model
}
