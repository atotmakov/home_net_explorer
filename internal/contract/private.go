package contract

import "net/netip"

// IsPrivate reports whether p lies entirely within the private IPv4 ranges of RFC 1918
// (10/8, 172.16/12, 192.168/16). Only such subnets are ever scanned (FR-006, Principle I).
// This file is the only place allowed to deal with concrete address ranges (make lint).
func IsPrivate(p netip.Prefix) bool {
	p = p.Masked()
	a := p.Addr()
	if !a.Is4() || !a.IsPrivate() {
		return false
	}
	// The prefix must not be wider than the RFC 1918 block containing it.
	switch b := a.As4(); {
	case b[0] == 10:
		return p.Bits() >= 8
	case b[0] == 172:
		return p.Bits() >= 12
	default: // 192.168
		return p.Bits() >= 16
	}
}

// IsPrivateAddr reports whether a is a private (RFC 1918) IPv4 address.
func IsPrivateAddr(a netip.Addr) bool {
	return a.Is4() && a.IsPrivate()
}

// AutoScannable reports whether a directly attached subnet is scanned without explicit
// configuration: private and /22 or narrower.
func AutoScannable(p netip.Prefix) bool {
	return IsPrivate(p) && p.Bits() >= MaxAutoScanPrefixBits && p.Bits() <= MaxPrefixBits
}
