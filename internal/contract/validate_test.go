package contract_test

import (
	"bytes"
	"errors"
	"net/netip"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/contract/contracttest"
)

// Every fixture must be listed here, so a new fixture can't silently go untested.
// Rules (data-model.md "Validation summary"):
//   - "`ip` must be inside a non-skipped subnet of the same run"
//   - "MAC is required when method is `arp`"
//   - "`observed_at` must be within the run window"
//   - "The run window must be no longer than 1 hour and must not be more than 5 minutes in the
//     future relative to `sent_at`"
//   - "No more than 4096 observations and 16 subnets per run"
//   - "`schema_version` must be supported"
//   - "Every subnet `cidr` must be private (RFC 1918) with a prefix from /16 to /30"
//   - `skip_reason` is required when method = `skipped`
var fixtureCodes = map[string]string{
	"valid_minimal.json":                   "",
	"valid_full.json":                      "",
	"valid_routed.json":                    "",
	"valid_new_subnet.json":                "",
	"invalid_mac_format.json":              contract.CodeValidation,
	"invalid_arp_without_mac.json":         contract.CodeValidation,
	"invalid_ip_outside_subnet.json":       contract.CodeValidation,
	"invalid_observed_outside_window.json": contract.CodeValidation,
	"invalid_window_over_1h.json":          contract.CodeValidation,
	"invalid_future_window.json":           contract.CodeValidation,
	"invalid_schema_version_2.json":        contract.CodeUnsupportedSchema,
	"invalid_unknown_field.json":           contract.CodeValidation,
	"invalid_public_subnet.json":           contract.CodeValidation,
	"invalid_skipped_without_reason.json":  contract.CodeValidation,
	"invalid_ip_in_skipped_subnet.json":    contract.CodeValidation,
}

func decodeAndValidate(data []byte) error {
	run, err := contract.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return contract.Validate(run)
}

func checkCode(t *testing.T, label string, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Errorf("%s: unexpected error: %v", label, err)
		}
		return
	}
	var ce *contract.Error
	if !errors.As(err, &ce) {
		t.Errorf("%s: want *contract.Error with code %q, got %v", label, want, err)
		return
	}
	if ce.Code != want {
		t.Errorf("%s: code = %q (%s), want %q", label, ce.Code, ce.Detail, want)
	}
}

func TestValidateFixtures(t *testing.T) {
	all := append(contracttest.Fixtures(t, "valid_"), contracttest.Fixtures(t, "invalid_")...)
	for _, name := range all {
		want, ok := fixtureCodes[name]
		if !ok {
			t.Errorf("fixture %s has no expected result in fixtureCodes", name)
			continue
		}
		checkCode(t, name, decodeAndValidate(contracttest.Fixture(t, name)), want)
	}
}

func TestValidateLimits(t *testing.T) {
	checkCode(t, "4097 observations", decodeAndValidate(contracttest.TooManyObservations(t)), contract.CodeValidation)
	checkCode(t, "17 subnets", decodeAndValidate(contracttest.TooManySubnets(t)), contract.CodeValidation)
}

func TestValidateSubnetRules(t *testing.T) {
	cases := map[string]func(m map[string]any){
		"unmasked cidr": func(m map[string]any) {
			m["subnets"].([]any)[0].(map[string]any)["cidr"] = "192.168.1.7/24"
		},
		"prefix wider than /16": func(m map[string]any) {
			m["subnets"].([]any)[0].(map[string]any)["cidr"] = "192.168.0.0/15"
		},
		"duplicate subnet": func(m map[string]any) {
			s := m["subnets"].([]any)
			m["subnets"] = append(s, s[0])
		},
		"skipped but complete": func(m map[string]any) {
			m["subnets"] = append(m["subnets"].([]any), map[string]any{
				"cidr": "10.9.0.0/16", "method": "skipped", "skip_reason": "too_large",
				"complete": true, "hosts_probed": 0,
			})
		},
		"bad collection id": func(m map[string]any) { m["collection_id"] = "not-a-uuid" },
		"bad collector name": func(m map[string]any) {
			m["collector"].(map[string]any)["name"] = "Desktop PC"
		},
	}
	for label, f := range cases {
		checkCode(t, label, decodeAndValidate(contracttest.Modify(t, "valid_minimal.json", f)), contract.CodeValidation)
	}
}

func TestIsPrivate(t *testing.T) {
	cases := map[string]bool{
		"192.168.1.0/24": true,
		"10.20.30.0/24":  true,
		"172.16.5.0/24":  true,
		"172.32.0.0/24":  false,
		"8.8.8.0/24":     false,
		"10.0.0.0/8":     true,
	}
	for s, want := range cases {
		if got := contract.IsPrivate(netip.MustParsePrefix(s)); got != want {
			t.Errorf("IsPrivate(%s) = %v, want %v", s, got, want)
		}
	}
}
