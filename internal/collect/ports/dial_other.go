package ports

import "syscall"

// dialControl adjusts a socket before it connects.
var dialControl func(network, address string, c syscall.RawConn) error
