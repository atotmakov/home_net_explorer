//go:build windows

package collect

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"net"
	"net/netip"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// The IP Helper API works without administrator rights and without Npcap (research R3).
var (
	iphlpapi              = windows.NewLazySystemDLL("iphlpapi.dll")
	procSendARP           = iphlpapi.NewProc("SendARP")
	procGetIpNetTable     = iphlpapi.NewProc("GetIpNetTable")
	procGetIpForwardTable = iphlpapi.NewProc("GetIpForwardTable")
	procIcmpCreateFile    = iphlpapi.NewProc("IcmpCreateFile")
	procIcmpCloseHandle   = iphlpapi.NewProc("IcmpCloseHandle")
	procIcmpSendEcho      = iphlpapi.NewProc("IcmpSendEcho")
)

// Platform returns the Windows implementations. close is a no-op.
func Platform() (Prober, PresenceProber, NeighborTable, RouteReader, func() error) {
	return SendARPProber{}, NewPresenceProber(), IPNetTable{}, WindowsRoutes{}, func() error { return nil }
}

// ipAddr converts an IPv4 address to the IPAddr (network byte order in a DWORD) the API expects.
func ipAddr(ip netip.Addr) uint32 {
	a := ip.As4()
	return binary.LittleEndian.Uint32(a[:])
}

func addrFromDWORD(v uint32) netip.Addr {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return netip.AddrFrom4(b)
}

// SendARPProber resolves on-link addresses with SendARP, which sends a real ARP request.
// The iface argument is ignored: Windows picks the interface from the routing table.
type SendARPProber struct{}

// Probe implements Prober. SendARP blocks for up to a few seconds when nobody answers and
// cannot be cancelled, so the context is only checked before sending.
func (SendARPProber) Probe(ctx context.Context, iface string, ip netip.Addr) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	var mac [8]byte
	size := uint32(len(mac))
	r, _, _ := procSendARP.Call(uintptr(ipAddr(ip)), 0, uintptr(unsafe.Pointer(&mac[0])), uintptr(unsafe.Pointer(&size)))
	if r != 0 || size != 6 {
		return "", false, nil // ERROR_BAD_NET_NAME etc.: nobody answered
	}
	m, ok := NormalizeMAC(net.HardwareAddr(mac[:6]).String())
	return m, ok, nil
}

// IPNetTable reads the ARP cache with GetIpNetTable (IPv4).
type IPNetTable struct{}

// mibIPNetRow is MIB_IPNETROW.
type mibIPNetRow struct {
	Index       uint32
	PhysAddrLen uint32
	PhysAddr    [8]byte
	Addr        uint32
	Type        uint32 // 1 other, 2 invalid, 3 dynamic, 4 static
}

// Entries implements NeighborTable.
func (IPNetTable) Entries(ctx context.Context) ([]Neighbor, error) {
	buf, err := callTable(procGetIpNetTable)
	if err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(buf)
	if n == 0 {
		return nil, nil
	}
	rows := unsafe.Slice((*mibIPNetRow)(unsafe.Pointer(&buf[4])), n)
	var out []Neighbor
	for _, r := range rows {
		if r.Type == 2 || r.PhysAddrLen != 6 {
			continue // invalid/incomplete
		}
		if mac, ok := NormalizeMAC(net.HardwareAddr(r.PhysAddr[:6]).String()); ok {
			out = append(out, Neighbor{IP: addrFromDWORD(r.Addr), MAC: mac})
		}
	}
	return out, nil
}

// callTable calls a GetXxxTable(pTable, pdwSize, bOrder) function, growing the buffer.
func callTable(proc *windows.LazyProc) ([]byte, error) {
	size := uint32(16 * 1024)
	for attempt := 0; attempt < 4; attempt++ {
		buf := make([]byte, size)
		r, _, _ := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 1)
		switch windows.Errno(r) {
		case 0:
			return buf, nil
		case windows.ERROR_INSUFFICIENT_BUFFER:
			continue
		case windows.ERROR_NO_DATA:
			return make([]byte, 4), nil // empty table
		default:
			return nil, fmt.Errorf("collect: %s: %w", proc.Name, windows.Errno(r))
		}
	}
	return nil, fmt.Errorf("collect: %s: table keeps growing", proc.Name)
}

// WindowsRoutes reports interfaces (GetAdaptersAddresses) and IPv4 routes (GetIpForwardTable).
type WindowsRoutes struct{}

