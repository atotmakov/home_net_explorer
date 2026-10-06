package auth_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/atotmakov/home_net_explorer/internal/auth"
	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/store/storetest"
)

var t0 = time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)

func TestOwnerPassword(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	o := auth.NewOwner(s.DB(), clock.NewFake(t0))

	if has, err := o.HasPassword(ctx); err != nil || has {
		t.Fatalf("HasPassword = %v, %v; want false", has, err)
	}
	if err := o.SetPassword(ctx, "short"); !errors.Is(err, auth.ErrPasswordTooShort) {
		t.Errorf("SetPassword(short) = %v, want ErrPasswordTooShort", err)
	}
	if err := o.SetPassword(ctx, "correct horse"); err != nil {
		t.Fatal(err)
	}
	if err := o.SetPassword(ctx, "another password"); !errors.Is(err, auth.ErrPasswordAlreadySet) {
		t.Errorf("second SetPassword = %v, want ErrPasswordAlreadySet", err)
	}
	if ok, _ := o.Verify(ctx, "correct horse"); !ok {
		t.Error("Verify(correct) = false")
	}
	if ok, _ := o.Verify(ctx, "wrong horse"); ok {
		t.Error("Verify(wrong) = true")
	}
	var hash string
	if err := s.DB().QueryRow(`SELECT value FROM settings WHERE key = 'owner_password_hash'`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if cost, err := bcrypt.Cost([]byte(hash)); err != nil || cost != 12 {
		t.Errorf("bcrypt cost = %d, %v; want 12", cost, err)
	}
}

func TestOwnerSessions(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	clk := clock.NewFake(t0)
	o := auth.NewOwner(s.DB(), clk)

	token, err := o.NewSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		t.Fatalf("session token must be 256-bit base64url, got %q (%v)", token, err)
	}
	var stored []byte
	if err := s.DB().QueryRow(`SELECT id_hash FROM sessions`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(token))
	if !bytes.Equal(stored, sum[:]) {
		t.Error("sessions.id_hash must be the SHA-256 of the token, not the token")
	}
	if ok, _ := o.ValidSession(ctx, token); !ok {
		t.Error("fresh session invalid")
	}
	if ok, _ := o.ValidSession(ctx, "bogus"); ok {
		t.Error("bogus session valid")
	}
	clk.Advance(30*24*time.Hour + time.Second)
	if ok, _ := o.ValidSession(ctx, token); ok {
		t.Error("session valid after 30 days")
	}

	token2, _ := o.NewSession(ctx)
	if err := o.DeleteSession(ctx, token2); err != nil {
		t.Fatal(err)
	}
	if ok, _ := o.ValidSession(ctx, token2); ok {
		t.Error("deleted session still valid")
	}
}

func TestLoginRateLimit(t *testing.T) {
	clk := clock.NewFake(t0)
	l := auth.NewRateLimiter(5, time.Minute, clk)
	for i := 0; i < 5; i++ {
		if !l.Allow("10.1.1.1") {
			t.Fatalf("attempt %d rejected", i+1)
		}
	}
	if l.Allow("10.1.1.1") {
		t.Error("6th attempt within a minute allowed")
	}
	if !l.Allow("10.1.1.2") {
		t.Error("other client limited")
	}
	clk.Advance(time.Minute + time.Second)
	if !l.Allow("10.1.1.1") {
		t.Error("attempt after the window rejected")
	}
}
