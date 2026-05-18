package utils

type Array[T any, ST any] struct {
	FromItems []T
	ToItems   *[]ST
}

func (*Array[T, ST]) FromSlice(S []T) *Array[T, ST] {
	return &Array[T, ST]{FromItems: S, ToItems: nil}
}
func (a *Array[T, ST]) Map(f func(T) ST) *[]ST {
	mapped := make([]ST, len(a.FromItems))
	for i, item := range a.FromItems {
		mapped[i] = f(item)
	}
	return &mapped
}

var UdpPorts = []int{
	53,   // DNS
	67,   // DHCP Server
	68,   // DHCP Client
	69,   // TFTP
	123,  // NTP
	161,  // SNMP
	162,  // SNMP Trap
	514,  // Syslog
	520,  // RIP
	1900, // SSDP
	5353, // mDNS
	// UDP Streaming Ports
	8000, // Icecast
	8080, // Alternate HTTP
	9000, // Streaming Media
	// mikrotik routeros udp services ports
	// winbox
	8291,
	// mntp
	20561,
}

var TcpPorts = []int{
	20,    // FTP Data
	21,    // FTP Control
	22,    // SSH
	23,    // Telnet
	25,    // SMTP
	53,    // DNS
	80,    // HTTP
	110,   // POP3
	143,   // IMAP
	443,   // HTTPS
	3306,  // MySQL
	5432,  // PostgreSQL
	6379,  // Redis
	8080,  // HTTP Alternate
	8443,  // HTTPS Alternate
	27017, // MongoDB
	// GAME PORTS
	25565, // Minecraft
	27015, // Source Engine (e.g., CS:GO)
	7777,  // ARK: Survival Evolved
	8888,  // Generic Game Port
	// VOIP PORTS
	5060, // SIP
	5061, // SIP TLS
	5222, // XMPP Client Connection
	5223, // XMPP Client Connection (SSL)
	// COMMON PROXY PORTS
	1080, // SOCKS Proxy
	3128, // Squid Proxy
	8080, // HTTP Proxy
	// MISCELLANEOUS
	161,   // SNMP
	162,   // SNMP Trap
	3306,  // MySQL
	6379,  // Redis
	11211, // Memcached
	// dns over tcp
	853, // DNS over TLS
	// android devices common ports
	5555, // ADB
	8087, // Android Debug Bridge over Wi-Fi
	8600, // Consul
	// macos common ports
	5000, // Bonjour / AirPlay
	7000, // AirPlay
	8008, // AirPlay
	// windows common ports
	3389, // RDP
}
