package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

var (
	ErrInvalidToken     = errors.New("auth: missing, unknown or revoked collector token")
	ErrCollectorExists  = errors.New("auth: a collector with this name already exists")
	ErrBadCollectorName = errors.New("auth: collector name must match [a-z0-9-]{1,64}")
)

// NewCollectorToken returns a random 256-bit token (base64url) and its SHA-256, which is the
// only form ever stored (FR-008, research R10).
func NewCollectorToken() (token string, hash []byte, err error) {
	token, err = RandomToken()
	if err != nil {
		return "", nil, err
	}
	return token, HashToken(token), nil
}

// HashToken is the stored form of a token.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// CreateCollector registers a remote collector and returns its token, shown to the owner once.
func CreateCollector(ctx context.Context, db *sql.DB, name string, at time.Time) (token string, id int64, err error) {
	if !contract.ValidCollectorName(name) {
		return "", 0, ErrBadCollectorName
	}
	token, hash, err := NewCollectorToken()
	if err != nil {
		return "", 0, err
	}
	res, err := db.ExecContext(ctx, `INSERT INTO collectors (name, kind, token_hash, created_at) VALUES (?, ?, ?, ?)`,
		name, store.KindRemote, hash, store.FormatTime(at))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return "", 0, ErrCollectorExists
		}
		return "", 0, err
	}
	id, err = res.LastInsertId()
	return token, id, err
}

// RevokeCollector disables a collector's token.
func RevokeCollector(ctx context.Context, db *sql.DB, id int64, at time.Time) error {
	_, err := db.ExecContext(ctx, `UPDATE collectors SET revoked_at = ? WHERE id = ? AND kind = ? AND revoked_at IS NULL`,
		store.FormatTime(at), id, store.KindRemote)
	return err
}

// Authenticate finds the active collector owning token. Every active hash is compared in
// constant time, so timing does not reveal how much of a token matched.
func Authenticate(ctx context.Context, db *sql.DB, token string) (store.Collector, error) {
	if token == "" {
		return store.Collector{}, ErrInvalidToken
	}
	want := HashToken(token)
	rows, err := db.QueryContext(ctx, `SELECT id, token_hash FROM collectors WHERE kind = ? AND token_hash IS NOT NULL AND revoked_at IS NULL AND deleted_at IS NULL`, store.KindRemote)
	if err != nil {
		return store.Collector{}, err
	}
	var match int64
	for rows.Next() {
		var id int64
		var hash []byte
		if err := rows.Scan(&id, &hash); err != nil {
			rows.Close()
			return store.Collector{}, err
		}
		if subtle.ConstantTimeCompare(hash, want) == 1 {
			match = id
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return store.Collector{}, err
	}
	if match == 0 {
		return store.Collector{}, ErrInvalidToken
	}
	return store.CollectorByID(ctx, db, match)
}
