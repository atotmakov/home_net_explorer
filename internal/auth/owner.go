package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Owner password and session rules (research R10).
const (
	MinPasswordLen = 10
	BcryptCost     = 12
	SessionTTL     = 30 * 24 * time.Hour
	passwordKey    = store.SettingOwnerPassword
)

var (
	ErrPasswordTooShort   = errors.New("auth: password must be at least 10 characters")
	ErrPasswordAlreadySet = errors.New("auth: owner password is already set")
)

// Owner manages the single owner account: password and login sessions.
type Owner struct {
	db    *sql.DB
	clock clock.Clock
}

// NewOwner returns an Owner backed by the settings and sessions tables.
func NewOwner(db *sql.DB, clk clock.Clock) *Owner {
	return &Owner{db: db, clock: clk}
}

// HasPassword reports whether first-run setup has been completed.
func (o *Owner) HasPassword(ctx context.Context) (bool, error) {
	var n int
	err := o.db.QueryRowContext(ctx, `SELECT count(*) FROM settings WHERE key = ?`, passwordKey).Scan(&n)
	return n > 0, err
}

// SetPassword sets the owner password once (first-run setup).
func (o *Owner) SetPassword(ctx context.Context, password string) error {
	if len(password) < MinPasswordLen {
		return ErrPasswordTooShort
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return err
	}
	res, err := o.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO NOTHING`, passwordKey, string(hash))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrPasswordAlreadySet
	}
	return nil
}

// Verify checks a password against the stored hash.
func (o *Owner) Verify(ctx context.Context, password string) (bool, error) {
	var hash string
	err := o.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, passwordKey).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil, nil
}

// NewSession creates a login session and returns its token. Only the SHA-256 of the token is
// stored.
func (o *Owner) NewSession(ctx context.Context) (string, error) {
	token, err := RandomToken()
	if err != nil {
		return "", err
	}
	now := o.clock.Now()
	sum := sha256.Sum256([]byte(token))
	_, err = o.db.ExecContext(ctx, `INSERT INTO sessions (id_hash, created_at, expires_at) VALUES (?, ?, ?)`,
		sum[:], store.FormatTime(now), store.FormatTime(now.Add(SessionTTL)))
	if err != nil {
		return "", err
	}
	return token, nil
}

// ValidSession reports whether token belongs to an unexpired session.
func (o *Owner) ValidSession(ctx context.Context, token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	sum := sha256.Sum256([]byte(token))
	var n int
	err := o.db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE id_hash = ? AND expires_at > ?`,
		sum[:], store.FormatTime(o.clock.Now())).Scan(&n)
	return n > 0, err
}

// DeleteSession ends a session (logout).
func (o *Owner) DeleteSession(ctx context.Context, token string) error {
	sum := sha256.Sum256([]byte(token))
	_, err := o.db.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ?`, sum[:])
	return err
}

// RandomToken returns 256 random bits, base64url-encoded without padding.
func RandomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// RateLimiter allows at most limit events per key within a sliding window.
type RateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	clock  clock.Clock
	hits   map[string][]time.Time
}

// NewRateLimiter returns a limiter of limit events per window.
func NewRateLimiter(limit int, window time.Duration, clk clock.Clock) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, clock: clk, hits: map[string][]time.Time{}}
}

// Allow records an event for key and reports whether it is within the limit.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	cutoff := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}
