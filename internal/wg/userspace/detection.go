package userspace

import (
	"bufio"
	"bytes"
	_ "embed"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

//go:embed oui-compact.txt
var ieeeOUIDatabase []byte

var (
	ouiDB     map[string]string // OUI -> Vendor name
	ouiDBOnce sync.Once
)

// AnalyzeFingerprint examines the collected port banners and services to produce an OS fingerprint.
func AnalyzeFingerprint(result *ScanResult) {
	var fp OSFingerprint
	var seen bool

	// First priority: Use MAC address for vendor identification if available
	if result.MACAddress != "" {
		fp.MACAddress = result.MACAddress
		if vendor, deviceType := lookupMACVendor(result.MACAddress); vendor != "" {
			fp.MACVendor = vendor
			fp.Vendor = vendor
			fp.DeviceType = deviceType
			fp.Confidence = 95
			fp.DetectionInfo = "MAC address OUI lookup"
			seen = true

			switch strings.ToLower(vendor) {
			case "mikrotik", "routerboard":
				fp.OSFamily = "routeros"
				fp.DeviceType = "router"
			case "cisco", "cisco systems":
				fp.OSFamily = "ios"
			case "ubiquiti", "ubiquiti networks":
				fp.OSFamily = "edgeos"
				fp.DeviceType = "router"
			case "juniper", "juniper networks":
				fp.OSFamily = "junos"
			case "apple":
				fp.OSFamily = "macos"
				fp.DeviceType = "workstation"
			case "microsoft":
				fp.OSFamily = "windows"
			case "raspberry pi foundation", "raspberry pi":
				fp.OSFamily = "linux"
				fp.Model = "Raspberry Pi"
			}
		}
	}

	// Copy hostname if available
	if result.Hostname != "" {
		fp.Hostname = result.Hostname
	}

	// Second priority: Nmap Service Probes & OS Detection
	for _, p := range result.OpenPorts() {
		// Use structured Nmap Info if available
		if p.NmapInfo != nil {
			if p.NmapInfo.OSType != "" || p.NmapInfo.Product != "" {
				// Map Nmap OS field (e.g., "Linux", "Windows", "RouterOS")
				if mapNmapOSToFingerprint(&fp, p.NmapInfo) {
					seen = true
				}
			}

			// If Nmap identified the service specifically as Winbox or similar
			if strings.Contains(strings.ToLower(p.NmapInfo.Service), "winbox") {
				fp.OSFamily = "routeros"
				fp.Vendor = "MikroTik"
				fp.DeviceType = "router"
				fp.Confidence = 95
				fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "Nmap: Winbox")
				seen = true
			}
		}

		// Fallback/Augment: Manual Banner analysis (legacy logic, kept for redundancy)
		banner := strings.ToLower(p.Banner)
		service := strings.ToLower(p.Service)

		// MikroTik RouterOS detection
		if fp.OSFamily == "" && (strings.Contains(service, "winbox") || strings.Contains(banner, "mikrotik") || strings.Contains(banner, "routeros")) {
			fp.OSFamily = "routeros"
			fp.Vendor = "MikroTik"
			fp.DeviceType = "router"
			if fp.Confidence < 90 {
				fp.Confidence = 90
			}
			fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "winbox/api/webfig banner")
			if ver := extractVersionFromBanner(p.Banner); ver != "" {
				fp.OSVersion = ver
			}
			if hostname := extractHostnameFromBanner(p.Banner); hostname != "" && fp.Hostname == "" {
				fp.Hostname = hostname
			}
			seen = true
		}

		// SSH-based linux
		if fp.OSFamily == "" && (strings.Contains(service, "ssh") || strings.Contains(banner, "openssh")) {
			if fp.OSFamily == "" {
				fp.OSFamily = "linux"
			}
			if fp.Vendor == "" {
				fp.Vendor = "Linux"
			}
			if fp.DeviceType == "" {
				fp.DeviceType = "server"
			}
			if fp.Confidence < 70 {
				fp.Confidence = 70
				fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "ssh banner")
			}
			if strings.Contains(banner, "ubuntu") {
				fp.Model = "Ubuntu"
				if fp.Confidence < 85 {
					fp.Confidence = 85
				}
				if ver := extractVersionFromBanner(p.Banner); ver != "" {
					fp.OSVersion = ver
				}
			} else if strings.Contains(banner, "debian") {
				fp.Model = "Debian"
				if fp.Confidence < 85 {
					fp.Confidence = 85
				}
			} else if strings.Contains(banner, "centos") || strings.Contains(banner, "red hat") || strings.Contains(banner, "rhel") {
				fp.Model = "RHEL/CentOS"
				if fp.Confidence < 85 {
					fp.Confidence = 85
				}
			}
			seen = true
		}

		// HTTP
		if strings.Contains(service, "http") || strings.Contains(banner, "server:") {
			if strings.Contains(banner, "microsoft-iis") || strings.Contains(strings.ToLower(banner), "iis/") {
				fp.OSFamily = "windows"
				if fp.Vendor == "" {
					fp.Vendor = "Microsoft"
				}
				fp.DeviceType = "server"
				if fp.Confidence < 85 {
					fp.Confidence = 85
					fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "IIS server header")
				}
				if ver := extractVersionFromBanner(p.Banner); ver != "" {
					fp.OSVersion = ver
				}
				seen = true
			}
			if strings.Contains(banner, "nginx") && fp.Confidence < 70 {
				if fp.OSFamily == "" {
					fp.OSFamily = "linux"
				}
				if fp.Vendor == "" {
					fp.Vendor = "Linux"
				}
				if fp.DeviceType == "" {
					fp.DeviceType = "server"
				}
				fp.Confidence = 70
				fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "nginx server header")
				seen = true
			}
			if strings.Contains(banner, "apache") && fp.Confidence < 70 {
				if fp.OSFamily == "" {
					fp.OSFamily = "linux"
				}
				if fp.Vendor == "" {
					fp.Vendor = "Linux"
				}
				if fp.DeviceType == "" {
					fp.DeviceType = "server"
				}
				fp.Confidence = 70
				fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "apache server header")
				seen = true
			}
		}

		// FTP
		if strings.Contains(service, "ftp") || (p.Port == 21 && p.State == "open") {
			if strings.Contains(banner, "microsoft ftp") || strings.Contains(banner, "iis") {
				fp.OSFamily = "windows"
				fp.Vendor = "Microsoft"
				fp.DeviceType = "server"
				if fp.Confidence < 85 {
					fp.Confidence = 85
					fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "Microsoft FTP banner")
				}
				seen = true
			} else if strings.Contains(banner, "vsftpd") || strings.Contains(banner, "proftpd") {
				if fp.OSFamily == "" {
					fp.OSFamily = "linux"
					fp.Vendor = "Linux"
					fp.DeviceType = "server"
					fp.Confidence = 75
					fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "Linux FTP banner")
				}
				seen = true
			}
		}

		// Telnet
		if strings.Contains(service, "telnet") || p.Port == 23 {
			if strings.Contains(banner, "mikrotik") || strings.Contains(banner, "routeros") {
				fp.OSFamily = "routeros"
				fp.Vendor = "MikroTik"
				fp.DeviceType = "router"
				if fp.Confidence < 90 {
					fp.Confidence = 90
					fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "RouterOS telnet")
				}
				seen = true
			} else if strings.Contains(banner, "cisco") {
				fp.OSFamily = "ios"
				fp.Vendor = "Cisco"
				fp.DeviceType = "router"
				if fp.Confidence < 85 {
					fp.Confidence = 85
					fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "Cisco telnet")
				}
				seen = true
			}
		}

		// RDP
		if p.Port == 3389 && p.State == "open" {
			if fp.OSFamily == "" || fp.OSFamily == "linux" {
				fp.OSFamily = "windows"
				fp.Vendor = "Microsoft"
				fp.DeviceType = "workstation"
				if fp.Confidence < 80 {
					fp.Confidence = 80
					fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "RDP port open")
				}
				seen = true
			}
		}

		// SMB
		if p.Port == 445 && p.State == "open" {
			if strings.Contains(banner, "samba") {
				if fp.OSFamily == "" {
					fp.OSFamily = "linux"
					fp.Vendor = "Linux"
					fp.DeviceType = "server"
					fp.Confidence = 75
					fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "Samba SMB")
				}
				seen = true
			} else if fp.OSFamily == "" {
				fp.OSFamily = "windows"
				fp.Vendor = "Microsoft"
				fp.DeviceType = "workstation"
				if fp.Confidence < 70 {
					fp.Confidence = 70
					fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "SMB port open")
				}
				seen = true
			}
		}

		// SNMP
		if strings.Contains(service, "snmp") || p.Port == 161 {
			if strings.Contains(banner, "mikrotik") || strings.Contains(banner, "routerboard") {
				fp.OSFamily = "routeros"
				fp.Vendor = "MikroTik"
				fp.DeviceType = "router"
				if fp.Confidence < 90 {
					fp.Confidence = 90
					fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "SNMP MikroTik")
				}
				seen = true
			} else if strings.Contains(banner, "cisco") {
				fp.OSFamily = "ios"
				fp.Vendor = "Cisco"
				fp.DeviceType = "router"
				if fp.Confidence < 85 {
					fp.Confidence = 85
					fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "SNMP Cisco")
				}
				seen = true
			}
		}
	}

	// Third priority: Port profile analysis
	if !seen || fp.Confidence < 60 {
		fp = analyzePortProfile(result, fp)
		if fp.Confidence > 0 {
			seen = true
		}
	}

	if seen {
		result.Fingerprint = &fp
	}
}

