package auth_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/auth"
	"github.com/atotmakov/home_net_explorer/internal/store"
	"github.com/atotmakov/home_net_explorer/internal/store/storetest"
)

func TestNewCollectorToken(t *testing.T) {
	tok, hash, err := auth.NewCollectorToken()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil || len(raw) != 32 {
		t.Fatalf("token must be 256-bit base64url, got %q (%v)", tok, err)
	}
	sum := sha256.Sum256([]byte(tok))
	if !bytes.Equal(hash, sum[:]) {
		t.Error("hash must be SHA-256 of the token")
	}
	tok2, _, _ := auth.NewCollectorToken()
	if tok2 == tok {
		t.Error("tokens are not random")
	}
}

func TestCollectorLifecycle(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)

	tok, id, err := auth.CreateCollector(ctx, s.DB(), "desktop", at)
	if err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err := s.DB().QueryRow(`SELECT token_hash FROM collectors WHERE id = ?`, id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(tok))
	if !bytes.Equal(stored, sum[:]) || bytes.Contains(stored, []byte(tok)) {
		t.Error("only the SHA-256 of the token may be stored")
	}

	c, err := auth.Authenticate(ctx, s.DB(), tok)
	if err != nil || c.ID != id || c.Name != "desktop" {
		t.Fatalf("Authenticate = %+v, %v", c, err)
	}
	if _, err := auth.Authenticate(ctx, s.DB(), "not-a-token"); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("unknown token: %v, want ErrInvalidToken", err)
	}
	if _, err := auth.Authenticate(ctx, s.DB(), ""); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("empty token: %v, want ErrInvalidToken", err)
	}

	if _, _, err := auth.CreateCollector(ctx, s.DB(), "desktop", at); !errors.Is(err, auth.ErrCollectorExists) {
		t.Errorf("duplicate name: %v, want ErrCollectorExists", err)
	}
	if _, _, err := auth.CreateCollector(ctx, s.DB(), "Desktop PC", at); !errors.Is(err, auth.ErrBadCollectorName) {
		t.Errorf("bad name: %v, want ErrBadCollectorName", err)
	}

	if err := auth.RevokeCollector(ctx, s.DB(), id, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Authenticate(ctx, s.DB(), tok); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("revoked token: %v, want ErrInvalidToken", err)
	}
}

// Feature 004: a removed collector's token is refused and its name can be reused.
func TestRemovedCollector(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	at := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	tok, id, err := auth.CreateCollector(ctx, s.DB(), "desktop", at)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := auth.CreateCollector(ctx, s.DB(), "desktop", at); !errors.Is(err, auth.ErrCollectorExists) {
		t.Fatalf("duplicate active name: err = %v", err)
	}
	if err := s.Tx(ctx, func(tx *sql.Tx) error {
		_, err := store.RemoveAllCollectors(ctx, tx, at.Add(time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Authenticate(ctx, s.DB(), tok); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("removed collector's token: err = %v, want ErrInvalidToken", err)
	}
	tok2, id2, err := auth.CreateCollector(ctx, s.DB(), "desktop", at.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("reusing a removed collector's name: %v", err)
	}
	if id2 == id {
		t.Error("the new collector reuses the removed one's id")
	}
	if c, err := auth.Authenticate(ctx, s.DB(), tok2); err != nil || c.ID != id2 || c.Removed {
		t.Errorf("new collector: %+v, %v", c, err)
	}
}
