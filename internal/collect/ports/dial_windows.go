//go:build windows

package ports

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SIO_TCP_INITIAL_RTO and its "no retransmissions" value, from mstcpip.h.
const (
	sioTCPInitialRTO      = 0x98000011
	rtoUnspecifiedRTT     = 0xFFFF
	rtoNoSynRetransmitted = 0xFE
)

// tcpInitialRTOParameters is TCP_INITIAL_RTO_PARAMETERS.
type tcpInitialRTOParameters struct {
	rtt                   uint16
	maxSynRetransmissions uint8
	_                     uint8
}

// dialControl stops Windows retrying a connect, which it also does after a refusal: a closed port
// took about 2 s to report, so with the 1 s timeout it looked silent (research R2). If Windows
// declines the setting, the check is only slower, so the connect goes ahead.
var dialControl = func(_, _ string, c syscall.RawConn) error {
	_ = setNoSynRetransmit(c)
	return nil
}

func setNoSynRetransmit(c syscall.RawConn) error {
	params := tcpInitialRTOParameters{rtt: rtoUnspecifiedRTT, maxSynRetransmissions: rtoNoSynRetransmitted}
	var opErr error
	err := c.Control(func(fd uintptr) {
		var returned uint32
		opErr = windows.WSAIoctl(windows.Handle(fd), sioTCPInitialRTO, (*byte)(unsafe.Pointer(&params)),
			uint32(unsafe.Sizeof(params)), nil, 0, &returned, nil, 0)
	})
	if err != nil {
		return err
	}
	return opErr
}
