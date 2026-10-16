package users

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cv-evaluator/internal/models"

	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrIncorrectPassword means the password given to confirm an account
	// change does not match the stored one.
	ErrIncorrectPassword = errors.New("incorrect password")
	// ErrSessionRevoked means a session token was issued before the user's
	// password last changed.
	ErrSessionRevoked = errors.New("session revoked")
)

// UpdateProfileRequest is a partial profile update: nil fields are left as
// they are. Values are trimmed and the email is lowercased, as at signup.
// Validation (email format) is the caller's job, as it is for Signup.
type UpdateProfileRequest struct {
	FirstName *string
	LastName  *string
	Email     *string
	Phone     *string
}

// UpdateProfile applies req to the user's profile and returns the result.
// It returns ErrEmailAlreadyUsed or ErrPhoneAlreadyUsed when another
// account has the new email or phone, and ErrUserNotFound when the user
// does not exist.
func (s *Service) UpdateProfile(ctx context.Context, userID ulid.ULID, req UpdateProfileRequest) (*Profile, error) {
	var updated models.User
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", userID).Take(&updated).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return err
		}

		changes := map[string]any{}
		if req.FirstName != nil {
			changes["first_name"] = strings.TrimSpace(*req.FirstName)
		}
		if req.LastName != nil {
			changes["last_name"] = strings.TrimSpace(*req.LastName)
		}
		if req.Email != nil {
			email := strings.ToLower(strings.TrimSpace(*req.Email))
			if email != updated.Email {
				taken, err := s.taken(tx, userID, "email", email)
				if err != nil {
					return err
				}
				if taken {
					return ErrEmailAlreadyUsed
				}
			}
			changes["email"] = email
		}
		if req.Phone != nil {
			phone := strings.TrimSpace(*req.Phone)
			if phone != "" && phone != updated.Phone {
				taken, err := s.taken(tx, userID, "phone", phone)
				if err != nil {
					return err
				}
				if taken {
					return ErrPhoneAlreadyUsed
				}
			}
			changes["phone"] = phone
		}
		if len(changes) == 0 {
			return nil
		}
		if err := tx.Model(&models.User{}).Where("id = ?", userID).Updates(changes).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", userID).Take(&updated).Error
	})
	if err != nil {
		return nil, err
	}

	profile := s.profileFromModel(&updated)
	return &profile, nil
}

// taken reports whether an account other than userID has column = value.
func (s *Service) taken(tx *gorm.DB, userID ulid.ULID, column, value string) (bool, error) {
	var n int64
	err := tx.Model(&models.User{}).
		Where(column+" = ? AND id <> ?", value, userID).
		Count(&n).Error
	return n > 0, err
}

// ChangePassword replaces the user's password after checking current, and
// returns a fresh session. Session tokens issued before the change are
// rejected from then on (see ValidateSession); API keys are not affected.
// It returns ErrIncorrectPassword when current is wrong and ErrUserNotFound
// when the user does not exist. Password rules are the caller's job, as
// they are for Signup.
func (s *Service) ChangePassword(ctx context.Context, userID ulid.ULID, current, next string) (*AuthResponse, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	// The new token's issued-at must not be before password_changed_at, and
	// JWT times have one-second precision: use the same truncated instant.
	changedAt := time.Now().UTC().Truncate(time.Second)

	var user models.User
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", userID).Take(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return err
		}
		if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(current)) != nil {
			return ErrIncorrectPassword
		}
		return tx.Model(&user).Updates(map[string]any{
			"password":            string(hash),
			"password_changed_at": changedAt,
		}).Error
	})
	if err != nil {
		return nil, err
	}

	token, err := s.generateTokenAt(userID, changedAt)
	if err != nil {
		return nil, err
	}
	return &AuthResponse{Token: token, User: s.profileFromModel(&user)}, nil
}

// VerifyPassword checks password against the user's. It returns
// ErrIncorrectPassword when it does not match and ErrUserNotFound when the
// user does not exist.
func (s *Service) VerifyPassword(ctx context.Context, userID ulid.ULID, password string) error {
	var user models.User
	err := s.db.WithContext(ctx).Select("id", "password").Where("id = ?", userID).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrUserNotFound
	}
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)) != nil {
		return ErrIncorrectPassword
	}
	return nil
}

// ValidateSession checks that a session token issued at issuedAt for userID
// is still good: the user exists and has not changed their password since.
// It returns ErrUserNotFound or ErrSessionRevoked otherwise.
//
// JWT times have one-second precision, so a token issued in the same second
// as a password change, but before it, is still accepted.
func (s *Service) ValidateSession(ctx context.Context, userID ulid.ULID, issuedAt time.Time) error {
	var user models.User
	err := s.db.WithContext(ctx).Select("id", "password_changed_at").Where("id = ?", userID).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrUserNotFound
	}
	if err != nil {
		return err
	}
	if user.PasswordChangedAt != nil && issuedAt.Before(user.PasswordChangedAt.Truncate(time.Second)) {
		return ErrSessionRevoked
	}
	return nil
}
