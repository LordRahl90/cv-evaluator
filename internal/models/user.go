package models

import (
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

// User is an account. PasswordChangedAt is when the password was last
// changed (nil if never): session tokens issued before it are rejected.
type User struct {
	ID        ulid.ULID `gorm:"type:bytea;primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`

	FirstName string
	LastName  string
	Email     string
	Phone     string
	Password  string

	PasswordChangedAt *time.Time
}

func (u *User) BeforeCreate(*gorm.DB) error {
	if u.ID == (ulid.ULID{}) {
		u.ID = ulid.Make()
	}

	return nil
}
