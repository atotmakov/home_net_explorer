package router

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/collect/router/routertest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// T035: the client never follows a redirect (it could point anywhere), and an oversized
// answer is rejected instead of being read into memory.
func TestReadDoesNotFollowRedirects(t *testing.T) {
	var followed atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			followed.Add(1)
			return
		}
		http.Redirect(w, r, "http://192.168.0.99/elsewhere", http.StatusFound)
	}))
	defer srv.Close()
	res := newSource(t, routertest.Redirect(srv.URL), testPassword).Read(context.Background())
	if res.Outcome != contract.OutcomePageNotUnderstood {
		t.Errorf("redirecting router = %s, want page_not_understood", res.Outcome)
	}
	if followed.Load() != 0 {
		t.Error("the client followed a redirect")
	}
}

func TestReadRejectsOversizedList(t *testing.T) {
	f := routertest.New(t, "root", testPassword)
	f.SetList(bytes.Repeat([]byte("x"), maxBody+1))
	res := newSource(t, f.Transport(), testPassword).Read(context.Background())
	if res.Outcome != contract.OutcomePageNotUnderstood {
		t.Errorf("oversized list = %s, want page_not_understood", res.Outcome)
	}
	if f.SessionOpen() {
		t.Error("no logout after an oversized list")
	}
}
