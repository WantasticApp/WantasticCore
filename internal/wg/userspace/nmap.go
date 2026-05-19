package userspace

import (
	"bufio"
	"bytes"
	"embed"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed nmap-services nmap-protocols nmap-service-probes nmap-os-db
var nmapFS embed.FS

// NmapServiceDBMap holds parsed nmap-services data
type NmapServiceDBMap map[string]string // "port/proto" -> "service-name"

// NmapProtocolDBMap holds parsed nmap-protocols data
type NmapProtocolDBMap map[int]string // protocol number -> "protocol-name"

// NmapMatchRule represents a 'match' directive
type NmapMatchRule struct {
	Service     string
	Pattern     *regexp.Regexp
	VersionInfo string
}

// NmapServiceProbe represents a 'Probe' directive and its associated matches
type NmapServiceProbe struct {
	Name      string
	String    []byte
	Format    string // q|...|
	Ports     []int  // tcp ports
	SSLPorts  []int
	Matches   []NmapMatchRule
	Rarity    int
	TotalWait time.Duration
}

// NmapServiceProbeDB holds all parsed probes
type NmapServiceProbeDB struct {
	Probes []*NmapServiceProbe
}

var (
	globalServiceDB  NmapServiceDBMap
	globalProtocolDB NmapProtocolDBMap
	globalProbes     *NmapServiceProbeDB

	loadServicesOnce  sync.Once
	loadProtocolsOnce sync.Once
	loadProbesOnce    sync.Once
)

// GetGlobalNmapServiceDB returns the singleton service DB
func GetGlobalNmapServiceDB() NmapServiceDBMap {
	loadServicesOnce.Do(func() {
		f, err := nmapFS.Open("nmap-services")
		if err == nil {
			defer f.Close()
			globalServiceDB, _ = LoadNmapServiceDB(f)
		}
	})
	return globalServiceDB
}

// GetGlobalNmapProtocolDB returns the singleton protocol DB
func GetGlobalNmapProtocolDB() NmapProtocolDBMap {
	loadProtocolsOnce.Do(func() {
		f, err := nmapFS.Open("nmap-protocols")
		if err == nil {
			defer f.Close()
			globalProtocolDB, _ = LoadNmapProtocolDB(f)
		}
	})
	return globalProtocolDB
}

// GetGlobalNmapProbes returns the singleton probe DB
func GetGlobalNmapProbes() *NmapServiceProbeDB {
	loadProbesOnce.Do(func() {
		f, err := nmapFS.Open("nmap-service-probes")
		if err == nil {
			defer f.Close()
			globalProbes, _ = LoadNmapServiceProbes(f)
		}
	})
	return globalProbes
}

// LoadNmapServiceDB parses nmap-services data
func LoadNmapServiceDB(r io.Reader) (NmapServiceDBMap, error) {
	db := make(NmapServiceDBMap)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			serviceName := parts[0]
			portProto := parts[1]
			db[portProto] = serviceName
		}
	}
	return db, scanner.Err()
}

// LoadNmapProtocolDB parses nmap-protocols data
func LoadNmapProtocolDB(r io.Reader) (NmapProtocolDBMap, error) {
	db := make(NmapProtocolDBMap)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 3 {
			name := parts[0]
			num, err := strconv.Atoi(parts[1])
			if err == nil {
				db[num] = name
			}
		}
	}
	return db, scanner.Err()
}

