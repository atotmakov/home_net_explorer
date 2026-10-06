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
