package contract_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/collect/collecttest"
	"github.com/atotmakov/home_net_explorer/internal/collect/ports"
	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/contract/contracttest"
	"github.com/atotmakov/home_net_explorer/internal/upload"
)

// The collector side of the contract: what the engine + upload client send must satisfy the
// CollectionRun schema and carry the bearer token.
func TestCollectorPayloadMatchesSchema(t *testing.T) {
	schema := contracttest.Schema(t, "CollectionRun")
	var got [][]byte
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, b)
		auths = append(auths, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(contract.UploadResult{Status: contract.StatusStored})
	}))
	defer srv.Close()

	fnet := collecttest.NewFakeNetwork().
		Add("192.168.1.100", collecttest.Host{MAC: "a0:b1:c2:d3:e4:f5", Hostname: "router.lan"}).
		Add("192.168.8.23", collecttest.Host{MAC: "3c:22:fb:44:55:66", Hostname: "printer.local", HostnameSource: contract.HostnameSourceMDNS})
	e := &collect.Engine{
		Prober: fnet, Presence: fnet, Neighbors: fnet, Resolver: fnet,
		Routes:          collecttest.NewFakeRoutes("192.168.1.100", "home=192.168.1.10/24", "internet=192.168.8.176/24"),
		Clock:           clock.NewFake(time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)),
		Collector:       contract.CollectorInfo{Name: "desktop", Version: "test", OS: "windows"},
		IntervalSeconds: 900,
	}
	run, err := e.Scan(context.Background(), collect.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sp := upload.Spool{Dir: t.TempDir()}
	if _, err := sp.Save(run); err != nil {
		t.Fatal(err)
	}
	if _, err := (&upload.Client{BaseURL: srv.URL, Token: "secret-token"}).Flush(context.Background(), sp); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("uploads = %d, want 1", len(got))
	}
	if err := contracttest.Validate(schema, got[0]); err != nil {
		t.Errorf("collector payload fails the CollectionRun schema: %v\n%s", err, got[0])
	}
	if auths[0] != "Bearer secret-token" {
		t.Errorf("Authorization = %q", auths[0])
	}
}

// Feature 015: the port run the collector's port job builds satisfies the schema and the rules.
func TestCollectorPortRunMatchesSchema(t *testing.T) {
	job := &collect.PortJob{Clock: clock.Real{}, Collector: contract.CollectorInfo{Name: "desktop", Version: "test", OS: "windows"},
		Checker: &ports.Checker{MaxPort: 64, BannerTimeout: 5 * time.Millisecond,
			Dial: func(ctx context.Context, a netip.AddrPort, _ time.Duration) (net.Conn, error) {
				if a.Port() == 22 {
					c, s := net.Pipe()
					go func() { s.Write([]byte("SSH-2.0-test
")); s.Close() }()
					return c, nil
				}
				return nil, errors.New("connection refused")
			},
			UDP: func(context.Context, netip.AddrPort, []byte, time.Duration) ([]byte, error) { return []byte{1}, nil }}}
	run := job.Run(context.Background(), contract.Vantage{}, []ports.Target{
		{IP: netip.MustParseAddr("192.168.1.79"), MAC: "00:00:5e:00:53:79"}, {IP: netip.MustParseAddr("192.168.1.80")}})
	if err := contract.Validate(run); err != nil {
		t.Fatalf("port run invalid: %v", err)
	}
	b, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	if err := contracttest.Validate(contracttest.Schema(t, "CollectionRun"), b); err != nil {
		t.Errorf("port run fails the schema: %v
%s", err, b)
	}
}
