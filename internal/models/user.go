package models

import "gorm.io/gorm"

type User struct {
	FirstName string
	LastName  string
	Email     string
	Phone     string
	Password  string

	gorm.Model
}
