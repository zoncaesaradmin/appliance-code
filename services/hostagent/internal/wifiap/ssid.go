package wifiap

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	ssidSuffix = "-AP"
	maxSSIDLen = 32
	minPSKLen  = 8
	maxPSKLen  = 63
)

// DeriveSSID builds <sanitized-base>-AP within the 32-octet 802.11 limit.
func DeriveSSID(base string) (string, error) {
	sanitized := sanitizeSSIDBase(base)
	if sanitized == "" {
		return "", fmt.Errorf("wifiap: empty SSID base after sanitization")
	}
	suffix := ssidSuffix
	maxBase := maxSSIDLen - len(suffix)
	if maxBase < 1 {
		return "", fmt.Errorf("wifiap: SSID length budget exhausted")
	}
	if len(sanitized) > maxBase {
		sanitized = sanitized[:maxBase]
		sanitized = strings.Trim(sanitized, "-")
	}
	if sanitized == "" {
		return "", fmt.Errorf("wifiap: empty SSID base after truncation")
	}
	return sanitized + suffix, nil
}

func sanitizeSSIDBase(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	// Prefer short hostname label when a FQDN is provided.
	if i := strings.Index(value, "."); i > 0 {
		value = value[:i]
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// ValidatePSK enforces WPA2-PSK passphrase length rules.
func ValidatePSK(psk string) error {
	if len(psk) < minPSKLen || len(psk) > maxPSKLen {
		return fmt.Errorf("wifiap: psk must be between %d and %d characters", minPSKLen, maxPSKLen)
	}
	for _, r := range psk {
		if r < 32 || r > 126 {
			return fmt.Errorf("wifiap: psk must be printable ASCII")
		}
	}
	return nil
}
