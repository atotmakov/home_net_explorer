package ports

// Service returns the usual name of a port, or "" (research R4). The table ships with the
// collector: nothing is looked up elsewhere (FR-011).
func Service(proto string, port int) string {
	if proto == "udp" {
		return udpServices[port]
	}
	return tcpServices[port]
}

var tcpServices = map[int]string{
	20: "ftp-data", 21: "ftp", 22: "ssh", 23: "telnet", 25: "smtp", 53: "dns", 80: "http",
	81: "http-alt", 88: "kerberos", 110: "pop3", 111: "rpcbind", 135: "msrpc", 139: "netbios-ssn",
	143: "imap", 179: "bgp", 389: "ldap", 443: "https", 445: "smb", 465: "smtps", 515: "printer",
	548: "afp", 554: "rtsp", 587: "submission", 631: "ipp", 636: "ldaps", 853: "dns-over-tls",
	873: "rsync", 990: "ftps", 993: "imaps", 995: "pop3s", 1080: "socks", 1194: "openvpn",
	1433: "mssql", 1723: "pptp", 1883: "mqtt", 1900: "upnp", 2049: "nfs", 2375: "docker",
	2376: "docker-tls", 3000: "http-alt", 3128: "http-proxy", 3260: "iscsi", 3306: "mysql",
	3389: "rdp", 3689: "daap", 4443: "https-alt", 5000: "synology-dsm", 5001: "synology-dsm-https",
	5005: "webdav", 5006: "webdav-https", 5060: "sip", 5222: "xmpp", 5357: "wsdapi", 5432: "postgresql",
	5900: "vnc", 6379: "redis", 6881: "bittorrent", 7000: "airplay", 7547: "tr-069", 8000: "http-alt",
	8008: "http-alt", 8009: "googlecast", 8080: "http-alt", 8081: "http-alt", 8083: "http-alt",
	8086: "influxdb", 8123: "home-assistant", 8200: "upnp-media", 8443: "https-alt", 8883: "mqtts",
	8888: "http-alt", 9000: "http-alt", 9090: "http-alt", 9091: "transmission", 9100: "jetdirect",
	9200: "elasticsearch", 10000: "webmin", 32400: "plex", 49152: "upnp", 62078: "iphone-sync",
}

var udpServices = map[int]string{
	53: "dns", 67: "dhcp", 69: "tftp", 123: "ntp", 137: "netbios-ns", 138: "netbios-dgm",
	161: "snmp", 500: "isakmp", 1900: "ssdp", 4500: "ipsec-nat", 5353: "mdns",
}