func analyzePortProfile(result *ScanResult, fp OSFingerprint) OSFingerprint {
	openPorts := result.OpenPorts()
	portSet := make(map[int]bool)
	for _, p := range openPorts {
		portSet[p.Port] = true
	}

	if portSet[8291] || portSet[8728] || portSet[8729] {
		if fp.OSFamily == "" {
			fp.OSFamily = "routeros"
			fp.Vendor = "MikroTik"
			fp.DeviceType = "router"
			fp.Confidence = 85
			fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "MikroTik port profile")
		}
	}

	if portSet[3389] && (portSet[445] || portSet[139]) {
		if fp.OSFamily == "" {
			fp.OSFamily = "windows"
			fp.Vendor = "Microsoft"
			fp.DeviceType = "workstation"
			fp.Confidence = 75
			fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "Windows port profile")
		}
	}

	if portSet[22] && (portSet[80] || portSet[443]) && !portSet[3389] {
		if fp.OSFamily == "" {
			fp.OSFamily = "linux"
			fp.Vendor = "Linux"
			fp.DeviceType = "server"
			fp.Confidence = 60
			fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "Linux server port profile")
		}
	}

	if portSet[631] || portSet[9100] {
		if fp.DeviceType == "" {
			fp.DeviceType = "printer"
			fp.Confidence = 70
			fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "Printer port profile")
		}
	}

	if portSet[5060] || portSet[5061] {
		if fp.DeviceType == "" {
			fp.DeviceType = "voip"
			fp.Confidence = 70
			fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "VoIP port profile")
		}
	}

	return fp
}

