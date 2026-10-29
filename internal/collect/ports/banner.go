package ports

import (
	"bytes"
	"net"
	"time"
)

// readBanner waits briefly for a greeting the server sends by itself (nothing is sent to it) and
// returns the service it names, or "". Only the name is kept, never the text (research R4).
func readBanner(conn net.Conn, timeout time.Duration) string {
	conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 64)
	n, _ := conn.Read(buf)
	return bannerService(buf[:n])
}

func bannerService(b []byte) string {
	switch {
	case len(b) == 0:
		return ""
	case bytes.HasPrefix(b, []byte("SSH-")):
		return "ssh"
	case b[0] == 0xff: // Telnet option negotiation (IAC)
		return "telnet"
	case bytes.HasPrefix(b, []byte("+OK")):
		return "pop3"
	case bytes.HasPrefix(b, []byte("* OK")):
		return "imap"
	case bytes.HasPrefix(b, []byte("220")):
		up := bytes.ToUpper(b)
		if bytes.Contains(up, []byte("SMTP")) {
			return "smtp"
		}
		if bytes.Contains(up, []byte("FTP")) {
			return "ftp"
		}
	}
	return ""
}
