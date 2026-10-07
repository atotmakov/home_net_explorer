package ingest_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/contract/contracttest"
	"github.com/atotmakov/home_net_explorer/internal/ingest"
	"github.com/atotmakov/home_net_explorer/internal/store"
	"github.com/atotmakov/home_net_explorer/internal/store/storetest"
)

type fakeApplier struct {
	calls      int
	newSubnets []string
}

func (f *fakeApplier) Apply(ctx context.Context, tx *sql.Tx, collectorID int64, run *contract.CollectionRun, receivedAt time.Time) ([]string, error) {
	f.calls++
	return f.newSubnets, nil
}

func decode(t *testing.T, raw []byte) *contract.CollectionRun {
	t.Helper()
	run, err := contract.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIngestStoresRunAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	cid, err := s.EnsureCollector(ctx, "nas", store.KindBuiltin, 900)
	if err != nil {
		t.Fatal(err)
	}
	fa := &fakeApplier{newSubnets: []string{"192.168.1.0/24"}}
	in := ingest.New(s, fa)

	raw := contracttest.Fixture(t, "valid_minimal.json")
	run := decode(t, raw)
	received := time.Date(2026, 10, 5, 10, 1, 3, 0, time.UTC) // sent_at is 10:01:01

	res, err := in.Ingest(ctx, cid, raw, run, received)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != contract.StatusStored || res.CollectionID != run.CollectionID {
		t.Errorf("result = %+v, want stored", res)
	}
	if res.ClockSkewMs != -2000 {
		t.Errorf("clock skew = %d, want -2000 (sent_at - received_at)", res.ClockSkewMs)
	}
	if !reflect.DeepEqual(res.NewSubnets, fa.newSubnets) {
		t.Errorf("new_subnets = %v, want %v from the applier", res.NewSubnets, fa.newSubnets)
	}
	if fa.calls != 1 {
		t.Errorf("applier calls = %d, want 1", fa.calls)
	}

	var gz []byte
	if err := s.DB().QueryRow(`SELECT payload_gz FROM collection_runs WHERE collection_id = ?`, run.CollectionID).Scan(&gz); err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := io.ReadAll(zr)
	if !bytes.Equal(payload, raw) {
		t.Error("payload_gz is not the gzip of the original body")
	}

	var method string
	var complete, probed int
	if err := s.DB().QueryRow(`SELECT method, complete, hosts_probed FROM run_subnets WHERE collection_id = ? AND cidr = ?`,
		run.CollectionID, "192.168.1.0/24").Scan(&method, &complete, &probed); err != nil {
		t.Fatal(err)
	}
	if method != "arp" || complete != 1 || probed != 254 {
		t.Errorf("run_subnets row = %s/%d/%d", method, complete, probed)
	}

	var lastReport string
	var skew int64
	if err := s.DB().QueryRow(`SELECT last_report_at, last_clock_skew_ms FROM collectors WHERE id = ?`, cid).Scan(&lastReport, &skew); err != nil {
		t.Fatal(err)
	}
	if lastReport != store.FormatTime(received) || skew != -2000 {
		t.Errorf("collector last_report_at=%s skew=%d", lastReport, skew)
	}

	// Duplicate upload: no changes, applier not called again (FR-011).
	runs := count(t, s.DB(), `SELECT count(*) FROM collection_runs`)
	subs := count(t, s.DB(), `SELECT count(*) FROM run_subnets`)
	res2, err := in.Ingest(ctx, cid, raw, run, received.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if res2.Status != contract.StatusDuplicate {
		t.Errorf("second upload status = %s, want duplicate", res2.Status)
	}
	if fa.calls != 1 {
		t.Errorf("applier called for duplicate (calls=%d)", fa.calls)
	}
	if count(t, s.DB(), `SELECT count(*) FROM collection_runs`) != runs || count(t, s.DB(), `SELECT count(*) FROM run_subnets`) != subs {
		t.Error("duplicate upload changed rows")
	}
}

// Clock skew is reported and flagged above 5 minutes, never rejected (T059).
func TestClockSkewFlag(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	cid, _ := s.EnsureCollector(ctx, "desktop", store.KindRemote, 900)
	in := ingest.New(s, &fakeApplier{})
	raw := contracttest.Fixture(t, "valid_minimal.json")
	run := decode(t, raw)
	res, err := in.Ingest(ctx, cid, raw, run, run.SentAt.Add(-6*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != contract.StatusStored || res.ClockSkewMs != 360000 {
		t.Errorf("result = %+v, want stored with skew 360000", res)
	}
	if !contract.SkewFlagged(res.ClockSkewMs) || !contract.SkewFlagged(-360000) {
		t.Error("6 minutes of skew must be flagged")
	}
	if contract.SkewFlagged(300000) || contract.SkewFlagged(-300000) {
		t.Error("exactly 5 minutes must not be flagged (threshold: exceeds 5 minutes)")
	}
}

// Ingest never filters: every accepted run is stored unchanged, including skipped entries
// (data-model.md "Ingest never filters").
func TestIngestNeverFilters(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	cid, err := s.EnsureCollector(ctx, "desktop", store.KindRemote, 900)
	if err != nil {
		t.Fatal(err)
	}
	in := ingest.New(s, &fakeApplier{})
	raw := contracttest.Fixture(t, "valid_new_subnet.json")
	run := decode(t, raw)
	if _, err := in.Ingest(ctx, cid, raw, run, time.Date(2026, 10, 5, 12, 1, 2, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM run_subnets WHERE collection_id = ?`, run.CollectionID); n != 2 {
		t.Errorf("run_subnets rows = %d, want 2 (including the skipped entry)", n)
	}
	var reason string
	if err := s.DB().QueryRow(`SELECT skip_reason FROM run_subnets WHERE cidr = '10.0.0.0/16'`).Scan(&reason); err != nil || reason != "too_large" {
		t.Errorf("skipped entry: reason=%q err=%v", reason, err)
	}
}
