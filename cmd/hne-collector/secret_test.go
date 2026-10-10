package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/collect/router/routertest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// SC-004 / FR-006: the router username and password never appear in stdout, stderr, the log
// files, the spool (including rejected runs), the rejection marker or any upload.
func TestRouterCredentialsNeverLeak(t *testing.T) {
	stamp := time.Now().UnixNano()
	user := fmt.Sprintf("user-%d", stamp)
	pass := fmt.Sprintf("pw-%d-secret", stamp)
	entry := func() map[string]any {
		return map[string]any{"model": "huawei-hg8145v5", "url": "http://192.168.0.1", "username": user, "password": pass}
	}

	srv := newFakeServer(t)
	f := routertest.New(t, user, pass)
	h := newHarness(t, srv.srv.URL)
	h.routerRT = f.Transport()
	h.withRouters(t, srv.srv.URL, entry())

	var out strings.Builder
	step := func(args ...string) {
		t.Helper()
		h.run(args...)
		fmt.Fprintf(&out, "$ %v\n%s\n%s\n", args, h.stdout.String(), h.stderr.String())
	}
	step("check")
	step("check", "--json")
	step("scan", "--once")
	step("scan", "--once", "--dry-run")
	step("scan", "--once", "--json")

	srv.uploadStatus = http.StatusBadRequest // the run goes to spool/rejected/
	step("scan", "--once")
	srv.uploadStatus = http.StatusCreated

	f.SetMode(routertest.WrongPassword) // writes the rejection marker
	step("scan", "--once")
	f.SetMode(routertest.OK)
	e := entry()
	e["subnet"] = "192.168.0.0/24"
	h.withRouters(t, srv.srv.URL, e)

	// One iteration of `run` (it logs to hne-collector.log).
	ctx, cancel := context.WithCancel(context.Background())
	srv.onUpload = cancel
	h.ctx = ctx
	done := make(chan struct{})
	go func() { step("run"); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		cancel()
		t.Fatal("run did not stop")
	}
	h.ctx = nil
	srv.onUpload = nil

	// A config error must not echo the entry either.
	bad := entry()
	bad["url"] = "http://8.8.8.8"
	h.withRouters(t, srv.srv.URL, bad)
	step("check")
	h.withRouters(t, srv.srv.URL, entry())

	if len(srv.bodies) < 4 {
		t.Fatalf("only %d uploads recorded", len(srv.bodies))
	}
	for i, b := range srv.bodies {
		fmt.Fprintf(&out, "upload %d: %s\n", i, b)
	}
	files := 0
	filepath.WalkDir(h.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(path) == "hne-collector.json" {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files++
		fmt.Fprintf(&out, "file %s: %s\n", path, b)
		return nil
	})
	for _, want := range []string{"hne-collector.log", markerFile, "rejected"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected output %q was not produced, so the search is incomplete", want)
		}
	}
	for _, secret := range []string{user, pass} {
		if strings.Contains(out.String(), secret) {
			i := strings.Index(out.String(), secret)
			lo := max(0, i-200)
			t.Errorf("credential found:\n...%s...", out.String()[lo:min(len(out.String()), i+60)])
		}
	}
	t.Logf("searched %d files, %d uploads and %d bytes of output", files, len(srv.bodies), out.Len())
}

// Feature 003 (FR-010, SC-002): a login fetched from the server for a router set up in the web
// UI appears in no output, no file the collector writes and no upload.
func TestServerRouterLoginNeverLeaks(t *testing.T) {
	stamp := time.Now().UnixNano()
	user := fmt.Sprintf("srv-user-%d", stamp)
	pass := fmt.Sprintf("srv-pw-%d-secret", stamp)
	srv := newFakeServer(t)
	srv.routers = []contract.RouterRef{uiRouter}
	srv.logins[1] = contract.RouterLogin{Username: user, Password: pass}
	f := routertest.New(t, user, pass)
	h := newHarness(t, srv.srv.URL)
	h.routerRT = f.Transport()

	var out strings.Builder
	step := func(args ...string) {
		t.Helper()
		h.run(args...)
		fmt.Fprintf(&out, "$ %v\n%s\n%s\n", args, h.stdout.String(), h.stderr.String())
	}
	step("check")
	step("check", "--json")
	step("scan", "--once")
	step("scan", "--once", "--dry-run")
	f.SetMode(routertest.WrongPassword) // rejection marker
	step("scan", "--once")
	f.SetMode(routertest.OK)
	srv.logins[1] = contract.RouterLogin{Username: user, Password: pass + "-2"}
	f2 := routertest.New(t, user, pass+"-2")
	h.routerRT = f2.Transport()

	ctx, cancel := context.WithCancel(context.Background())
	srv.onUpload = cancel
	h.ctx = ctx
	done := make(chan struct{})
	go func() { step("run"); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		cancel()
		t.Fatal("run did not stop")
	}
	h.ctx, srv.onUpload = nil, nil

	writeConfig(t, h.dir, map[string]any{"server_url": closedURL(t), "name": "desktop", "token": "tok", "subnets": []string{}})
	step("scan", "--once") // server down: cached list, login unavailable

	for i, b := range srv.bodies {
		fmt.Fprintf(&out, "upload %d: %s\n", i, b)
	}
	filepath.WalkDir(h.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := os.ReadFile(path)
		fmt.Fprintf(&out, "file %s: %s\n", path, b)
		return nil
	})
	for _, want := range []string{"hne-collector.routers", markerFile, "hne-collector.log"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected %q to be produced, so the search is incomplete", want)
		}
	}
	for _, secret := range []string{user, pass} {
		if i := strings.Index(out.String(), secret); i >= 0 {
			t.Errorf("credential found:\n...%s...", out.String()[max(0, i-200):min(out.Len(), i+60)])
		}
	}
}
