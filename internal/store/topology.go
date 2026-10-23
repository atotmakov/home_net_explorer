package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Topology is the Map view (feature 010, data-model.md): subnet groups, device and port nodes,
// and the connections between them, computed at request time from stored data (research R1).
type Topology struct {
	Groups []TopoGroup `json:"groups"`
	Nodes  []TopoNode  `json:"nodes"`
	Edges  []TopoEdge  `json:"edges"`
}

// TopoGroup is a subnet (or "other") on the map.
type TopoGroup struct {
	ID    string `json:"id"` // subnet-<id> or other
	Label string `json:"label"`
	CIDR  string `json:"cidr"`
	Stale bool   `json:"stale"`
}

// TopoNode is a device or a router port / Wi-Fi interface.
type TopoNode struct {
	ID          string   `json:"id"`   // dev-<id> or port-<router device id>-<via>
	Kind        string   `json:"kind"` // device or port
	Parent      string   `json:"parent"`
	DeviceID    int64    `json:"device_id"`
	Label       string   `json:"label"`
	IP          string   `json:"ip"`
	IPs         []string `json:"ips"`
	MAC         string   `json:"mac"`
	Vendor      string   `json:"vendor"`
	Type        string   `json:"type"`
	State       string   `json:"state"` // online, stale or offline (feature 006)
	LastSeenAgo string   `json:"last_seen_ago"`
	GatewayOf   []string `json:"gateway_of"` // group ids
}

// MarshalJSON writes a port node with its four fields only (contracts/web-ui-changes.md).
func (n TopoNode) MarshalJSON() ([]byte, error) {
	if n.Kind == "port" {
		return json.Marshal(struct {
			ID     string `json:"id"`
			Kind   string `json:"kind"`
			Parent string `json:"parent"`
			Label  string `json:"label"`
		}{n.ID, n.Kind, n.Parent, n.Label})
	}
	type plain TopoNode
	p := plain(n)
	if p.IPs == nil {
		p.IPs = []string{}
	}
	if p.GatewayOf == nil {
		p.GatewayOf = []string{}
	}
	return json.Marshal(p)
}

// TopoEdge is a connection: gateway (device → its subnet), bridge (device → another subnet it
// is in), port (router → port, port → device; Current false = last known), manual (owner).
type TopoEdge struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Source  string `json:"source"`
	Target  string `json:"target"`
	Label   string `json:"label,omitempty"`
	Current bool   `json:"current"`
}

func devNodeID(id int64) string   { return "dev-" + strconv.FormatInt(id, 10) }
func groupID(subnet int64) string { return "subnet-" + strconv.FormatInt(subnet, 10) }

