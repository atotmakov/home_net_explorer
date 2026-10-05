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
