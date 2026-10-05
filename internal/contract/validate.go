package contract

import (
	"net/netip"
	"regexp"
)

var (
	uuidRe          = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	collectorNameRe = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
	macRe           = regexp.MustCompile(`^([0-9a-f]{2}:){5}[0-9a-f]{2}$`)
)

// ValidMAC reports whether s is a lower-case colon-separated MAC address.
func ValidMAC(s string) bool { return macRe.MatchString(s) }

// ValidCollectorName reports whether s is a valid collector name.
func ValidCollectorName(s string) bool { return collectorNameRe.MatchString(s) }

// Validate enforces the rules of data-model.md "Validation summary". Clock skew between the
// collector and the server is reported (UploadResult.clock_skew_ms), not rejected.
func Validate(run *CollectionRun) error {
	if run.SchemaVersion != SchemaVersion {
		return &Error{Code: CodeUnsupportedSchema, Detail: "schema_version must be 1"}
	}
	if !uuidRe.MatchString(run.CollectionID) {
		return invalid("collection_id must be a UUID")
	}
	c := run.Collector
	if !collectorNameRe.MatchString(c.Name) {
		return invalid("collector.name must match [a-z0-9-]{1,64}")
	}
	if c.Version == "" {
		return invalid("collector.version is required")
	}
	switch c.OS {
	case "windows", "linux", "darwin":
	default:
		return invalid("collector.os must be windows, linux or darwin")
	}
	if run.IntervalSeconds < 0 {
		return invalid("interval_seconds must be >= 0")
	}

	if run.StartedAt.IsZero() || run.FinishedAt.IsZero() || run.SentAt.IsZero() {
		return invalid("started_at, finished_at and sent_at are required")
	}
	if run.FinishedAt.Before(run.StartedAt) {
		return invalid("finished_at is before started_at")
	}
	if run.FinishedAt.Sub(run.StartedAt) > MaxRunWindow {
		return invalid("the run window must be no longer than 1 hour")
	}
	if run.FinishedAt.Sub(run.SentAt) > MaxFutureSkew {
		return invalid("the run window must not be more than 5 minutes in the future relative to sent_at")
	}

	if run.Subnets == nil || run.Observations == nil || run.Vantage.Interfaces == nil || run.Vantage.Routes == nil {
		return invalid("subnets, observations, vantage.interfaces and vantage.routes are required arrays")
	}
	if len(run.Subnets) > MaxSubnets {
		return invalid("no more than %d subnets per run", MaxSubnets)
	}
	if len(run.Observations) > MaxObservations {
		return invalid("no more than %d observations per run", MaxObservations)
	}
	if err := validateVantage(run.Vantage); err != nil {
		return err
	}

	scanned := make([]netip.Prefix, 0, len(run.Subnets))
	seen := map[netip.Prefix]bool{}
	for _, s := range run.Subnets {
		p, err := ParseSubnet(s.CIDR)
		if err != nil {
			return err
		}
		if seen[p] {
			return invalid("subnet %s listed twice", s.CIDR)
		}
		seen[p] = true
		switch s.Method {
		case MethodARP, MethodICMPTCP:
			if s.SkipReason != "" {
				return invalid("subnet %s: skip_reason is only allowed when method = skipped", s.CIDR)
			}
			if s.HostsProbed < 0 {
				return invalid("subnet %s: hosts_probed must be >= 0", s.CIDR)
			}
			scanned = append(scanned, p)
		case MethodSkipped:
			if s.SkipReason != SkipTooLarge && s.SkipReason != SkipIgnored {
				return invalid("subnet %s: skip_reason is required when method = skipped", s.CIDR)
			}
			if s.Complete || s.HostsProbed != 0 {
				return invalid("subnet %s: skipped entries must have complete=false and hosts_probed=0", s.CIDR)
			}
		default:
			return invalid("subnet %s: unknown method %q", s.CIDR, s.Method)
		}
	}

	for i, o := range run.Observations {
		if err := validateObservation(i, o, run, scanned); err != nil {
			return err
		}
	}
	return nil
}

// ParseSubnet parses a subnet CIDR and enforces "Every subnet cidr must be private (RFC 1918)
// with a prefix from /16 to /30" (and that it is written in masked form).
func ParseSubnet(s string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(s)
	if err != nil || !p.Addr().Is4() {
		return netip.Prefix{}, invalid("subnet %q is not an IPv4 CIDR", s)
	}
	if p != p.Masked() {
		return netip.Prefix{}, invalid("subnet %q must be written in masked form (%s)", s, p.Masked())
	}
	if p.Bits() < MinPrefixBits || p.Bits() > MaxPrefixBits {
		return netip.Prefix{}, invalid("subnet %s: prefix must be from /16 to /30", s)
	}
	if !IsPrivate(p) {
		return netip.Prefix{}, invalid("subnet %s is not a private (RFC 1918) range", s)
	}
	return p, nil
}

func validateObservation(i int, o Observation, run *CollectionRun, scanned []netip.Prefix) error {
	if o.ObservedAt.Before(run.StartedAt) || o.ObservedAt.After(run.FinishedAt) {
		return invalid("observations[%d]: observed_at must be within the run window", i)
	}
	ip, err := netip.ParseAddr(o.IP)
	if err != nil || !ip.Is4() {
		return invalid("observations[%d]: ip %q is not an IPv4 address", i, o.IP)
	}
	inside := false
	for _, p := range scanned {
		if p.Contains(ip) {
			inside = true
			break
		}
	}
	if !inside {
		return invalid("observations[%d]: ip %s must be inside a non-skipped subnet of the same run", i, o.IP)
	}
	switch o.Method {
	case ObsARP, ObsNeighborCache, ObsICMP, ObsTCP:
	default:
		return invalid("observations[%d]: unknown method %q", i, o.Method)
	}
	if o.MAC != "" && !macRe.MatchString(o.MAC) {
		return invalid("observations[%d]: mac %q must be lower-case aa:bb:cc:dd:ee:ff", i, o.MAC)
	}
	if o.Method == ObsARP && o.MAC == "" {
		return invalid("observations[%d]: MAC is required when method is arp", i)
	}
	if len(o.Hostname) > 253 {
		return invalid("observations[%d]: hostname longer than 253 characters", i)
	}
	switch o.HostnameSource {
	case "", HostnameSourceDNS, HostnameSourceMDNS:
	default:
		return invalid("observations[%d]: unknown hostname_source %q", i, o.HostnameSource)
	}
	return nil
}

func validateVantage(v Vantage) error {
	for i, ifc := range v.Interfaces {
		if a, err := netip.ParseAddr(ifc.IP); err != nil || !a.Is4() {
			return invalid("vantage.interfaces[%d]: ip %q is not IPv4", i, ifc.IP)
		}
		if ifc.PrefixLen < 0 || ifc.PrefixLen > 32 {
			return invalid("vantage.interfaces[%d]: prefix_len out of range", i)
		}
		if ifc.MAC != "" && !macRe.MatchString(ifc.MAC) {
			return invalid("vantage.interfaces[%d]: bad mac %q", i, ifc.MAC)
		}
	}
	for i, r := range v.Routes {
		if p, err := netip.ParsePrefix(r.Destination); err != nil || !p.Addr().Is4() {
			return invalid("vantage.routes[%d]: destination %q is not an IPv4 CIDR", i, r.Destination)
		}
		if r.NextHop != "" {
			if a, err := netip.ParseAddr(r.NextHop); err != nil || !a.Is4() {
				return invalid("vantage.routes[%d]: next_hop %q is not IPv4", i, r.NextHop)
			}
		}
	}
	return nil
}