// Topology builds the map at time now: every device of the unfiltered Devices list (R2), grouped
// by the subnet of its first address (R3), with gateways (R4), two-subnet links (R5), router
// ports (R6) and the owner's links (R7).
func (s *Store) Topology(ctx context.Context, now time.Time) (Topology, error) {
	tp := Topology{Groups: []TopoGroup{}, Nodes: []TopoNode{}, Edges: []TopoEdge{}}
	devs, err := s.ListDevices(ctx, DeviceFilter{Now: now})
	if err != nil {
		return tp, err
	}
	subnets, err := s.SubnetStatus(ctx, now)
	if err != nil {
		return tp, err
	}
	scanned, err := s.latestRunCIDRs(ctx)
	if err != nil {
		return tp, err
	}
	vantages, err := s.latestVantages(ctx)
	if err != nil {
		return tp, err
	}
	routers, err := s.routerAddressesBySubnet(ctx)
	if err != nil {
		return tp, err
	}
	links, err := s.userLinkTargets(ctx)
	if err != nil {
		return tp, err
	}
	vias, err := s.latestVias(ctx)
	if err != nil {
		return tp, err
	}

	// Device nodes.
	type holder struct{ dev, subnet int64 }
	byIP := map[string]holder{}
	byMAC := map[string]int64{}
	nodeIdx := map[int64]int{}
	used := map[string]bool{}
	for _, d := range devs {
		n := TopoNode{ID: devNodeID(d.ID), Kind: "device", Parent: "other", DeviceID: d.ID, Label: d.DisplayName,
			IP: d.PrimaryIP(), IPs: []string{}, MAC: d.MAC, Vendor: d.Manufacturer, Type: d.Type, State: d.State,
			LastSeenAgo: Ago(d.Age), GatewayOf: []string{}}
		if len(d.Addresses) > 0 {
			n.Parent = groupID(d.Addresses[0].SubnetID)
		}
		for _, a := range d.Addresses {
			n.IPs = append(n.IPs, a.IP)
			byIP[a.IP] = holder{d.ID, a.SubnetID}
			used[groupID(a.SubnetID)] = true
		}
		used[n.Parent] = true
		if d.MAC != "" {
			byMAC[strings.ToLower(d.MAC)] = d.ID
		}
		nodeIdx[d.ID] = len(tp.Nodes)
		tp.Nodes = append(tp.Nodes, n)
	}

	// Groups: subnets holding devices, plus subnets in a collector's latest run (R3).
	order := map[string]int{}
	cidrGroup := map[string]string{}
	subnetCIDR := map[int64]string{}
	for _, si := range subnets {
		if si.Ignored {
			continue
		}
		id := groupID(si.ID)
		subnetCIDR[si.ID] = si.CIDR
		if !used[id] && !scanned[si.CIDR] {
			continue
		}
		label := si.CIDR
		if si.Name != "" {
			label = si.Name + " (" + si.CIDR + ")"
		}
		order[id] = len(tp.Groups)
		cidrGroup[si.CIDR] = id
		tp.Groups = append(tp.Groups, TopoGroup{ID: id, Label: label, CIDR: si.CIDR, Stale: si.Stale})
	}
	if used["other"] {
		order["other"] = len(tp.Groups)
		tp.Groups = append(tp.Groups, TopoGroup{ID: "other", Label: "Other"})
	}
	hasEdge := map[string]bool{}
	addEdge := func(e TopoEdge) {
		if !hasEdge[e.ID] {
			hasEdge[e.ID] = true
			tp.Edges = append(tp.Edges, e)
		}
	}

	// Gateways (R4): router addresses of a subnet and route next hops.
	gateways := map[int64]map[string]bool{}
	addGateway := func(ip string) {
		h, ok := byIP[ip]
		if !ok {
			return
		}
		g := groupID(h.subnet)
		if _, ok := order[g]; !ok {
			return
		}
		if gateways[h.dev] == nil {
			gateways[h.dev] = map[string]bool{}
		}
		gateways[h.dev][g] = true
	}
	for _, ips := range routers {
		for _, ip := range ips {
			addGateway(ip)
		}
	}
	for _, v := range vantages {
		for _, r := range v.Routes {
			if r.NextHop != "" {
				addGateway(r.NextHop)
			}
		}
	}
	for _, d := range devs {
		gs := gateways[d.ID]
		if len(gs) == 0 {
			continue
		}
		n := &tp.Nodes[nodeIdx[d.ID]]
		for g := range gs {
			n.GatewayOf = append(n.GatewayOf, g)
		}
		slices.SortFunc(n.GatewayOf, func(a, b string) int { return order[a] - order[b] })
		for _, g := range n.GatewayOf {
			addEdge(TopoEdge{ID: "gw-" + strconv.FormatInt(d.ID, 10) + "-" + g, Kind: "gateway", Source: n.ID, Target: g, Current: true})
		}
	}

	// Two-subnet devices (R5): current addresses in other subnets, and collectors' interfaces.
	bridge := func(dev int64, g string) {
		n := tp.Nodes[nodeIdx[dev]]
		if _, ok := order[g]; !ok || g == n.Parent {
			return
		}
		addEdge(TopoEdge{ID: "br-" + strconv.FormatInt(dev, 10) + "-" + g, Kind: "bridge", Source: n.ID, Target: g, Current: true})
	}
	for _, d := range devs {
		for _, a := range d.Addresses {
			bridge(d.ID, groupID(a.SubnetID))
		}
	}
	for _, v := range vantages {
		var dev int64
		for _, in := range v.Interfaces {
			if id, ok := byMAC[strings.ToLower(in.MAC)]; ok && in.MAC != "" {
				dev = id
				break
			}
		}
		if dev == 0 {
			continue
		}
		for _, in := range v.Interfaces {
			ip, err := netip.ParseAddr(in.IP)
			if err != nil {
				continue
			}
			p, err := ip.Prefix(in.PrefixLen)
			if err != nil {
				continue
			}
			if g, ok := cidrGroup[p.String()]; ok {
				bridge(dev, g)
			}
		}
	}

	// The owner's links (R7) replace router ports.
	manual := map[int64]bool{}
	for _, d := range devs {
		to, ok := links[d.ID]
		if !ok || to == d.ID {
			continue
		}
		if _, ok := nodeIdx[to]; !ok {
			continue
		}
		manual[d.ID] = true
		addEdge(TopoEdge{ID: "mu-" + strconv.FormatInt(d.ID, 10), Kind: "manual", Source: devNodeID(d.ID), Target: devNodeID(to), Current: true})
	}

	// Router ports and Wi-Fi (R6): the latest reported via, under the router of that subnet.
	for _, d := range devs {
		v, ok := vias[d.ID]
		if !ok || manual[d.ID] {
			continue
		}
		var router int64
		for _, ip := range routers[subnetCIDR[v.subnet]] {
			if h, ok := byIP[ip]; ok {
				router = h.dev
				break
			}
		}
		if router == 0 || router == d.ID {
			continue
		}
		rs := strconv.FormatInt(router, 10)
		port := "port-" + rs + "-" + v.via
		if !hasEdge["rp-"+rs+"-"+v.via] {
			tp.Nodes = append(tp.Nodes, TopoNode{ID: port, Kind: "port", Parent: tp.Nodes[nodeIdx[router]].Parent, Label: v.via})
			addEdge(TopoEdge{ID: "rp-" + rs + "-" + v.via, Kind: "port", Source: devNodeID(router), Target: port, Label: v.via, Current: true})
		}
		addEdge(TopoEdge{ID: "pd-" + strconv.FormatInt(d.ID, 10), Kind: "port", Source: port, Target: devNodeID(d.ID),
			Label: v.via, Current: d.State != StateOffline})
	}
	return tp, nil
}

