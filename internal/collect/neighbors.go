package collect

import (
	"encoding/binary"
	"net"
	"net/netip"
)

// Neighbor tables keep entries long after a host stopped answering: Linux marks them STALE,
// Windows Stale, and /proc/net/arp or GetIpNetTable do not say so. The NAS reported a Dune player
// at an address it had left six days earlier. Only entries the kernel recently confirmed (or that
// are permanent) are neighbors; these parsers read the state too. They are plain byte parsers so
// they are tested on every platform.

// Linux neighbor states (NUD_*) that count: reachable, delay, probe, permanent.
const nudFresh = 0x02 | 0x08 | 0x10 | 0x80

// parseNetlinkNeighbors parses an RTM_GETNEIGH dump (netlink messages in host byte order) and
// returns the IPv4 entries in a fresh state.
func parseNetlinkNeighbors(b []byte) []Neighbor {
	const (
		hdrLen      = 16
		ndmsgLen    = 12
		nlmsgDone   = 3
		rtmNewNeigh = 28
		ndaDst      = 1
		ndaLLAddr   = 2
	)
	var out []Neighbor
	for len(b) >= hdrLen {
		n := int(binary.NativeEndian.Uint32(b[0:4]))
		typ := binary.NativeEndian.Uint16(b[4:6])
		if n < hdrLen || n > len(b) || typ == nlmsgDone {
			break
		}
		msg := b[hdrLen:n]
		b = b[align4(n):]
		if typ != rtmNewNeigh || len(msg) < ndmsgLen || msg[0] != 2 { // AF_INET
			continue
		}
		state := binary.NativeEndian.Uint16(msg[8:10])
		if state&nudFresh == 0 {
			continue
		}
		var ip netip.Addr
		var mac string
		for attrs := msg[ndmsgLen:]; len(attrs) >= 4; {
			l := int(binary.NativeEndian.Uint16(attrs[0:2]))
			t := binary.NativeEndian.Uint16(attrs[2:4])
			if l < 4 || l > len(attrs) {
				break
			}
			data := attrs[4:l]
			switch {
			case t == ndaDst && len(data) == 4:
				ip = netip.AddrFrom4([4]byte(data))
			case t == ndaLLAddr && len(data) == 6:
				mac = net.HardwareAddr(data).String()
			}
			attrs = attrs[min(align4(l), len(attrs)):]
		}
		if m, ok := NormalizeMAC(mac); ok && ip.IsValid() {
			out = append(out, Neighbor{IP: ip, MAC: m})
		}
	}
	return out
}

func align4(n int) int { return (n + 3) &^ 3 }

// Windows NL_NEIGHBOR_STATE values that count: probe, delay, reachable, permanent.
var winFresh = map[uint32]bool{2: true, 3: true, 5: true, 6: true}

// parseIPNetTable2 parses a MIB_IPNET_TABLE2 (NumEntries, then 88-byte MIB_IPNET_ROW2 rows from
// offset 8) and returns the IPv4 entries in a fresh state.
func parseIPNetTable2(b []byte) []Neighbor {
	const rowLen, first = 88, 8
	if len(b) < first {
		return nil
	}
	n := int(binary.LittleEndian.Uint32(b[0:4]))
	var out []Neighbor
	for i := 0; i < n; i++ {
		off := first + i*rowLen
		if off+rowLen > len(b) {
			break
		}
		r := b[off : off+rowLen]
		if binary.LittleEndian.Uint16(r[0:2]) != 2 { // AF_INET
			continue
		}
		if !winFresh[binary.LittleEndian.Uint32(r[76:80])] || binary.LittleEndian.Uint32(r[72:76]) != 6 {
			continue
		}
		ip := netip.AddrFrom4([4]byte(r[4:8]))
		if m, ok := NormalizeMAC(net.HardwareAddr(r[40:46]).String()); ok {
			out = append(out, Neighbor{IP: ip, MAC: m})
		}
	}
	return out
}
