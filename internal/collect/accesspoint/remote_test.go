package accesspoint

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/collect/accesspoint/eaptest"
	"github.com/atotmakov/home_net_explorer/internal/collect/router"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

type memRejections struct {
	mu  sync.Mutex
	set map[string]bool
}

func (m *memRejections) Rejected(h string) bool { m.mu.Lock(); defer m.mu.Unlock(); return m.set[h] }
func (m *memRejections) Reject(h string)        { m.mu.Lock(); defer m.mu.Unlock(); m.set[h] = true }

// Access points set up in the web UI: the login is fetched before each read, and a rejected
// login is never tried again (spec FR-010, feature 003 FR-014).
func TestRemoteLoginAndRejections(t *testing.T) {
	ap := eaptest.Start(t, eaptest.New, testUser, testPassword)
	ap.SetClients(clients()...)
	password, fetches := "wrong", 0
	var fetchErr error
	login := func(context.Context) (string, router.Secret, error) {
		fetches++
		return testUser, router.Secret(password), fetchErr
	}
	rej := &memRejections{set: map[string]bool{}}
	src := NewRemote(ModelEAP225, netip.MustParseAddr(testAddr), netip.MustParsePrefix("192.168.0.0/24"), login, rej, ap.Transport())
	if src.Model() != ModelEAP225 || src.Address().String() != testAddr {
		t.Errorf("source = %s at %s", src.Model(), src.Address())
	}

	if res := src.Read(context.Background()); res.Outcome != contract.OutcomeLoginRejected {
		t.Fatalf("wrong password = %s", res.Outcome)
	}
	before := len(ap.Requests())
	for i := 0; i < 3; i++ {
		if res := src.Read(context.Background()); res.Outcome != contract.OutcomeSkippedAfterRejection {
			t.Errorf("read after a rejection = %s, want skipped_after_rejection", res.Outcome)
		}
	}
	if n := len(ap.Requests()); n != before {
		t.Errorf("%d requests to the access point after the rejection, want none", n-before)
	}
	if ap.Logins() != 1 {
		t.Errorf("login attempts = %d, want 1", ap.Logins())
	}

	// A new password (saved in the UI) is tried again.
	password = testPassword
	if res := src.Read(context.Background()); res.Outcome != contract.OutcomeOK || len(res.Clients) != 3 {
		t.Errorf("after a new password = %s, %d clients", res.Outcome, len(res.Clients))
	}
	if fetches != 5 {
		t.Errorf("login fetched %d times, want once per read (5)", fetches)
	}

	fetchErr = errors.New("server unreachable")
	if res := src.Read(context.Background()); res.Outcome != contract.OutcomeLoginUnavailable {
		t.Errorf("login fetch failed = %s, want login_unavailable", res.Outcome)
	}
}

func TestRemoteLockIsRecorded(t *testing.T) {
	ap := eaptest.Start(t, eaptest.New, testUser, testPassword)
	ap.SetLocked(true)
	rej := &memRejections{set: map[string]bool{}}
	login := func(context.Context) (string, router.Secret, error) { return testUser, router.Secret(testPassword), nil }
	src := NewRemote(ModelEAP225, netip.MustParseAddr(testAddr), netip.MustParsePrefix("192.168.0.0/24"), login, rej, ap.Transport())
	if res := src.Read(context.Background()); res.Outcome != contract.OutcomeLocked {
		t.Fatalf("locked = %s", res.Outcome)
	}
	if res := src.Read(context.Background()); res.Outcome != contract.OutcomeSkippedAfterRejection {
		t.Errorf("after a lock = %s, want skipped_after_rejection", res.Outcome)
	}
}