func appendDetectionInfo(existing, new string) string {
	if existing == "" {
		return new
	}
	if strings.Contains(existing, new) {
		return existing
	}
	return existing + ", " + new
}

func extractHostnameFromBanner(banner string) string {
	if strings.HasPrefix(banner, "220 ") {
		parts := strings.SplitN(banner[4:], " ", 2)
		if len(parts) > 0 {
			hostname := strings.TrimSpace(parts[0])
			if isValidHostname(hostname) {
				return hostname
			}
		}
	}

	if strings.HasPrefix(banner, "220 ") && strings.Contains(strings.ToLower(banner), "smtp") {
		parts := strings.SplitN(banner[4:], " ", 2)
		if len(parts) > 0 {
			hostname := strings.TrimSpace(parts[0])
			if isValidHostname(hostname) {
				return hostname
			}
		}
	}
	return ""
}

func isValidHostname(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

func lookupMACVendor(mac string) (vendor string, deviceType string) {
	ouiDBOnce.Do(func() {
		ouiDB = parseOUIDatabase(ieeeOUIDatabase)
		log.Debug().Int("entries", len(ouiDB)).Msg("Loaded IEEE OUI database")
	})

	mac = strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(mac, ":", ""), "-", ""))
	if len(mac) < 6 {
		return "", ""
	}
	oui := mac[:6]

	if vendorName, ok := ouiDB[oui]; ok {
		deviceType = inferDeviceType(vendorName)
		return vendorName, deviceType
	}
	return "", ""
}

