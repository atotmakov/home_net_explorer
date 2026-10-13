package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Feature 005: a subnet a collector skipped because its network path faked answers is a scan
// problem until that collector's next run scans it normally.

func faked(cidr string) contract.SubnetScan {
	return contract.SubnetScan{CIDR: cidr, Method: contract.MethodSkipped, SkipReason: contract.SkipFakedAnswers}
}

func TestScanProblems(t *testing.T) {
	ctx := context.Background()
	h := it.New(t)
	h.Ingest(it.Run{Collector: "desktop", Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24"), faked("192.168.0.0/24")}})
	h.Ingest(it.Run{Collector: "laptop", Subnets: []contract.SubnetScan{faked("192.168.50.0/24")}})

	ps, err := h.Store.ScanProblems(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || ps[0].Collector != "desktop" || ps[0].CIDR != "192.168.0.0/24" || ps[0].Reason != contract.SkipFakedAnswers ||
		ps[1].Collector != "laptop" || ps[1].CIDR != "192.168.50.0/24" || ps[0].At.IsZero() {
		t.Fatalf("problems = %+v", ps)
	}
	ov, err := h.Store.CollectorOverviews(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range ov {
		if o.Name == "desktop" && (len(o.Problems) != 1 || o.Problems[0].CIDR != "192.168.0.0/24") {
			t.Errorf("desktop overview problems = %+v", o.Problems)
		}
	}

	// The desktop's next run scans the subnet normally: its problem is gone.
	h.Ingest(it.Run{Collector: "desktop", Start: it.T0.Add(15 * time.Minute),
		Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24"), {CIDR: "192.168.0.0/24", Method: contract.MethodICMPTCP, Complete: true, HostsProbed: 254}}})
	ps, err = h.Store.ScanProblems(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].Collector != "laptop" {
		t.Errorf("problems after the desktop's normal run = %+v, want only the laptop's", ps)
	}
	// Removed collectors (feature 004): their problems are not shown.
	h.Fact(func(tx *sql.Tx) error { _, err := store.RemoveAllCollectors(ctx, tx, it.T0.Add(time.Hour)); return err })
	ps, err = h.Store.ScanProblems(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 0 {
		t.Errorf("problems after removing the collectors = %+v", ps)
	}
}
