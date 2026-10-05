package oui

import (
	"fmt"
	"strings"
	"testing"
)

func TestKnownPrefix(t *testing.T) {
	// 00:11:32 is Synology's MA-L block.
	if got := Lookup("00:11:32:aa:bb:cc"); !strings.Contains(got, "Synology") {
		t.Errorf("Lookup(00:11:32:…) = %q, want Synology", got)
	}
}

func TestUnknownPrefix(t *testing.T) {
	// The locally administered range is never assigned by the IEEE.
	if got := Lookup("02:00:00:00:00:01"); got != "" {
		t.Errorf("Lookup(locally administered) = %q, want empty", got)
	}
	if got := Lookup("not a mac"); got != "" {
		t.Errorf("Lookup(garbage) = %q, want empty", got)
	}
}

// MA-M (28-bit) and MA-S (36-bit) blocks are carved out of MA-L blocks owned by the IEEE
// Registration Authority, so a correct table must prefer the longest matching prefix.
func TestLongestPrefixWins(t *testing.T) {
	tab := load()
	for _, bits := range []int{28, 36} {
		found := false
		for prefix, vendor := range tab.byBits[bits] {
			parent := prefix >> uint(bits-24)
			if _, ok := tab.byBits[24][parent]; !ok {
				continue
			}
			mac := macFromPrefix(prefix, bits)
			if got := Lookup(mac); got != vendor {
				t.Errorf("Lookup(%s) = %q, want the %d-bit owner %q", mac, got, bits, vendor)
			}
			found = true
			break
		}
		if !found {
			t.Errorf("no %d-bit block nested in a registered MA-L block; is the table complete?", bits)
		}
	}
}

func TestIsRandomized(t *testing.T) {
	cases := map[string]bool{
		"da:a1:19:01:02:03": true, // 0xda has the locally administered bit (0x02) set
		"02:00:00:00:00:01": true,
		"00:11:32:aa:bb:cc": false,
		"a0:b1:c2:d3:e4:f5": false,
	}
	for mac, want := range cases {
		if got := IsRandomized(mac); got != want {
			t.Errorf("IsRandomized(%s) = %v, want %v", mac, got, want)
		}
	}
}

func TestManufacturerSkipsRandomized(t *testing.T) {
	if got := Manufacturer("da:a1:19:01:02:03"); got != "" {
		t.Errorf("Manufacturer(randomized) = %q, want empty (null for randomized or unknown MACs)", got)
	}
	if got := Manufacturer("00:11:32:aa:bb:cc"); got == "" {
		t.Error("Manufacturer(Synology MAC) is empty")
	}
}

func macFromPrefix(prefix uint64, bits int) string {
	v := prefix << uint(48-bits)
	b := make([]string, 6)
	for i := 0; i < 6; i++ {
		b[i] = fmt.Sprintf("%02x", (v>>uint(40-8*i))&0xff)
	}
	return strings.Join(b, ":")
}