func parseOUIDatabase(data []byte) map[string]string {
	db := make(map[string]string, 40000)
	scanner := bufio.NewScanner(bytes.NewReader(data))

	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 8 {
			continue
		}
		oui := strings.ToUpper(line[:6])
		rest := strings.TrimSpace(line[6:])
		if len(rest) > 0 {
			db[oui] = rest
		}
	}
	return db
}

func inferDeviceType(vendor string) string {
	vendorLower := strings.ToLower(vendor)

	keywords := map[string][]string{
		"router":       {"routerboard", "mikrotik", "cisco", "juniper", "huawei", "zte", "ubiquiti", "netgear", "tp-link", "asus", "linksys", "dlink", "zyxel"},
		"access_point": {"cambium", "mimosa", "ruckus", "aruba", "meraki", "ubnt", "unifi", "engenius"},
		"switch":       {"arista", "brocade", "extreme networks", "allied telesis", "dell networking", "mellanox"},
		"firewall":     {"fortinet", "palo alto", "sonicwall", "watchguard", "barracuda", "checkpoint", "sophos"},
		"server":       {"dell", "supermicro", "hpe", "hewlett", "lenovo", "ibm", "oracle", "fujitsu", "inspur"},
		"nas":          {"synology", "qnap", "netapp", "emc", "pure storage"},
		"workstation":  {"apple", "microsoft", "intel", "amd", "nvidia"},
		"iot":          {"raspberry", "espressif", "arduino", "particle", "seeed", "nordic semiconductor"},
		"virtual":      {"vmware", "xen", "virtualbox", "hyper-v", "parallels"},
		"camera":       {"hikvision", "dahua", "axis", "hanwha", "vivotek"},
		"phone":        {"samsung", "xiaomi", "huawei", "oppo", "vivo", "oneplus", "google", "motorola"},
	}

	for dtype, kws := range keywords {
		for _, kw := range kws {
			if strings.Contains(vendorLower, kw) {
				return dtype
			}
		}
	}

	return "unknown"
}