// mibIPForwardRow is MIB_IPFORWARDROW.
type mibIPForwardRow struct {
	Dest, Mask, Policy, NextHop, IfIndex, Type, Proto, Age, NextHopAS uint32
	Metric1, Metric2, Metric3, Metric4, Metric5                       uint32
}

// Vantage implements RouteReader.
func (WindowsRoutes) Vantage(ctx context.Context) (contract.Vantage, error) {
	v := contract.Vantage{Interfaces: []contract.Interface{}, Routes: []contract.Route{}}
	v.Hostname, _ = os.Hostname()
	adapters, err := adapterAddresses()
	if err != nil {
		return v, err
	}
	names := map[uint32]string{}
	for a := adapters; a != nil; a = a.Next {
		name := windows.UTF16PtrToString(a.FriendlyName)
		names[a.IfIndex] = name
		if a.OperStatus != windows.IfOperStatusUp || a.IfType == windows.IF_TYPE_SOFTWARE_LOOPBACK {
			continue
		}
		mac := ""
		if a.PhysicalAddressLength == 6 {
			mac, _ = NormalizeMAC(net.HardwareAddr(a.PhysicalAddress[:6]).String())
		}
		for u := a.FirstUnicastAddress; u != nil; u = u.Next {
			ip, ok := sockaddrIPv4(u.Address)
			if !ok {
				continue
			}
			v.Interfaces = append(v.Interfaces, contract.Interface{Name: name, IP: ip.String(), PrefixLen: int(u.OnLinkPrefixLength), MAC: mac})
		}
	}

	buf, err := callTable(procGetIpForwardTable)
	if err != nil {
		return v, nil // interfaces alone still allow scanning
	}
	n := binary.LittleEndian.Uint32(buf)
	if n == 0 {
		return v, nil
	}
	rows := unsafe.Slice((*mibIPForwardRow)(unsafe.Pointer(&buf[4])), n)
	for _, r := range rows {
		dest := addrFromDWORD(r.Dest)
		maskBits := bits.OnesCount32(r.Mask)
		if dest.IsMulticast() || maskBits == 32 {
			continue // multicast and host routes say nothing about topology
		}
		rt := contract.Route{Destination: netip.PrefixFrom(dest, maskBits).Masked().String(), Interface: names[r.IfIndex]}
		if hop := addrFromDWORD(r.NextHop); !hop.IsUnspecified() && r.Type == 4 { // 4 = indirect (via gateway)
			rt.NextHop = hop.String()
		}
		v.Routes = append(v.Routes, rt)
	}
	return v, nil
}

func adapterAddresses() (*windows.IpAdapterAddresses, error) {
	size := uint32(15 * 1024)
	for attempt := 0; attempt < 4; attempt++ {
		buf := make([]byte, size)
		p := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(windows.AF_INET, windows.GAA_FLAG_INCLUDE_PREFIX|windows.GAA_FLAG_INCLUDE_GATEWAYS, 0, p, &size)
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
			return nil, fmt.Errorf("collect: GetAdaptersAddresses: %w", err)
		}
	}
	return nil, errors.New("collect: GetAdaptersAddresses: buffer keeps growing")
}

func sockaddrIPv4(sa windows.SocketAddress) (netip.Addr, bool) {
	if sa.Sockaddr == nil || sa.Sockaddr.Addr.Family != windows.AF_INET {
		return netip.Addr{}, false
	}
	in := (*windows.RawSockaddrInet4)(unsafe.Pointer(sa.Sockaddr))
	return netip.AddrFrom4(in.Addr), true
}

// icmpEcho uses IcmpSendEcho, which needs no administrator rights.
func icmpEcho(ctx context.Context, ip netip.Addr) bool {
	h, _, _ := procIcmpCreateFile.Call()
	if h == uintptr(windows.InvalidHandle) {
		return false
	}
	defer procIcmpCloseHandle.Call(h)
	timeout := uint32(1000)
	data := []byte("hne")
	reply := make([]byte, 256) // ICMP_ECHO_REPLY + data + 8 bytes for an ICMP error
	n, _, _ := procIcmpSendEcho.Call(h, uintptr(ipAddr(ip)), uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)),
		0, uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), uintptr(timeout))
	if n == 0 {
		return false
	}
	status := binary.LittleEndian.Uint32(reply[4:8]) // ICMP_ECHO_REPLY.Status; 0 = IP_SUCCESS
	return status == 0
}
