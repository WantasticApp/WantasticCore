package tenant

import "strings"

// ParsedUserAgent contains parsed browser and OS info from user agent string
type ParsedUserAgent struct {
	Browser        string
	BrowserVersion string
	OS             string
	DeviceType     string
}

// ParseUserAgent extracts browser, version, OS, and device type from a user agent string
func ParseUserAgent(ua string) *ParsedUserAgent {
	result := &ParsedUserAgent{
		Browser:        "Unknown",
		BrowserVersion: "",
		OS:             "Unknown",
		DeviceType:     "Desktop",
	}

	if ua == "" {
		return result
	}

	uaLower := strings.ToLower(ua)

	// Detect device type
	if strings.Contains(uaLower, "mobile") || strings.Contains(uaLower, "android") && !strings.Contains(uaLower, "tablet") {
		result.DeviceType = "Mobile"
	} else if strings.Contains(uaLower, "tablet") || strings.Contains(uaLower, "ipad") {
		result.DeviceType = "Tablet"
	}

	// Detect OS
	switch {
	case strings.Contains(uaLower, "windows nt 10") || strings.Contains(uaLower, "windows 10"):
		result.OS = "Windows 10/11"
	case strings.Contains(uaLower, "windows nt 6.3") || strings.Contains(uaLower, "windows 8.1"):
		result.OS = "Windows 8.1"
	case strings.Contains(uaLower, "windows nt 6.2") || strings.Contains(uaLower, "windows 8"):
		result.OS = "Windows 8"
	case strings.Contains(uaLower, "windows nt 6.1") || strings.Contains(uaLower, "windows 7"):
		result.OS = "Windows 7"
	case strings.Contains(uaLower, "windows"):
		result.OS = "Windows"
	case strings.Contains(uaLower, "mac os x") || strings.Contains(uaLower, "macos") || strings.Contains(uaLower, "macintosh"):
		result.OS = "macOS"
	case strings.Contains(uaLower, "iphone"):
		result.OS = "iOS"
		result.DeviceType = "Mobile"
	case strings.Contains(uaLower, "ipad"):
		result.OS = "iPadOS"
		result.DeviceType = "Tablet"
	case strings.Contains(uaLower, "android"):
		result.OS = "Android"
	case strings.Contains(uaLower, "linux") && !strings.Contains(uaLower, "android"):
		result.OS = "Linux"
	case strings.Contains(uaLower, "cros"):
		result.OS = "Chrome OS"
	case strings.Contains(uaLower, "x11"):
		result.OS = "UNIX"
	}

	// Detect browser and version (order matters - check specific browsers first)
	// Edge (Chromium-based)
	if idx := strings.Index(ua, "Edg/"); idx != -1 {
		result.Browser = "Edge"
		result.BrowserVersion = extractVersion(ua[idx+4:])
	} else if idx := strings.Index(ua, "Edge/"); idx != -1 {
		result.Browser = "Edge"
		result.BrowserVersion = extractVersion(ua[idx+5:])
		// Opera
	} else if idx := strings.Index(ua, "OPR/"); idx != -1 {
		result.Browser = "Opera"
		result.BrowserVersion = extractVersion(ua[idx+4:])
		// Brave
	} else if strings.Contains(uaLower, "brave") {
		result.Browser = "Brave"
		if idx := strings.Index(ua, "Chrome/"); idx != -1 {
			result.BrowserVersion = extractVersion(ua[idx+7:])
		}
		// Chrome
	} else if idx := strings.Index(ua, "Chrome/"); idx != -1 && !strings.Contains(uaLower, "chromium") {
		result.Browser = "Chrome"
		result.BrowserVersion = extractVersion(ua[idx+7:])
		// Chromium
	} else if idx := strings.Index(ua, "Chromium/"); idx != -1 {
		result.Browser = "Chromium"
		result.BrowserVersion = extractVersion(ua[idx+9:])
		// Firefox
	} else if idx := strings.Index(ua, "Firefox/"); idx != -1 {
		result.Browser = "Firefox"
		result.BrowserVersion = extractVersion(ua[idx+8:])
		// Safari (must check after Chrome since Chrome UA contains Safari)
	} else if strings.Contains(uaLower, "safari") && !strings.Contains(uaLower, "chrome") && !strings.Contains(uaLower, "android") {
		result.Browser = "Safari"
		if idx := strings.Index(ua, "Version/"); idx != -1 {
			result.BrowserVersion = extractVersion(ua[idx+8:])
		}
	} else {
		// Fallback: use first definition looking specific
		if result.Browser == "Unknown" && len(ua) < 30 && len(ua) > 3 {
			result.Browser = ua
		}
	}

	return result
}

// extractVersion extracts the version number from a string starting with the version
func extractVersion(s string) string {
	var version strings.Builder
	for _, c := range s {
		if (c >= '0' && c <= '9') || c == '.' {
			version.WriteRune(c)
		} else {
			break
		}
	}
	v := version.String()
	// Return just major.minor (e.g., "120.0" instead of "120.0.6099.129")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return v
}
