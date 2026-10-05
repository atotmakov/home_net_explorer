// Package storetest opens throwaway stores for tests.
package storetest

import (
	"path/filepath"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/store"
)

// New opens a fully migrated store in a temporary directory, closed when the test ends.
func New(t testing.TB) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "hne.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
