package contract_test

import (
	"bytes"
	"errors"
	"net/netip"
	"strings"
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
//   - feature 002: `router_table` observations need a MAC; `via` only on `router_table`, at most
//     32 characters; at most 8 `sources`, each with a private address and a known outcome
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
	"valid_router.json":                    "",
	"valid_router_failed.json":             "",
	"valid_router_login_unavailable.json":  "",
	"valid_faked_answers.json":             "", // feature 005
	"invalid_router_bad_outcome.json":      contract.CodeValidation,
	"invalid_source_with_password.json":    contract.CodeValidation,
	"invalid_via_too_long.json":            contract.CodeValidation,
	"invalid_router_without_mac.json":      contract.CodeValidation,
	"invalid_via_on_arp.json":              contract.CodeValidation,
	"invalid_router_public_address.json":   contract.CodeValidation,
	"valid_access_point.json":              "", // feature 012
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
	checkCode(t, "17 sources", decodeAndValidate(contracttest.TooManySources(t)), contract.CodeValidation)
	checkCode(t, "16 sources", decodeAndValidate(contracttest.Sources(t, 16)), "")
}

func TestValidateRouterRules(t *testing.T) {
	obs := func(m map[string]any, i int) map[string]any { return m["observations"].([]any)[i].(map[string]any) }
	src := func(m map[string]any) map[string]any { return m["sources"].([]any)[0].(map[string]any) }
	bad := map[string]func(m map[string]any){
		"router_table observation outside every scanned subnet": func(m map[string]any) { obs(m, 1)["ip"] = "192.168.5.4" },
		"via of 33 characters": func(m map[string]any) { obs(m, 1)["via"] = strings.Repeat("x", 33) },
		"via on an icmp observation": func(m map[string]any) {
			o := obs(m, 0)
			delete(o, "mac")
			o["method"] = "icmp"
			o["via"] = "LAN1"
		},
		"unknown hostname_source":       func(m map[string]any) { obs(m, 1)["hostname_source"] = "dhcp" },
		"unknown source type":           func(m map[string]any) { src(m)["type"] = "switch" },
		"public source address":         func(m map[string]any) { src(m)["address"] = "8.8.8.8" },
		"source address not IPv4":       func(m map[string]any) { src(m)["address"] = "router.lan" },
		"public source subnet":          func(m map[string]any) { src(m)["subnet"] = "8.8.8.0/24" },
		"source subnet too wide":        func(m map[string]any) { src(m)["subnet"] = "10.0.0.0/8" },
		"unknown outcome":               func(m map[string]any) { src(m)["outcome"] = "rebooted" },
		"negative online count":         func(m map[string]any) { src(m)["online"] = -1 },
		"counts on a failed read":       func(m map[string]any) { src(m)["outcome"] = "unreachable" },
		"model too long":                func(m map[string]any) { src(m)["model"] = strings.Repeat("m", 65) },
		"counts with login_unavailable": func(m map[string]any) { src(m)["outcome"] = "login_unavailable" },
	}
	for label, f := range bad {
		checkCode(t, label, decodeAndValidate(contracttest.Modify(t, "valid_router.json", f)), contract.CodeValidation)
	}
	good := map[string]func(m map[string]any){
		"via of 32 characters":         func(m map[string]any) { obs(m, 1)["via"] = strings.Repeat("x", 32) },
		"failed read with zero counts": func(m map[string]any) { src(m)["outcome"], src(m)["online"], src(m)["offline"] = "session_busy", 0, 0 },
		"skipped after rejection": func(m map[string]any) {
			src(m)["outcome"], src(m)["online"], src(m)["offline"] = "skipped_after_rejection", 0, 0
		},
		"router hostname on an arp entry": func(m map[string]any) { obs(m, 0)["hostname_source"] = "router" },
		"no sources at all":               func(m map[string]any) { delete(m, "sources") },
	}
	for label, f := range good {
		checkCode(t, label, decodeAndValidate(contracttest.Modify(t, "valid_router.json", f)), "")
	}
}

// Feature 012: access point client lists (contracts/upload-api-changes.md).
func TestValidateAccessPointRules(t *testing.T) {
	obs := func(m map[string]any, i int) map[string]any { return m["observations"].([]any)[i].(map[string]any) }
	src := func(m map[string]any, i int) map[string]any { return m["sources"].([]any)[i].(map[string]any) }
	const ap = 4 // first ap_client observation of valid_access_point.json
	bad := map[string]func(m map[string]any){
		"ap_client without mac":          func(m map[string]any) { delete(obs(m, ap), "mac") },
		"ap_client without via":          func(m map[string]any) { delete(obs(m, ap), "via") },
		"ap_client without access_point": func(m map[string]any) { delete(obs(m, ap), "access_point") },
		"public access_point":            func(m map[string]any) { obs(m, ap)["access_point"] = "8.8.8.8" },
		"malformed access_point":         func(m map[string]any) { obs(m, ap)["access_point"] = "eap.lan" },
		"ap_client via of 33 characters": func(m map[string]any) { obs(m, ap)["via"] = strings.Repeat("x", 33) },
		"ap_client outside every scanned subnet": func(m map[string]any) { obs(m, ap)["ip"] = "192.168.5.4" },
		"access_point on a router_table observation": func(m map[string]any) { obs(m, 1)["access_point"] = "192.168.0.5" },
		"access_point on an arp observation":         func(m map[string]any) { obs(m, 0)["access_point"] = "192.168.0.5" },
		"unknown source type":                        func(m map[string]any) { src(m, 1)["type"] = "switch" },
		"counts with controller_managed":             func(m map[string]any) { src(m, 1)["outcome"] = "controller_managed" },
	}
	for label, f := range bad {
		checkCode(t, label, decodeAndValidate(contracttest.Modify(t, "valid_access_point.json", f)), contract.CodeValidation)
	}
	good := map[string]func(m map[string]any){
		"ap_client via of 32 characters": func(m map[string]any) { obs(m, ap)["via"] = strings.Repeat("x", 32) },
		"controller_managed with zero counts": func(m map[string]any) {
			src(m, 1)["outcome"], src(m, 1)["online"], src(m, 1)["offline"] = "controller_managed", 0, 0
		},
		"controller_managed on a router source": func(m map[string]any) {
			src(m, 0)["outcome"], src(m, 0)["online"], src(m, 0)["offline"] = "controller_managed", 0, 0
		},
	}
	for _, o := range []string{"unreachable", "login_rejected", "locked", "session_busy", "page_not_understood",
		"skipped_after_rejection", "login_unavailable"} {
		good["access point "+o] = func(m map[string]any) {
			src(m, 1)["outcome"], src(m, 1)["online"], src(m, 1)["offline"] = o, 0, 0
		}
	}
	for label, f := range good {
		checkCode(t, label, decodeAndValidate(contracttest.Modify(t, "valid_access_point.json", f)), "")
	}
	if contract.ObsAPClient != "ap_client" || contract.SourceTypeAccessPoint != "access_point" ||
		contract.OutcomeControllerManaged != "controller_managed" || !contract.ValidOutcome("controller_managed") {
		t.Error("feature 012 constants")
	}
	if len(contract.RouterModels) != 1 || contract.RouterModels[0] != "huawei-hg8145v5" ||
		len(contract.AccessPointModels) != 1 || contract.AccessPointModels[0] != "tplink-eap225" {
		t.Errorf("models: %v / %v", contract.RouterModels, contract.AccessPointModels)
	}
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
