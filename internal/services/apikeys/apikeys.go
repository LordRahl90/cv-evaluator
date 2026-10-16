// Package apikeys manages personal API keys.
//
// A key looks like "sk_live_" followed by 40 base62 characters (~238 bits of
// entropy). Only the SHA-256 hash of the full key is persisted. Because keys
// are long random strings, a fast unsalted hash is sufficient: there is no
// dictionary to attack, and it lets Verify look a key up by hash in O(1).
package apikeys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/telemetry"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

const (
	// KeyPrefix starts every live secret key.
	KeyPrefix = "sk_live_"
	// secretLength is the number of random base62 characters after KeyPrefix.
	secretLength = 40
	// displayPrefixLength is how many leading characters of the key are kept
	// in clear so users can tell their keys apart (e.g. "sk_live_AbCd").
	displayPrefixLength = len(KeyPrefix) + 4
	// MaxNameLength bounds the user-supplied label.
	MaxNameLength = 100
	// lastUsedResolution throttles last_used_at writes to one per key per window.
	lastUsedResolution = time.Minute

	base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

var (
	ErrNotFound     = errors.New("api key not found")
	ErrInvalidKey   = errors.New("invalid api key")
	ErrNameRequired = errors.New("api key name is required")
	ErrNameTooLong  = fmt.Errorf("api key name must be at most %d characters", MaxNameLength)
)

// Key is the public view of an API key. It never contains the secret.
type Key struct {
	ID         ulid.ULID
	Name       string
	Prefix     string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// CreatedKey is returned once, from Create, and carries the full secret.
type CreatedKey struct {
	Key
	Secret string
}

type Service struct {
	db  *gorm.DB
	now func() time.Time
}

func New(db *gorm.DB) *Service {
	return &Service{db: db, now: time.Now}
}

// Create issues a new key for userID. The returned Secret is not stored and
// cannot be retrieved again.
func (s *Service) Create(ctx context.Context, userID ulid.ULID, name string) (*CreatedKey, error) {
	ctx, span := telemetry.Start(ctx, "apikeys.Service.Create", telemetry.ID(telemetry.AttrUserID, userID))
	defer span.End()
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, span.RecordError(ErrNameRequired)
	}
	if utf8.RuneCountInString(name) > MaxNameLength {
		return nil, span.RecordError(ErrNameTooLong)
	}

	secret, err := generateKey()
	if err != nil {
		return nil, span.RecordError(err)
	}

	record := &models.APIKey{
		UserID:  userID,
		Name:    name,
		Prefix:  secret[:displayPrefixLength],
		KeyHash: hashKey(secret),
	}
	if err := s.db.WithContext(ctx).Create(record).Error; err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}

	return &CreatedKey{Key: toKey(record), Secret: secret}, nil
}

// List returns the caller's active keys, newest first.
func (s *Service) List(ctx context.Context, userID ulid.ULID) ([]Key, error) {
	ctx, span := telemetry.Start(ctx, "apikeys.Service.List", telemetry.ID(telemetry.AttrUserID, userID))
	defer span.End()
	var records []models.APIKey
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}

	keys := make([]Key, 0, len(records))
	for i := range records {
		keys = append(keys, toKey(&records[i]))
	}
	return keys, nil
}

// Delete revokes one of the caller's keys. Keys owned by other users are
// reported as ErrNotFound so their existence is not disclosed.
func (s *Service) Delete(ctx context.Context, userID, keyID ulid.ULID) error {
	ctx, span := telemetry.Start(ctx, "apikeys.Service.Delete", telemetry.ID(telemetry.AttrUserID, userID), telemetry.ID(telemetry.AttrAPIKeyID, keyID))
	defer span.End()
	result := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", keyID, userID).
		Delete(&models.APIKey{})
	if result.Error != nil {
		return fmt.Errorf("delete api key: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return span.RecordError(ErrNotFound)
	}
	return nil
}

// Verify resolves a raw key to its owner. It returns ErrInvalidKey for
// unknown, malformed or revoked keys, and for keys whose owner was deleted.
// On success it records last_used_at (at most once per minute per key).
func (s *Service) Verify(ctx context.Context, rawKey string) (ulid.ULID, error) {
	ctx, span := telemetry.Start(ctx, "apikeys.Service.Verify")
	defer span.End()
	if !strings.HasPrefix(rawKey, KeyPrefix) || len(rawKey) != len(KeyPrefix)+secretLength {
		return ulid.ULID{}, span.RecordError(ErrInvalidKey)
	}

	var record models.APIKey
	err := s.db.WithContext(ctx).
		Select("api_keys.*").
		Joins("JOIN users ON users.id = api_keys.user_id AND users.deleted_at IS NULL").
		Where("api_keys.key_hash = ?", hashKey(rawKey)).
		First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ulid.ULID{}, span.RecordError(ErrInvalidKey)
	}
	if err != nil {
		return ulid.ULID{}, fmt.Errorf("verify api key: %w", err)
	}

	span.SetAttributes(telemetry.ID(telemetry.AttrAPIKeyID, record.ID), telemetry.ID(telemetry.AttrUserID, record.UserID))
	now := s.now()
	if err := s.db.WithContext(ctx).
		Model(&models.APIKey{}).
		Where("id = ? AND (last_used_at IS NULL OR last_used_at < ?)", record.ID, now.Add(-lastUsedResolution)).
		UpdateColumn("last_used_at", now).Error; err != nil {
		return ulid.ULID{}, fmt.Errorf("touch api key: %w", err)
	}

	return record.UserID, nil
}

func toKey(record *models.APIKey) Key {
	return Key{
		ID:         record.ID,
		Name:       record.Name,
		Prefix:     record.Prefix,
		CreatedAt:  record.CreatedAt,
		LastUsedAt: record.LastUsedAt,
	}
}

func hashKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

// generateKey returns KeyPrefix followed by secretLength uniformly random
// base62 characters, using rejection sampling to avoid modulo bias.
func generateKey() (string, error) {
	const maxUnbiased = 256 - (256 % len(base62)) // 248

	var b strings.Builder
	b.Grow(len(KeyPrefix) + secretLength)
	b.WriteString(KeyPrefix)

	buf := make([]byte, secretLength*2)
	for n := 0; n < secretLength; {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("generate api key: %w", err)
		}
		for _, c := range buf {
			if int(c) >= maxUnbiased {
				continue
			}
			b.WriteByte(base62[int(c)%len(base62)])
			n++
			if n == secretLength {
				break
			}
		}
	}

	return b.String(), nil
}
