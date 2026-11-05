//go:build !windows

package ports

import "syscall"

// dialControl adjusts a socket before it connects. Other systems report a refusal at once.
var dialControl func(network, address string, c syscall.RawConn) error
