package contract_test

import (
	"encoding/json"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Feature 008: hne-collector.json may carry // comments.
func TestStripComments(t *testing.T) {
	in := `{
  // where the server is
  "server_url": "http://192.168.1.100:8181", // trailing comment
  "name": "a // not a comment",
  "token": "q\"//x",
  // "subnets": ["10.0.0.0/24"],
  "subnets": []
}
// end`
	var cfg struct {
		ServerURL string   `json:"server_url"`
		Name      string   `json:"name"`
		Token     string   `json:"token"`
		Subnets   []string `json:"subnets"`
	}
	if err := json.Unmarshal(contract.StripComments([]byte(in)), &cfg); err != nil {
		t.Fatalf("not JSON after stripping comments: %v\n%s", err, contract.StripComments([]byte(in)))
	}
	if cfg.ServerURL != "http://192.168.1.100:8181" || cfg.Name != "a // not a comment" || cfg.Token != `q"//x` || len(cfg.Subnets) != 0 {
		t.Errorf("cfg = %+v (// inside strings must be kept)", cfg)
	}
	plain := `{"a": "b"}`
	if got := string(contract.StripComments([]byte(plain))); got != plain {
		t.Errorf("JSON without comments changed: %q", got)
	}
}
