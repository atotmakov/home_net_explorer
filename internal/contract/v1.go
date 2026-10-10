package contract

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Limits and versions of the v1 upload contract (contracts/collector-upload-api.yaml).
const (
	SchemaVersion         = 1
	MaxObservations       = 4096
	MaxSubnets            = 16
	MaxBodyBytes          = 2 << 20
	MaxAutoScanPrefixBits = 22 // collectors auto-scan only subnets of /22 or narrower
	MinPrefixBits         = 16
	MaxPrefixBits         = 30
	MaxRunWindow          = time.Hour
	MaxFutureSkew         = 5 * time.Minute
	ClockSkewFlagMs       = 300000
	MaxSources            = 8  // router sources per run (feature 002)
	MaxViaLen             = 32 // Observation.via
	MaxModelLen           = 64 // RunSource.model
)

// Scan methods, observation methods, and skip reasons.
const (
	MethodARP             = "arp"
	MethodICMPTCP         = "icmp_tcp"
	MethodSkipped         = "skipped"
	ObsARP                = "arp"
	ObsNeighborCache      = "neighbor_cache"
	ObsICMP               = "icmp"
	ObsTCP                = "tcp"
	SkipTooLarge          = "too_large"
	SkipIgnored           = "ignored"
	HostnameSourceDNS     = "dns"
	HostnameSourceMDNS    = "mdns"
	HostnameSourceRouter  = "router"
	MethodRouterTable     = "router_table" // subnet covered by a router's device list
	ObsRouterTable        = "router_table" // device listed as online by a router
	SourceTypeRouter      = "router"
	StatusStored          = "stored"
	StatusDuplicate       = "duplicate"
	StatusDiscarded       = "discarded" // started before the owner's data reset; not stored (feature 004)
	CodeValidation        = "validation_failed"
	CodeUnsupportedSchema = "unsupported_schema"
	CodeInvalidToken      = "invalid_token"
	CodePayloadTooLarge   = "payload_too_large"
)

// Router read outcomes (RunSource.Outcome).
const (
	OutcomeOK                    = "ok"
	OutcomeUnreachable           = "unreachable"
	OutcomeLoginRejected         = "login_rejected"
	OutcomeLocked                = "locked"
	OutcomeSessionBusy           = "session_busy"
	OutcomePageNotUnderstood     = "page_not_understood"
	OutcomeSkippedAfterRejection = "skipped_after_rejection"
	OutcomeLoginUnavailable      = "login_unavailable" // the login could not be fetched from the server (feature 003)
)

// ValidOutcome reports whether s is a known router read outcome.
func ValidOutcome(s string) bool {
	switch s {
	case OutcomeOK, OutcomeUnreachable, OutcomeLoginRejected, OutcomeLocked, OutcomeSessionBusy,
		OutcomePageNotUnderstood, OutcomeSkippedAfterRejection, OutcomeLoginUnavailable:
		return true
	}
	return false
}

// CollectionRun is one completed scan by one collector.
type CollectionRun struct {
	SchemaVersion   int           `json:"schema_version"`
	CollectionID    string        `json:"collection_id"`
	Collector       CollectorInfo `json:"collector"`
	StartedAt       time.Time     `json:"started_at"`
	FinishedAt      time.Time     `json:"finished_at"`
	SentAt          time.Time     `json:"sent_at"`
	IntervalSeconds int           `json:"interval_seconds"`
	Vantage         Vantage       `json:"vantage"`
	Subnets         []SubnetScan  `json:"subnets"`
	Observations    []Observation `json:"observations"`
	Sources         []RunSource   `json:"sources,omitempty"`
}

// RunSource is the outcome of one router read in a run (feature 002). It deliberately has no
// credential fields: router logins never leave the collector (FR-006).
type RunSource struct {
	Type    string `json:"type"`
	Model   string `json:"model"`
	Address string `json:"address"`
	Subnet  string `json:"subnet"`
	Outcome string `json:"outcome"`
	Online  int    `json:"online"`
	Offline int    `json:"offline"`
}

// CollectorInfo identifies the collector build that produced a run.
type CollectorInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	OS      string `json:"os"`
}

// Vantage is the collector's own network position, used for topology inference.
type Vantage struct {
	Hostname   string      `json:"hostname,omitempty"`
	Interfaces []Interface `json:"interfaces"`
	Routes     []Route     `json:"routes"`
}

// Interface is one IPv4 address of the collector.
type Interface struct {
	Name      string `json:"name"`
	IP        string `json:"ip"`
	PrefixLen int    `json:"prefix_len"`
	MAC       string `json:"mac,omitempty"`
}

// Route is one IPv4 route of the collector. NextHop is empty for on-link routes.
type Route struct {
	Destination string `json:"destination"`
	NextHop     string `json:"next_hop,omitempty"`
	Interface   string `json:"interface,omitempty"`
}

// SubnetScan reports how one subnet was handled in a run.
type SubnetScan struct {
	CIDR        string `json:"cidr"`
	Method      string `json:"method"`
	SkipReason  string `json:"skip_reason,omitempty"`
	Complete    bool   `json:"complete"`
	HostsProbed int    `json:"hosts_probed"`
}

// Observation is a single sighting of an address.
type Observation struct {
	ObservedAt     time.Time `json:"observed_at"`
	IP             string    `json:"ip"`
	MAC            string    `json:"mac,omitempty"`
	Hostname       string    `json:"hostname,omitempty"`
	HostnameSource string    `json:"hostname_source,omitempty"`
	Method         string    `json:"method"`
	Via            string    `json:"via,omitempty"` // router port or Wi-Fi interface (router_table only)
}

// UploadResult is the server's answer to an upload.
type UploadResult struct {
	CollectionID string   `json:"collection_id"`
	Status       string   `json:"status"`
	ClockSkewMs  int64    `json:"clock_skew_ms"`
	NewSubnets   []string `json:"new_subnets,omitempty"`
}

// ErrorResponse is the JSON body of an error response.
type ErrorResponse struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}

// PingResponse is the body of GET /api/v1/ping.
type PingResponse struct {
	Collector               string      `json:"collector"`
	ServerTime              time.Time   `json:"server_time"`
	SupportedSchemaVersions []int       `json:"supported_schema_versions"`
	IgnoredSubnets          []string    `json:"ignored_subnets"`
	Routers                 []RouterRef `json:"routers,omitempty"` // routers configured in the web UI (feature 003)
}

// RouterRef is a router configured in the web UI, as listed to collectors. It never carries
// credentials; collectors fetch those with GET /api/v1/routers/{id}/login before each read.
type RouterRef struct {
	ID      int64  `json:"id"`
	Model   string `json:"model"`
	Address string `json:"address"`
	Subnet  string `json:"subnet"`
}

// RouterLogin is the answer of GET /api/v1/routers/{id}/login. Collectors keep it in memory
// for one read only.
type RouterLogin struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// SkewFlagged reports whether a collector clock skew "exceeds 5 minutes" (either direction).
func SkewFlagged(ms int64) bool { return ms > ClockSkewFlagMs || ms < -ClockSkewFlagMs }

// Error is a contract violation with a machine-readable code.
type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

func invalid(format string, args ...any) error {
	return &Error{Code: CodeValidation, Detail: fmt.Sprintf(format, args...)}
}

// Decode parses an upload body strictly: unknown fields are rejected.
func Decode(r io.Reader) (*CollectionRun, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var run CollectionRun
	if err := dec.Decode(&run); err != nil {
		return nil, invalid("decode: %v", err)
	}
	if dec.More() {
		return nil, invalid("decode: trailing data after the JSON document")
	}
	return &run, nil
}
