package upload_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/upload"
)

func run(id string) *contract.CollectionRun {
	t := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	return &contract.CollectionRun{SchemaVersion: 1, CollectionID: id, StartedAt: t, FinishedAt: t, SentAt: t,
		Collector:    contract.CollectorInfo{Name: "desktop", Version: "test", OS: "windows"},
		Vantage:      contract.Vantage{Interfaces: []contract.Interface{}, Routes: []contract.Route{}},
		Subnets:      []contract.SubnetScan{},
		Observations: []contract.Observation{}}
}

func TestSpoolSaveListRemove(t *testing.T) {
	sp := upload.Spool{Dir: filepath.Join(t.TempDir(), "spool")}
	for _, id := range []string{"00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000001"} {
		if _, err := sp.Save(run(id)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(15 * time.Millisecond) // distinct modification times
	}
	if _, err := os.Stat(filepath.Join(sp.Dir, "00000000-0000-4000-8000-000000000002.json")); err != nil {
		t.Fatalf("run not written to spool/<collection_id>.json: %v", err)
	}
	items, err := sp.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "00000000-0000-4000-8000-000000000002" {
		t.Fatalf("pending = %+v, want oldest first", items)
	}
	if err := sp.Remove(items[0].ID); err != nil {
		t.Fatal(err)
	}
	items, _ = sp.Pending()
	if len(items) != 1 {
		t.Errorf("after remove: %d pending", len(items))
	}
}

func TestSpoolIgnoresPartialWrites(t *testing.T) {
	sp := upload.Spool{Dir: t.TempDir()}
	partial := filepath.Join(sp.Dir, ".tmp-crashed.json")
	if err := os.WriteFile(partial, []byte(`{"schema_version": 1, "colle`), 0o600); err != nil {
		t.Fatal(err)
	}
	items, err := sp.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("partial file listed as pending: %+v", items)
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Error("stale partial file not cleaned up")
	}
}

func TestSpoolReject(t *testing.T) {
	sp := upload.Spool{Dir: t.TempDir()}
	id := "00000000-0000-4000-8000-000000000003"
	sp.Save(run(id))
	if err := sp.Reject(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sp.Dir, "rejected", id+".json")); err != nil {
		t.Errorf("rejected run not moved to spool/rejected/: %v", err)
	}
	if items, _ := sp.Pending(); len(items) != 0 {
		t.Error("rejected run still pending")
	}
}
