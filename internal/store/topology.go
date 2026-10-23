package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
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

// Topology builds the map at time now.
func (s *Store) Topology(ctx context.Context, now time.Time) (Topology, error) {
	return Topology{Groups: []TopoGroup{}, Nodes: []TopoNode{}, Edges: []TopoEdge{}}, nil
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