func extractVersionFromBanner(banner string) string {
	reList := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(routeros|mikrotik)[/\\s]*v?(\d+\.\d+(?:\.\d+)?)`),
		regexp.MustCompile(`(?i)openssh[_/\\s]*(\d+\.\d+(?:p\d+)?)`),
		regexp.MustCompile(`(?i)iis/(\d+\.\d+)`),
		regexp.MustCompile(`(?i)(ubuntu|debian)[/\\s]*(\d+\.\d+)`),
	}
	for _, re := range reList {
		if m := re.FindStringSubmatch(banner); len(m) > 2 {
			return m[2]
		}
		if m := re.FindStringSubmatch(banner); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

func isWebPageService(service, banner string) bool {
	bannerLower := strings.ToLower(banner)
	if strings.Contains(bannerLower, "<!doctype html") ||
		strings.Contains(bannerLower, "<html") ||
		(strings.Contains(bannerLower, "<head>") && strings.Contains(bannerLower, "<body")) {
		return true
	}
	if strings.Contains(bannerLower, "content-type:") &&
		strings.Contains(bannerLower, "text/html") {
		return true
	}
	serviceLower := strings.ToLower(service)
	if strings.Contains(serviceLower, "http-html") ||
		strings.Contains(serviceLower, "webfig") ||
		strings.Contains(serviceLower, "http-admin") ||
		strings.Contains(serviceLower, "http-mgmt") {
		return true
	}
	return false
}

func detectProtocol(banner []byte, port int) string {
	bannerStr := string(banner)
	bannerLower := strings.ToLower(bannerStr)
	if len(banner) > 0 {
		if len(banner) > 2 && banner[0] == 0x4d && banner[1] == 0x32 {
			return "tcp/winbox"
		}
		if len(banner) > 4 && banner[2] == 0x4d && banner[3] == 0x32 {
			return "tcp/winbox"
		}
		if strings.Contains(bannerLower, "mikrotik") {
			return "tcp/unknown-mikrotik"
		}
	}
	return ""
}

// checkTLS tries to perform a TLS handshake (send ClientHello) to detect SSL/TLS service
func checkTLS(conn net.Conn, timeout time.Duration) (bool, error) {
	clientHello := []byte{
		0x16,       // Content Type: Handshake
		0x03, 0x01, // Version: TLS 1.0
		0x00, 0x2d, // Length: 45 bytes
		0x01,             // Handshake Type: Client Hello
		0x00, 0x00, 0x29, // Length: 41 bytes
		0x03, 0x03, // Version: TLS 1.2
		// Random
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		0x00,       // Session ID length: 0
		0x00, 0x02, // Cipher Suites length: 2
		0x00, 0x2f, // Cipher Suite: TLS_RSA_WITH_AES_128_CBC_SHA
		0x01, // Compression methods length
		0x00, // Compression method: null
	}

	conn.SetWriteDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(clientHello); err != nil {
		return false, err
	}

	conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil || n < 1 {
		return false, err
	}

	if buf[0] == 0x16 {
		if n > 2 && buf[1] == 0x03 {
			return true, nil
		}
	} else if buf[0] == 0x15 {
		return true, nil
	}

	return false, nil
}

// getUDPServiceByPort returns the well-known service name for a UDP port
func getUDPServiceByPort(port int) string {
	switch port {
	case 53:
		return "udp/dns"
	case 161:
		return "udp/snmp"
	case 123:
		return "udp/ntp"
	case 8291:
		return "udp/mikrotik-winbox"
	case 51820:
		return "udp/wireguard"
	default:
		return "udp/unknown"
	}
}

// detectUDPServiceFromResponse tries to identify UDP service from response data
func detectUDPServiceFromResponse(port int, response []byte) string {
	if len(response) == 0 {
		return ""
	}
	// Simplified detection logic
	if port == 53 && len(response) > 12 {
		return "udp/dns"
	}
	return ""
}

// getUDPProbe returns a protocol-specific probe for the given port
func getUDPProbe(port int) []byte {
	return []byte{0x00} // Default empty for now
}

// mapNmapOSToFingerprint maps Nmap detected OS/Product to our fingerprint struct
func mapNmapOSToFingerprint(fp *OSFingerprint, info *NmapServiceInfo) bool {
	found := false

	// Check OS Type directly
	if info.OSType != "" {
		switch strings.ToLower(info.OSType) {
		case "linux":
			if fp.OSFamily == "" {
				fp.OSFamily = "linux"
				fp.Vendor = "Linux"
				fp.DeviceType = "server"
				found = true
			}
		case "windows", "windows_server":
			if fp.OSFamily == "" {
				fp.OSFamily = "windows"
				fp.Vendor = "Microsoft"
				found = true
			}
		case "routeros":
			fp.OSFamily = "routeros"
			fp.Vendor = "MikroTik"
			fp.DeviceType = "router"
			found = true
		case "ios":
			fp.OSFamily = "ios"
			fp.Vendor = "Cisco"
			fp.DeviceType = "router"
			found = true
		case "macos", "macosx":
			fp.OSFamily = "macos"
			fp.Vendor = "Apple"
			fp.DeviceType = "workstation"
			found = true
		}
		if found {
			fp.DetectionInfo = appendDetectionInfo(fp.DetectionInfo, "Nmap OS: "+info.OSType)
			fp.Confidence = 90
		}
	}

	// Check CPEs
	for _, cpe := range info.CPEs {
		// cpe:/o:mikrotik:routeros:6.48.6
		parts := strings.Split(cpe, ":")
		if len(parts) >= 5 && parts[1] == "o" {
			vendor := parts[2]
			product := parts[3]
			version := parts[4]

			if vendor == "mikrotik" && product == "routeros" {
				fp.OSFamily = "routeros"
				fp.Vendor = "MikroTik"
				fp.DeviceType = "router"
				fp.OSVersion = version
				fp.Confidence = 99
				found = true
			} else if vendor == "linux" {
				if fp.OSFamily == "" {
					fp.OSFamily = "linux"
					found = true
				}
			} else if vendor == "microsoft" && strings.HasPrefix(product, "windows") {
				fp.OSFamily = "windows"
				fp.Vendor = "Microsoft"
				found = true
			}
		}
	}

	// Identify Device Type
	if info.DeviceType != "" && fp.DeviceType == "" {
		fp.DeviceType = info.DeviceType
	}

	// Identify Version from Product/Version fields if not set
	if fp.OSVersion == "" && info.Version != "" {
		// If product matches OS family, use version
		if strings.EqualFold(info.Product, fp.OSFamily) || strings.Contains(strings.ToLower(info.Product), fp.OSFamily) {
			fp.OSVersion = info.Version
		}
	}

	return found
}
