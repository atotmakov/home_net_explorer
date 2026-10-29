package ports

import "testing"

// Feature 015 (research R4): usual service names, shipped with the collector (FR-011).
func TestServiceNames(t *testing.T) {
	cases := []struct {
		proto string
		port  int
		want  string
	}{
		{"tcp", 22, "ssh"}, {"tcp", 80, "http"}, {"tcp", 443, "https"}, {"tcp", 445, "smb"},
		{"tcp", 554, "rtsp"}, {"tcp", 631, "ipp"}, {"tcp", 5000, "synology-dsm"},
		{"tcp", 8009, "googlecast"}, {"tcp", 9100, "jetdirect"},
		{"udp", 53, "dns"}, {"udp", 5353, "mdns"}, {"udp", 1900, "ssdp"},
		{"tcp", 40000, ""}, {"udp", 22, ""},
	}
	for _, c := range cases {
		if got := Service(c.proto, c.port); got != c.want {
			t.Errorf("Service(%s, %d) = %q, want %q", c.proto, c.port, got, c.want)
		}
	}
}
