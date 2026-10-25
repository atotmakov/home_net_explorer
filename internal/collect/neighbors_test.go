package collect

import (
	"encoding/binary"
	"net/netip"
	"slices"
	"testing"
)

// The NAS reported the Dune player at 192.168.1.43 for six days from a STALE entry of its ARP
// cache (/proc/net/arp shows no state): only entries the kernel recently confirmed count.

func ips(ns []Neighbor) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.IP.String()+"="+n.MAC)
	}
	slices.Sort(out)
	return out
}

// nlNeigh builds one RTM_NEWNEIGH netlink message (nlmsghdr, ndmsg, NDA_DST, NDA_LLADDR).
func nlNeigh(family byte, state uint16, ip []byte, mac []byte) []byte {
	attr := func(typ uint16, data []byte) []byte {
		b := make([]byte, 4, 4+len(data)+3)
		binary.NativeEndian.PutUint16(b[0:], uint16(4+len(data)))
		binary.NativeEndian.PutUint16(b[2:], typ)
		b = append(b, data...)
		for len(b)%4 != 0 {
			b = append(b, 0)
		}
		return b
	}
	nd := make([]byte, 12)
	nd[0] = family
	binary.NativeEndian.PutUint32(nd[4:], 2) // ifindex
	binary.NativeEndian.PutUint16(nd[8:], state)
	body := append(nd, attr(1, ip)...)
	if mac != nil {
		body = append(body, attr(2, mac)...)
	}
	h := make([]byte, 16)
	binary.NativeEndian.PutUint32(h[0:], uint32(16+len(body)))
	binary.NativeEndian.PutUint16(h[4:], 28) // RTM_NEWNEIGH
	return append(h, body...)
}

func TestParseNetlinkNeighbors(t *testing.T) {
	mac := func(last byte) []byte { return []byte{0x14, 0xc0, 0x89, 0x10, 0x83, last} }
	var dump []byte
	dump = append(dump, nlNeigh(2, 0x02, []byte{192, 168, 1, 9}, mac(1))...)   // REACHABLE
	dump = append(dump, nlNeigh(2, 0x04, []byte{192, 168, 1, 43}, mac(2))...)  // STALE: the Dune's old address
	dump = append(dump, nlNeigh(2, 0x08, []byte{192, 168, 1, 20}, mac(3))...)  // DELAY
	dump = append(dump, nlNeigh(2, 0x10, []byte{192, 168, 1, 21}, mac(4))...)  // PROBE
	dump = append(dump, nlNeigh(2, 0x20, []byte{192, 168, 1, 22}, mac(5))...)  // FAILED
	dump = append(dump, nlNeigh(2, 0x01, []byte{192, 168, 1, 23}, nil)...)     // INCOMPLETE, no MAC
	dump = append(dump, nlNeigh(2, 0x80, []byte{192, 168, 1, 24}, mac(7))...)  // PERMANENT
	dump = append(dump, nlNeigh(10, 0x02, make([]byte, 16), mac(8))...)        // IPv6: skipped
	done := make([]byte, 20)
	binary.NativeEndian.PutUint32(done[0:], 20)
	binary.NativeEndian.PutUint16(done[4:], 3) // NLMSG_DONE
	dump = append(dump, done...)

	got := ips(parseNetlinkNeighbors(dump))
	want := []string{"192.168.1.20=14:c0:89:10:83:03", "192.168.1.21=14:c0:89:10:83:04",
		"192.168.1.24=14:c0:89:10:83:07", "192.168.1.9=14:c0:89:10:83:01"}
	if !slices.Equal(got, want) {
		t.Errorf("fresh neighbors = %v, want %v", got, want)
	}
	if n := parseNetlinkNeighbors(dump[:10]); len(n) != 0 {
		t.Errorf("truncated dump = %v", n)
	}
}

// ipnetRow2 builds one MIB_IPNET_ROW2 (88 bytes) for an IPv4 address.
func ipnetRow2(ip netip.Addr, mac []byte, state uint32) []byte {
	r := make([]byte, 88)
	binary.LittleEndian.PutUint16(r[0:], 2) // AF_INET
	a := ip.As4()
	copy(r[4:8], a[:])
	copy(r[40:], mac)
	binary.LittleEndian.PutUint32(r[72:], uint32(len(mac)))
	binary.LittleEndian.PutUint32(r[76:], state)
	return r
}

func TestParseIPNetTable2(t *testing.T) {
	mac := func(last byte) []byte { return []byte{0x14, 0xc0, 0x89, 0x13, 0xfc, last} }
	rows := [][]byte{
		ipnetRow2(netip.MustParseAddr("192.168.1.9"), mac(1), 5),  // Reachable
		ipnetRow2(netip.MustParseAddr("192.168.1.43"), mac(2), 4), // Stale
		ipnetRow2(netip.MustParseAddr("192.168.1.20"), mac(3), 3), // Delay
		ipnetRow2(netip.MustParseAddr("192.168.1.21"), mac(4), 2), // Probe
		ipnetRow2(netip.MustParseAddr("192.168.1.22"), mac(5), 0), // Unreachable
		ipnetRow2(netip.MustParseAddr("192.168.1.23"), nil, 1),    // Incomplete
		ipnetRow2(netip.MustParseAddr("192.168.1.24"), mac(7), 6), // Permanent
	}
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf, uint32(len(rows)))
	for _, r := range rows {
		buf = append(buf, r...)
	}
	got := ips(parseIPNetTable2(buf))
	want := []string{"192.168.1.20=14:c0:89:13:fc:03", "192.168.1.21=14:c0:89:13:fc:04",
		"192.168.1.24=14:c0:89:13:fc:07", "192.168.1.9=14:c0:89:13:fc:01"}
	if !slices.Equal(got, want) {
		t.Errorf("fresh neighbors = %v, want %v", got, want)
	}
	if n := parseIPNetTable2(buf[:8+88]); len(n) != 1 {
		t.Errorf("table cut after one row = %v", n)
	}
	if n := parseIPNetTable2(nil); len(n) != 0 {
		t.Errorf("empty = %v", n)
	}
}