// latestRunCIDRs returns the subnets scanned (not skipped) in each collector's latest run.
func (s *Store) latestRunCIDRs(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT rs.cidr FROM run_subnets rs
		JOIN collection_runs r ON r.collection_id = rs.collection_id
		WHERE rs.method != 'skipped'
		AND r.rowid = (SELECT MAX(r2.rowid) FROM collection_runs r2 WHERE r2.collector_id = r.collector_id)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out[c] = true
	}
	return out, rows.Err()
}

// latestVantages returns the vantage (interfaces, routes) of each collector's latest run. A
// payload that cannot be read is skipped: the map then lacks its gateways, nothing else.
func (s *Store) latestVantages(ctx context.Context) ([]contract.Vantage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.payload_gz FROM collection_runs r
		WHERE r.rowid = (SELECT MAX(r2.rowid) FROM collection_runs r2 WHERE r2.collector_id = r.collector_id)`)
	if err != nil {
		return nil, err
	}
	var payloads [][]byte
	for rows.Next() {
		var gz []byte
		if err := rows.Scan(&gz); err != nil {
			rows.Close()
			return nil, err
		}
		payloads = append(payloads, gz)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []contract.Vantage
	for _, gz := range payloads {
		zr, err := gzip.NewReader(bytes.NewReader(gz))
		if err != nil {
			continue
		}
		run, err := contract.Decode(zr)
		zr.Close()
		if err != nil {
			continue
		}
		out = append(out, run.Vantage)
	}
	return out, nil
}

// routerAddressesBySubnet returns, per subnet CIDR, the addresses of routers read there (run
// sources with outcome ok) and of routers set up in the UI (feature 003).
func (s *Store) routerAddressesBySubnet(ctx context.Context) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT subnet, address FROM run_sources WHERE outcome = 'ok'
		ORDER BY subnet, address`)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for rows.Next() {
		var cidr, ip string
		if err := rows.Scan(&cidr, &ip); err != nil {
			rows.Close()
			return nil, err
		}
		out[cidr] = append(out[cidr], ip)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	refs, err := s.ListRouters(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range refs {
		if !slices.Contains(out[r.Subnet], r.Address) {
			out[r.Subnet] = append(out[r.Subnet], r.Address)
		}
	}
	return out, nil
}

type viaSighting struct {
	subnet int64
	via    string
}

// latestVias returns each device's most recent sighting with a router port or Wi-Fi interface.
func (s *Store) latestVias(ctx context.Context) (map[int64]viaSighting, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT device_id, subnet_id, via FROM sightings WHERE via <> ''
		ORDER BY device_id, last_seen DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]viaSighting{}
	for rows.Next() {
		var dev int64
		var v viaSighting
		if err := rows.Scan(&dev, &v.subnet, &v.via); err != nil {
			return nil, err
		}
		if _, ok := out[dev]; !ok {
			out[dev] = v
		}
	}
	return out, rows.Err()
}

// Ago formats an age as "less than a minute", "45 mins", "3 hours, 5 mins" or
// "2 days, 4 hours, 1 min".
func Ago(d time.Duration) string {
	if d < time.Minute {
		return "less than a minute"
	}
	unit := func(n int, name string) string {
		if n == 1 {
			return "1 " + name
		}
		return fmt.Sprintf("%d %ss", n, name)
	}
	m := int(d.Minutes())
	days, hours, mins := m/(24*60), m/60%24, m%60
	switch {
	case days > 0:
		return unit(days, "day") + ", " + unit(hours, "hour") + ", " + unit(mins, "min")
	case hours > 0:
		return unit(hours, "hour") + ", " + unit(mins, "min")
	default:
		return unit(mins, "min")
	}
}
