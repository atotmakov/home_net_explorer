package integration_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/contract/contracttest"
)

// Feature 002, US2: the Collectors page shows each collector's latest router read (FR-013,
// SC-005), and nothing the server stores carries router credentials (FR-006).
func TestUS2RouterStatusOnCollectorsPage(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.login()
	desktop := e.createCollector("desktop")
	e.createCollector("laptop")

	if status, _ := e.upload(desktop.Token, contracttest.Fixture(t, "valid_router.json")); status != http.StatusCreated {
		t.Fatalf("upload = %d", status)
	}
	page := e.get("/collectors")
	if !strings.Contains(page, "Router") {
		t.Error("/collectors has no router column")
	}
	if !strings.Contains(page, "huawei-hg8145v5 at 192.168.0.1: OK, 14 online / 16 offline") {
		t.Errorf("/collectors does not show the successful router read:\n%s", page)
	}

	if status, _ := e.upload(desktop.Token, contracttest.Fixture(t, "valid_router_failed.json")); status != http.StatusCreated {
		t.Fatalf("upload = %d", status)
	}
	page = e.get("/collectors")
	if !strings.Contains(page, "huawei-hg8145v5 at 192.168.0.1: login rejected") {
		t.Errorf("/collectors does not show the failed read of the latest run:\n%s", page)
	}
	if strings.Contains(page, "OK, 14 online") {
		t.Error("/collectors still shows the older run's result")
	}
	if n := strings.Count(page, "huawei-hg8145v5"); n != 1 {
		t.Errorf("router shown %d times; the laptop (no router) must show none", n)
	}

	rows, err := e.app.Store.DB().Query(`SELECT payload_gz FROM collection_runs`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var gz []byte
		rows.Scan(&gz)
		r, err := gzip.NewReader(bytes.NewReader(gz))
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(r)
		for _, key := range []string{`"password"`, `"username"`} {
			if bytes.Contains(raw, []byte(key)) {
				t.Errorf("a stored run carries %s", key)
			}
		}
	}
}
