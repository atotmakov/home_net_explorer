//go:build linux

package collect

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"math/bits"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Platform returns the Linux implementations: raw ARP, /proc/net/arp and /proc/net/route.
// close releases the raw sockets.
func Platform() (Prober, PresenceProber, NeighborTable, RouteReader, func() error) {
	p := NewARPProber()
	return p, NewPresenceProber(), ProcNeighbors{Path: "/proc/net/arp"}, SystemRoutes{RoutePath: "/proc/net/route"}, p.Close
}

// ProcNeighbors reads the kernel neighbor table from /proc/net/arp.
type ProcNeighbors struct{ Path string }

// Entries implements NeighborTable.
func (n ProcNeighbors) Entries(ctx context.Context) ([]Neighbor, error) {
	f, err := os.Open(n.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseProcARP(f), nil
}

// parseProcARP parses "IP address  HW type  Flags  HW address  Mask  Device" lines, skipping
// incomplete entries (flags 0x0).
func parseProcARP(r io.Reader) []Neighbor {
	var out []Neighbor
	sc := bufio.NewScanner(r)
	sc.Scan() // header
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 || f[2] == "0x0" {
			continue
		}
		ip, err := netip.ParseAddr(f[0])
		if err != nil || !ip.Is4() {
			continue
		}
		if mac, ok := NormalizeMAC(f[3]); ok {
			out = append(out, Neighbor{IP: ip, MAC: mac})
		}
	}
	return out
}

// SystemRoutes reports interfaces from the OS and IPv4 routes from /proc/net/route.
type SystemRoutes struct{ RoutePath string }

// Vantage implements RouteReader.
func (s SystemRoutes) Vantage(ctx context.Context) (contract.Vantage, error) {
	v := contract.Vantage{Interfaces: []contract.Interface{}, Routes: []contract.Route{}}
	v.Hostname, _ = os.Hostname()
	ifs, err := net.Interfaces()
	if err != nil {
		return v, err
	}
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		mac, _ := NormalizeMAC(ifc.HardwareAddr.String())
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			ones, _ := ipnet.Mask.Size()
			v.Interfaces = append(v.Interfaces, contract.Interface{Name: ifc.Name, IP: ipnet.IP.To4().String(), PrefixLen: ones, MAC: mac})
		}
	}
	f, err := os.Open(s.RoutePath)
	if err != nil {
		return v, nil // interfaces alone still allow scanning
	}
	defer f.Close()
	v.Routes = parseProcRoute(f)
	return v, nil
}

// parseProcRoute parses /proc/net/route (little-endian hex addresses), keeping routes that
// are up (RTF_UP).
func parseProcRoute(r io.Reader) []contract.Route {
	out := []contract.Route{}
	sc := bufio.NewScanner(r)
	sc.Scan() // header
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 8 {
			continue
		}
		flags, err := strconv.ParseUint(f[3], 16, 32)
		if err != nil || flags&0x1 == 0 {
			continue
		}
		dst, ok1 := hexAddr(f[1])
		gw, ok2 := hexAddr(f[2])
		mask, ok3 := hexAddr(f[7])
		if !ok1 || !ok2 || !ok3 {
			continue
		}
		m := mask.As4()
		prefix := netip.PrefixFrom(dst, bits.OnesCount32(binary.BigEndian.Uint32(m[:])))
		rt := contract.Route{Destination: prefix.Masked().String(), Interface: f[0]}
		if !gw.IsUnspecified() {
			rt.NextHop = gw.String()
		}
		out = append(out, rt)
	}
	return out
}

func hexAddr(s string) (netip.Addr, bool) {
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return netip.Addr{}, false
	}
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(v))
	return netip.AddrFrom4(b), true
}
