package users

import "github.com/oklog/ulid/v2"

type User struct {
	ID        ulid.ULID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
}

type AuthResponse struct {
	Token string  `json:"token"`
	User  Profile `json:"user"`
}

type Profile struct {
	ID        ulid.ULID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
}