// LoadNmapServiceProbes parses nmap-service-probes data
func LoadNmapServiceProbes(r io.Reader) (*NmapServiceProbeDB, error) {
	db := &NmapServiceProbeDB{}
	var currentProbe *NmapServiceProbe

	scanner := bufio.NewScanner(r)
	// Increase buffer size for long lines
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "Probe ") {
			// Probe <protocol> <probename> <probestring>
			parts := strings.SplitN(line, " ", 4)
			if len(parts) < 4 {
				continue
			}
			proto := parts[1]
			if proto != "TCP" && proto != "UDP" {
				continue // Only support TCP/UDP for now
			}
			name := parts[2]
			probeStr := parts[3]

			// Parse probe string q|...|
			raw, err := parseNmapProbeString(probeStr)
			if err != nil {
				continue
			}

			currentProbe = &NmapServiceProbe{
				Name:   name,
				String: raw,
				Format: probeStr,
			}
			db.Probes = append(db.Probes, currentProbe)

		} else if strings.HasPrefix(line, "match ") && currentProbe != nil {
			// match <service> <pattern> <versioninfo>
			parts := strings.SplitN(line, " ", 3)
			if len(parts) < 3 {
				continue
			}
			service := parts[1]
			remainder := parts[2]

			// Parse pattern
			// Nmap match format: match <service> m|pattern|[flags] [versioninfo]
			if len(remainder) < 3 || remainder[0] != 'm' {
				continue
			}
			delim := remainder[1]
			endIdx := strings.IndexByte(remainder[2:], delim)
			if endIdx == -1 {
				continue
			}
			endIdx += 2 // Adjust for offset (m|)

			patternStr := remainder[2:endIdx]

			// flags and version info follow
			rest := remainder[endIdx+1:]

			flagEnd := strings.IndexByte(rest, ' ')
			var flags, versionInfo string
			if flagEnd == -1 {
				flags = rest
			} else {
				flags = rest[:flagEnd]
				versionInfo = strings.TrimSpace(rest[flagEnd:])
			}

			if strings.Contains(flags, "s") {
				patternStr = "(?s)" + patternStr
			}
			if strings.Contains(flags, "i") {
				patternStr = "(?i)" + patternStr
			}

			re, err := regexp.Compile(patternStr)
			if err != nil {
				continue // Skip invalid regex
			}

			currentProbe.Matches = append(currentProbe.Matches, NmapMatchRule{
				Service:     service,
				Pattern:     re,
				VersionInfo: versionInfo,
			})

		} else if strings.HasPrefix(line, "ports ") && currentProbe != nil {
			currentProbe.Ports = parseNmapPorts(strings.TrimPrefix(line, "ports "))
		} else if strings.HasPrefix(line, "sslports ") && currentProbe != nil {
			currentProbe.SSLPorts = parseNmapPorts(strings.TrimPrefix(line, "sslports "))
		} else if strings.HasPrefix(line, "totalwaitms ") && currentProbe != nil {
			ms, _ := strconv.Atoi(strings.TrimPrefix(line, "totalwaitms "))
			currentProbe.TotalWait = time.Duration(ms) * time.Millisecond
		} else if strings.HasPrefix(line, "rarity ") && currentProbe != nil {
			currentProbe.Rarity, _ = strconv.Atoi(strings.TrimPrefix(line, "rarity "))
		}
	}

	return db, scanner.Err()
}

func parseNmapProbeString(s string) ([]byte, error) {
	if !strings.HasPrefix(s, "q|") || !strings.HasSuffix(s, "|") {
		return nil, fmt.Errorf("invalid probe format")
	}
	inner := s[2 : len(s)-1]
	var buf bytes.Buffer
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			i++
			switch inner[i] {
			case '0':
				buf.WriteByte(0x00)
			case 'a':
				buf.WriteByte(0x07)
			case 'b':
				buf.WriteByte(0x08)
			case 't':
				buf.WriteByte(0x09)
			case 'n':
				buf.WriteByte(0x0a)
			case 'v':
				buf.WriteByte(0x0b)
			case 'f':
				buf.WriteByte(0x0c)
			case 'r':
				buf.WriteByte(0x0d)
			case '\\':
				buf.WriteByte('\\')
			case 'x':
				if i+2 < len(inner) {
					hex := inner[i+1 : i+3]
					val, err := strconv.ParseUint(hex, 16, 8)
					if err == nil {
						buf.WriteByte(byte(val))
						i += 2
					} else {
						buf.WriteByte('x') // Fallback
					}
				} else {
					buf.WriteByte('x')
				}
			default:
				buf.WriteByte(inner[i])
			}
		} else {
			buf.WriteByte(inner[i])
		}
	}
	return buf.Bytes(), nil
}

func parseNmapPorts(s string) []int {
	var ports []int
	parts := strings.Split(s, ",")
	for _, p := range parts {
		if strings.Contains(p, "-") {
			rangeParts := strings.Split(p, "-")
			if len(rangeParts) == 2 {
				start, _ := strconv.Atoi(rangeParts[0])
				end, _ := strconv.Atoi(rangeParts[1])
				for i := start; i <= end; i++ {
					ports = append(ports, i)
				}
			}
		} else {
			port, _ := strconv.Atoi(p)
			ports = append(ports, port)
		}
	}
	return ports
}

