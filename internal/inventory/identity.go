package inventory

// Device statuses (data-model.md state machine).
const (
	StatusNew        = "new"
	StatusNewOffline = "new_offline"
	StatusOnline     = "online"
	StatusOffline    = "offline"
	StatusMergedAway = "merged_away"
)

// Event types.
const (
	EventNewDevice       = "new_device"
	EventIPChanged       = "ip_changed"
	EventHostnameChanged = "hostname_changed"
	EventMACChanged      = "mac_changed"
	EventWentOffline     = "went_offline"
	EventCameOnline      = "came_online"
	EventMerged          = "merged"
	EventSplit           = "split"
)

// MACKey is the strong identity key of a device with a known MAC (research R6).
func MACKey(mac string) string { return "mac:" + mac }

// Identity strengths.
const (
	StrengthStrong = "strong" // keyed by MAC
	StrengthWeak   = "weak"   // no MAC known (routed subnets): keyed by hostname or IP
)

// WeakKey is the identity of a device seen without a MAC: by hostname when there is one,
// otherwise by IP, always within its subnet (research R6).
func WeakKey(cidr, ip, hostname string) string {
	if hostname != "" {
		return "host:" + cidr + ":" + hostname
	}
	return "ip:" + cidr + ":" + ip
}
