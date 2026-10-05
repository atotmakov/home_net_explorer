// Package inventorytest builds scripted runs and feeds them through the real ingest + projection
// path, so inventory, store and web tests share one way of creating history.
package inventorytest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/ingest"
	"github.com/atotmakov/home_net_explorer/internal/inventory"
	"github.com/atotmakov/home_net_explorer/internal/store"
	"github.com/atotmakov/home_net_explorer/internal/store/storetest"
)

// T0 is the start of every scripted history.
var T0 = time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)

// Harness is a store with an ingester wired to the real Applier.
type Harness struct {
	T          testing.TB
	Store      *store.Store
	Applier    *inventory.Applier
	Ingester   *ingest.Ingester
	collectors map[string]int64
	seq        int
}

// New returns a harness over a fresh store.
func New(t testing.TB) *Harness {
	t.Helper()
	s := storetest.New(t)
	a := inventory.NewApplier()
	return &Harness{T: t, Store: s, Applier: a, Ingester: ingest.New(s, a), collectors: map[string]int64{}}
}

// Collector returns the id of a named collector, creating it.
func (h *Harness) Collector(name string) int64 {
	h.T.Helper()
	if id, ok := h.collectors[name]; ok {
		return id
	}
	kind := store.KindRemote
	if name == "nas" {
		kind = store.KindBuiltin
	}
	id, err := h.Store.EnsureCollector(context.Background(), name, kind, 900)
	if err != nil {
		h.T.Fatal(err)
	}
	h.collectors[name] = id
	return id
}

// Run describes a scripted collection run.
type Run struct {
	Collector string        // default "nas"
	Start     time.Time     // default T0
	Duration  time.Duration // default 1 minute
	Interval  int           // default 900
	Subnets   []contract.SubnetScan
	Obs       []contract.Observation
	Vantage   *contract.Vantage
}

// Build turns a Run into a contract.CollectionRun with a fresh collection_id. Observations
// without a time are stamped at Start.
func (h *Harness) Build(r Run) *contract.CollectionRun {
	h.seq++
	if r.Collector == "" {
		r.Collector = "nas"
	}
	if r.Start.IsZero() {
		r.Start = T0
	}
	if r.Duration == 0 {
		r.Duration = time.Minute
	}
	if r.Interval == 0 {
		r.Interval = 900
	}
	v := contract.Vantage{Interfaces: []contract.Interface{}, Routes: []contract.Route{}}
	if r.Vantage != nil {
		v = *r.Vantage
	}
	obs := make([]contract.Observation, len(r.Obs))
	for i, o := range r.Obs {
		if o.ObservedAt.IsZero() {
			o.ObservedAt = r.Start
		}
		obs[i] = o
	}
	subnets := append([]contract.SubnetScan{}, r.Subnets...)
	finished := r.Start.Add(r.Duration)
	return &contract.CollectionRun{
		SchemaVersion:   contract.SchemaVersion,
		CollectionID:    fmt.Sprintf("00000000-0000-4000-8000-%012d", h.seq),
		Collector:       contract.CollectorInfo{Name: r.Collector, Version: "test", OS: "linux"},
		StartedAt:       r.Start,
		FinishedAt:      finished,
		SentAt:          finished,
		IntervalSeconds: r.Interval,
		Vantage:         v,
		Subnets:         subnets,
		Observations:    obs,
	}
}

// Ingest builds, validates and ingests a run, received right after it finished.
func (h *Harness) Ingest(r Run) (*contract.CollectionRun, contract.UploadResult) {
	h.T.Helper()
	run := h.Build(r)
	return run, h.IngestRun(run, run.FinishedAt.Add(time.Second))
}

// IngestRun validates and ingests a prepared run.
func (h *Harness) IngestRun(run *contract.CollectionRun, received time.Time) contract.UploadResult {
	h.T.Helper()
	if err := contract.Validate(run); err != nil {
		h.T.Fatalf("scripted run is invalid: %v", err)
	}
	raw, err := json.Marshal(run)
	if err != nil {
		h.T.Fatal(err)
	}
	res, err := h.Ingester.Ingest(context.Background(), h.Collector(run.Collector.Name), raw, run, received)
	if err != nil {
		h.T.Fatalf("ingest: %v", err)
	}
	return res
}

// Fact applies a user fact through the ingester lock (as the web UI does).
func (h *Harness) Fact(f func(tx *sql.Tx) error) {
	h.T.Helper()
	if err := h.Ingester.Do(context.Background(), f); err != nil {
		h.T.Fatal(err)
	}
}

// Scan is a complete ARP scan entry for cidr.
func Scan(cidr string) contract.SubnetScan {
	return contract.SubnetScan{CIDR: cidr, Method: contract.MethodARP, Complete: true, HostsProbed: 254}
}

// Partial is an incomplete ARP scan entry for cidr.
func Partial(cidr string) contract.SubnetScan {
	return contract.SubnetScan{CIDR: cidr, Method: contract.MethodARP, Complete: false, HostsProbed: 10}
}

// TooLarge is a skipped entry for an on-link subnet wider than /22.
func TooLarge(cidr string) contract.SubnetScan {
	return contract.SubnetScan{CIDR: cidr, Method: contract.MethodSkipped, SkipReason: contract.SkipTooLarge}
}

// ARP is an ARP observation; host may be empty.
func ARP(ip, mac, host string) contract.Observation {
	o := contract.Observation{IP: ip, MAC: mac, Method: contract.ObsARP, Hostname: host}
	if host != "" {
		o.HostnameSource = contract.HostnameSourceDNS
	}
	return o
}

// At sets the observation time.
func At(o contract.Observation, t time.Time) contract.Observation {
	o.ObservedAt = t
	return o
}

// Snapshot dumps every projection table (ordered rows as strings) for equality checks.
func (h *Harness) Snapshot() map[string][]string {
	h.T.Helper()
	return SnapshotStore(h.T, h.Store)
}

// SnapshotStore dumps every projection table of s.
func SnapshotStore(t testing.TB, s *store.Store) map[string][]string {
	t.Helper()
	tables := []string{"subnets", "devices", "device_addresses", "sightings", "events", "links"}
	out := map[string][]string{}
	for _, table := range tables {
		rows, err := s.DB().Query(`SELECT * FROM ` + table + ` ORDER BY 1, 2, 3`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			out[table] = append(out[table], fmt.Sprint(vals...))
		}
		rows.Close()
	}
	return out
}