// ApplyVersionSubstitutions replaces $1, $2 etc in version info with captured groups
// Returns a structured NmapServiceInfo object
func (m *NmapMatchRule) ApplyVersionSubstitutions(matches []string) *NmapServiceInfo {
	info := &NmapServiceInfo{
		Service: m.Service,
	}

	if m.VersionInfo == "" {
		return info
	}

	result := m.VersionInfo
	for i, match := range matches {
		if i == 0 {
			continue
		} // Skip full match
		placeholder := fmt.Sprintf("$%d", i)
		result = strings.ReplaceAll(result, placeholder, match)
	}

	// Parse the flag/info string (e.g., "p/Postfix smtpd/ v/2.0.1/ o/Linux/ cpe:/a:postfix:postfix:2.0.1/")

	parseField := func(prefix string) string {
		if idx := strings.Index(result, prefix); idx != -1 {
			start := idx + len(prefix)
			// Find the delimiter (usually /)
			// Actually the format is p/value/
			// We need to find the next / but handle escaping if necessary?
			// Nmap usually doesn't escape / inside, it just ends options.
			// Let's assume / is the delimiter.
			rest := result[start:]
			if end := strings.Index(rest, "/"); end != -1 {
				return rest[:end]
			}
		}
		return ""
	}

	info.Product = parseField("p/")
	info.Version = parseField("v/")
	info.Extrainfo = parseField("i/")
	info.Hostname = parseField("h/")
	info.OSType = parseField("o/")
	info.DeviceType = parseField("d/")

	// CPEs can be multiple
	for _, part := range strings.Split(result, " ") {
		if strings.HasPrefix(part, "cpe:/") {
			// Extract until next space or end
			// Usually Nmap CPEs are space separated or part of the string
			// result string is something like "p/foo/ cpe:/a:foo:bar/"
			// But we are parsing from the 'substituted' string.

			// Let's re-parse CPEs properly from the full string
			cpeStart := strings.Index(part, "cpe:/")
			if cpeStart != -1 {
				// Check where it ends (slash?)
				// Standard: cpe:/.../
				end := strings.LastIndex(part, "/")
				if end > cpeStart {
					info.CPEs = append(info.CPEs, part[cpeStart:end+1])
				}
			}
		}
	}

	// Better CPE parsing loop
	// Iterate through "cpe:/" occurrences
	scanObj := result
	for {
		idx := strings.Index(scanObj, "cpe:/")
		if idx == -1 {
			break
		}
		remainder := scanObj[idx:]
		end := strings.Index(remainder[5:], "/") // Find closing slash
		if end != -1 {
			// Found one
			cpe := remainder[:5+end+1]
			// Avoid duplicates
			exists := false
			for _, c := range info.CPEs {
				if c == cpe {
					exists = true
					break
				}
			}
			if !exists {
				info.CPEs = append(info.CPEs, cpe)
			}
			scanObj = remainder[5+end+1:]
		} else {
			break
		}
	}

	return info
}

// NmapServiceInfo holds structured service detection results
type NmapServiceInfo struct {
	Service    string
	Product    string
	Version    string
	Extrainfo  string
	Hostname   string
	OSType     string
	DeviceType string
	CPEs       []string
}

func (i *NmapServiceInfo) String() string {
	var parts []string
	if i.Product != "" {
		parts = append(parts, i.Product)
	}
	if i.Version != "" {
		parts = append(parts, i.Version)
	}
	if i.Extrainfo != "" {
		parts = append(parts, "("+i.Extrainfo+")")
	}
	if i.OSType != "" {
		parts = append(parts, "OS: "+i.OSType)
	}
	if i.DeviceType != "" {
		parts = append(parts, "Device: "+i.DeviceType)
	}
	if len(parts) == 0 {
		return i.Service
	}
	return i.Service + " " + strings.Join(parts, " ")
}

// -----------------------------------------------------------------------------
// OS Parsing
// -----------------------------------------------------------------------------

// NmapOSFingerprint represents an entry in nmap-os-db
type NmapOSFingerprint struct {
	FingerprintName string
	Class           []string // e.g. "Class 2N | embedded || specialized"
	CPEs            []string
	// We store the raw lines for now as we might not implement full TCP matching engine yet
	MatchPoints []string
}

var (
	globalOSDB   []*NmapOSFingerprint
	loadOSDBOnce sync.Once
)

// GetGlobalNmapOSDB returns the singleton OS DB
func GetGlobalNmapOSDB() []*NmapOSFingerprint {
	loadOSDBOnce.Do(func() {
		f, err := nmapFS.Open("nmap-os-db")
		if err == nil {
			defer f.Close()
			globalOSDB, _ = LoadNmapOSDB(f)
		}
	})
	return globalOSDB
}

// LoadNmapOSDB parses nmap-os-db
func LoadNmapOSDB(r io.Reader) ([]*NmapOSFingerprint, error) {
	var db []*NmapOSFingerprint
	var current *NmapOSFingerprint

	scanner := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "Fingerprint ") {
			if current != nil {
				db = append(db, current)
			}
			current = &NmapOSFingerprint{
				FingerprintName: strings.TrimPrefix(line, "Fingerprint "),
			}
		} else if current != nil {
			if strings.HasPrefix(line, "Class ") {
				current.Class = append(current.Class, strings.TrimPrefix(line, "Class "))
			} else if strings.HasPrefix(line, "CPE ") {
				current.CPEs = append(current.CPEs, strings.TrimPrefix(line, "CPE "))
			} else {
				// Match lines like SEQ(...), OPS(...), etc.
				current.MatchPoints = append(current.MatchPoints, line)
			}
		}
	}
	if current != nil {
		db = append(db, current)
	}

	return db, scanner.Err()
}
