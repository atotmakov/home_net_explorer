package contract_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/contract/contracttest"
)

// Fixtures whose violation the OpenAPI schema itself can detect. The rest (subnet membership,
// RFC 1918, run window, MAC-required-for-arp) are enforced only by contract.Validate (T008).
var schemaInvalid = []string{
	"invalid_mac_format.json",
	"invalid_schema_version_2.json",
	"invalid_unknown_field.json",
	"invalid_skipped_without_reason.json",
	"invalid_router_bad_outcome.json",
	"invalid_source_with_password.json",
	"invalid_via_too_long.json",
}

func TestValidFixturesMatchSchema(t *testing.T) {
	s := contracttest.Schema(t, "CollectionRun")
	for _, name := range contracttest.Fixtures(t, "valid_") {
		if err := contracttest.Validate(s, contracttest.Fixture(t, name)); err != nil {
			t.Errorf("%s: unexpected schema error: %v", name, err)
		}
	}
}

func TestSchemaDetectableInvalidFixtures(t *testing.T) {
	s := contracttest.Schema(t, "CollectionRun")
	for _, name := range schemaInvalid {
		if err := contracttest.Validate(s, contracttest.Fixture(t, name)); err == nil {
			t.Errorf("%s: schema accepted an invalid document", name)
		}
	}
	if err := contracttest.Validate(s, contracttest.TooManyObservations(t)); err == nil {
		t.Error("schema accepted 4097 observations")
	}
	if err := contracttest.Validate(s, contracttest.TooManySubnets(t)); err == nil {
		t.Error("schema accepted 17 subnets")
	}
	if err := contracttest.Validate(s, contracttest.TooManySources(t)); err == nil {
		t.Error("schema accepted 9 sources")
	}
}

// Marshaling the Go types must produce JSON that still satisfies the schema, so the collector
// can never emit a payload the contract rejects.
func TestGoTypesRoundTrip(t *testing.T) {
	s := contracttest.Schema(t, "CollectionRun")
	for _, name := range contracttest.Fixtures(t, "valid_") {
		run, err := contract.Decode(bytes.NewReader(contracttest.Fixture(t, name)))
		if err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		out, err := json.Marshal(run)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		if err := contracttest.Validate(s, out); err != nil {
			t.Errorf("%s: re-encoded document fails schema: %v\n%s", name, err, out)
		}
	}
}

// Feature 003: the ping lists UI-configured routers without credentials, and the login endpoint
// answers RouterLogin.
func TestPingAndLoginSchemas(t *testing.T) {
	ping := contracttest.Schema(t, "PingResponse")
	ok := []byte(`{"collector":"desktop","server_time":"2026-10-10T10:00:00Z","supported_schema_versions":[1],
		"ignored_subnets":[],"routers":[{"id":1,"model":"huawei-hg8145v5","address":"192.168.0.1","subnet":"192.168.0.0/24"}]}`)
	if err := contracttest.Validate(ping, ok); err != nil {
		t.Errorf("ping with routers: %v", err)
	}
	leak := []byte(`{"collector":"desktop","server_time":"2026-10-10T10:00:00Z","supported_schema_versions":[1],
		"ignored_subnets":[],"routers":[{"id":1,"model":"huawei-hg8145v5","address":"192.168.0.1","subnet":"192.168.0.0/24","password":"x"}]}`)
	if err := contracttest.Validate(ping, leak); err == nil {
		t.Error("ping schema accepted a router entry with a password")
	}
	old := []byte(`{"collector":"desktop","server_time":"2026-10-10T10:00:00Z","supported_schema_versions":[1],"ignored_subnets":[]}`)
	if err := contracttest.Validate(ping, old); err != nil {
		t.Errorf("ping without routers (old server): %v", err)
	}
	login := contracttest.Schema(t, "RouterLogin")
	if err := contracttest.Validate(login, []byte(`{"username":"root","password":"x"}`)); err != nil {
		t.Errorf("router login: %v", err)
	}
	if err := contracttest.Validate(login, []byte(`{"username":"","password":"x"}`)); err == nil {
		t.Error("router login schema accepted an empty username")
	}
}

// Feature 004: a run that started before the owner's data reset is answered "discarded".
func TestUploadResultDiscarded(t *testing.T) {
	res := contracttest.Schema(t, "UploadResult")
	ok := []byte(`{"collection_id":"5a7c1e2d-3b4f-4a6e-9d8c-1f2e3d4c5b6a","status":"discarded","clock_skew_ms":0}`)
	if err := contracttest.Validate(res, ok); err != nil {
		t.Errorf("discarded: %v", err)
	}
	bad := []byte(`{"collection_id":"5a7c1e2d-3b4f-4a6e-9d8c-1f2e3d4c5b6a","status":"ignored","clock_skew_ms":0}`)
	if err := contracttest.Validate(res, bad); err == nil {
		t.Error("UploadResult schema accepted an unknown status")
	}
	if contract.StatusDiscarded != "discarded" {
		t.Errorf("StatusDiscarded = %q", contract.StatusDiscarded)
	}
}
