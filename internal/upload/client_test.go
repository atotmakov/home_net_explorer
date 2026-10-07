package upload_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/upload"
)

func TestBackoff(t *testing.T) {
	b := upload.NewBackoff()
	want := []time.Duration{1, 2, 4, 8, 16, 32, 60, 60}
	for i, w := range want {
		if got := b.Next(); got != w*time.Minute {
			t.Fatalf("step %d: %v, want %v (exponential, 1 minute up to 1 hour)", i, got, w*time.Minute)
		}
	}
	b.Reset()
	if got := b.Next(); got != time.Minute {
		t.Errorf("after reset: %v", got)
	}
}

// server answers uploads with the given status and records bearer tokens.
func server(t *testing.T, status int, body any) (*httptest.Server, *[]string) {
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &auths
}

func spoolWith(t *testing.T, ids ...string) upload.Spool {
	sp := upload.Spool{Dir: t.TempDir()}
	for _, id := range ids {
		if _, err := sp.Save(run(id)); err != nil {
			t.Fatal(err)
		}
	}
	return sp
}

const id1 = "00000000-0000-4000-8000-000000000011"

func TestFlushUploadsAndRemoves(t *testing.T) {
	srv, auths := server(t, http.StatusCreated, contract.UploadResult{CollectionID: id1, Status: contract.StatusStored})
	sp := spoolWith(t, id1)
	c := &upload.Client{BaseURL: srv.URL, Token: "tok"}
	res, err := c.Flush(context.Background(), sp)
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 {
		t.Errorf("uploaded = %d", res.Uploaded)
	}
	if items, _ := sp.Pending(); len(items) != 0 {
		t.Error("uploaded run still in the spool")
	}
	if (*auths)[0] != "Bearer tok" {
		t.Errorf("Authorization = %q", (*auths)[0])
	}
}

func TestFlushDuplicateCountsAsDone(t *testing.T) {
	srv, _ := server(t, http.StatusOK, contract.UploadResult{CollectionID: id1, Status: contract.StatusDuplicate})
	sp := spoolWith(t, id1)
	if _, err := (&upload.Client{BaseURL: srv.URL, Token: "tok"}).Flush(context.Background(), sp); err != nil {
		t.Fatal(err)
	}
	if items, _ := sp.Pending(); len(items) != 0 {
		t.Error("duplicate run should be removed from the spool")
	}
}

func TestFlushTokenRejectedKeepsFile(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv, _ := server(t, status, contract.ErrorResponse{Error: contract.CodeInvalidToken})
		sp := spoolWith(t, id1)
		_, err := (&upload.Client{BaseURL: srv.URL, Token: "tok"}).Flush(context.Background(), sp)
		if !errors.Is(err, upload.ErrTokenRejected) {
			t.Errorf("status %d: err = %v, want ErrTokenRejected", status, err)
		}
		if items, _ := sp.Pending(); len(items) != 1 {
			t.Errorf("status %d: run must stay in the spool", status)
		}
	}
}

func TestFlushNetworkErrorKeepsFile(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	l.Close()
	sp := spoolWith(t, id1)
	_, err := (&upload.Client{BaseURL: "http://" + addr, Token: "tok"}).Flush(context.Background(), sp)
	if err == nil || errors.Is(err, upload.ErrTokenRejected) {
		t.Fatalf("err = %v, want a network error", err)
	}
	if items, _ := sp.Pending(); len(items) != 1 {
		t.Error("network error must keep the run for retry")
	}
}

func TestFlushRejectedPayloadMovesFile(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity} {
		srv, _ := server(t, status, contract.ErrorResponse{Error: contract.CodeValidation, Detail: "observations[0]: bad ip"})
		sp := spoolWith(t, id1)
		var logs bytes.Buffer
		c := &upload.Client{BaseURL: srv.URL, Token: "tok", Log: slog.New(slog.NewTextHandler(&logs, nil))}
		res, err := c.Flush(context.Background(), sp)
		if err != nil {
			t.Fatalf("status %d: %v", status, err)
		}
		if res.Rejected != 1 {
			t.Errorf("status %d: rejected = %d", status, res.Rejected)
		}
		if _, err := os.Stat(filepath.Join(sp.Dir, "rejected", id1+".json")); err != nil {
			t.Errorf("status %d: not moved to spool/rejected/", status)
		}
		if status != http.StatusRequestEntityTooLarge && !strings.Contains(logs.String(), "bad ip") {
			t.Errorf("status %d: server detail not logged: %s", status, logs.String())
		}
	}
}

func TestPing(t *testing.T) {
	srv, _ := server(t, http.StatusOK, contract.PingResponse{Collector: "desktop", SupportedSchemaVersions: []int{1},
		IgnoredSubnets: []string{"10.20.30.0/24"}})
	p, err := (&upload.Client{BaseURL: srv.URL, Token: "tok"}).Ping(context.Background())
	if err != nil || p.Collector != "desktop" || len(p.IgnoredSubnets) != 1 {
		t.Errorf("ping = %+v, %v", p, err)
	}
	srv401, _ := server(t, http.StatusUnauthorized, contract.ErrorResponse{Error: contract.CodeInvalidToken})
	if _, err := (&upload.Client{BaseURL: srv401.URL, Token: "bad"}).Ping(context.Background()); !errors.Is(err, upload.ErrTokenRejected) {
		t.Errorf("ping with a rejected token: %v", err)
	}
}
