package wificlient

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"appliance-code/services/hostagent/internal/wifiap"
)

var (
	ifaceLineRE     = regexp.MustCompile(`(?m)^\s*Interface\s+(\S+)`)
	typeLineRE      = regexp.MustCompile(`(?m)^\s*type\s+(\S+)`)
	phyLineRE       = regexp.MustCompile(`(?m)^Wiphy\s+(\S+)`)
	apCapRE         = regexp.MustCompile(`(?m)^\s*\*\s+AP\b`)
	apWordRE        = regexp.MustCompile(`(?i)\bap\b`)
	maxInterfacesRE = regexp.MustCompile(`<=\s*([0-9]+)`)
)

type radioCandidate struct {
	Iface     string
	Phy       string
	Type      string
	APCapable bool
}

type radioInventory struct {
	candidates []radioCandidate
	managed    []radioCandidate
	apActive   []radioCandidate
	probeKnown bool
	phyCaps    map[string]phyCapability
}

type phyCapability struct {
	AP                  bool
	ConcurrentKnown     bool
	ConcurrentManagedAP bool
}

func inspectRadios(ctx context.Context, runner wifiap.Runner) (radioInventory, error) {
	if _, err := runner.LookPath("iw"); err != nil {
		return radioInventory{}, nil
	}
	devs, err := runner.CombinedOutput(ctx, "iw", "dev")
	if err != nil {
		return radioInventory{}, nil
	}
	inv := radioInventory{candidates: parseIWDev(devs), probeKnown: true}
	if len(inv.candidates) == 0 {
		return inv, nil
	}
	listOut, listErr := runner.CombinedOutput(ctx, "iw", "list")
	if listErr == nil && strings.TrimSpace(listOut) != "" {
		inv.phyCaps = parsePHYCapabilities(listOut)
	}
	for i := range inv.candidates {
		if len(inv.phyCaps) == 0 {
			inv.candidates[i].APCapable = true
		} else if inv.candidates[i].Phy != "" {
			inv.candidates[i].APCapable = inv.phyCaps[inv.candidates[i].Phy].AP
		}
		switch inv.candidates[i].Type {
		case "managed":
			inv.managed = append(inv.managed, inv.candidates[i])
		case "ap":
			inv.apActive = append(inv.apActive, inv.candidates[i])
		}
	}
	return inv, nil
}

func (inv radioInventory) defaultManagedIface() string {
	if len(inv.managed) == 0 {
		return ""
	}
	return inv.managed[0].Iface
}

func (inv radioInventory) supportedCapable() bool {
	return len(inv.managed) > 0
}

func (inv radioInventory) clientCapability() (state, detail string) {
	if !inv.probeKnown {
		return CapabilityUnknown, "The wireless driver did not return a client interface inventory."
	}
	if len(inv.managed) > 0 {
		return CapabilitySupported, "A client-capable Wi-Fi interface is detected."
	}
	if len(inv.apActive) > 0 {
		return CapabilityLimited, "The detected Wi-Fi interface is currently in AP mode; no client interface is available to this appliance."
	}
	return CapabilityUnsupported, "The host did not detect a client-capable Wi-Fi interface."
}

func (inv radioInventory) unavailableReason() string {
	if len(inv.apActive) > 0 {
		return ReasonRadioInUseByAP
	}
	return ReasonNoHardware
}

func (inv radioInventory) concurrentAPSupport() (state string, supported bool, detail string) {
	if !inv.probeKnown {
		return CapabilityUnknown, false, "The wireless driver did not return an interface inventory, so simultaneous client Wi-Fi and AP capability cannot be assessed."
	}
	if len(inv.managed) == 0 {
		return CapabilityUnsupported, false, "No client-capable Wi-Fi interface is currently detected."
	}
	if len(inv.phyCaps) == 0 {
		return CapabilityUnknown, false, "The driver did not expose AP-mode capability details, so simultaneous client Wi-Fi and AP capability cannot be assessed."
	}
	clientPHY := inv.managed[0].Phy
	clientCaps, clientKnown := inv.phyCaps[clientPHY]
	if !clientKnown {
		return CapabilityUnknown, false, "The driver did not map the client interface to an AP capability record."
	}
	for _, candidate := range inv.candidates {
		if candidate.Iface != inv.managed[0].Iface && candidate.APCapable {
			return CapabilitySupported, true, "Separate usable Wi-Fi interfaces are detected for client Wi-Fi and Wi-Fi AP."
		}
	}
	if !clientCaps.AP {
		for phy, caps := range inv.phyCaps {
			if phy != clientPHY && caps.AP {
				return CapabilityLimited, false, "An AP-capable radio exists, but the appliance does not currently have a separate usable interface for it."
			}
		}
		return CapabilityUnsupported, false, "The client Wi-Fi radio does not report AP mode, and no separate AP-capable radio is detected."
	}
	if !clientCaps.ConcurrentKnown {
		return CapabilityUnknown, false, "The driver reports client and AP modes but does not expose their simultaneous-mode limit."
	}
	if clientCaps.ConcurrentManagedAP {
		return CapabilityLimited, false, "Hardware reports simultaneous client and AP mode on one radio, but this appliance requires a separate usable interface to enable both at once."
	}
	return CapabilityUnsupported, false, "The wireless driver reports client and AP modes but no simultaneous client-plus-AP combination."
}

func parseIWDev(out string) []radioCandidate {
	lines := strings.Split(out, "\n")
	var current *radioCandidate
	var all []radioCandidate
	flush := func() {
		if current != nil && current.Iface != "" {
			all = append(all, *current)
		}
		current = nil
	}
	for _, line := range lines {
		if m := ifaceLineRE.FindStringSubmatch(line); len(m) == 2 {
			flush()
			current = &radioCandidate{Iface: strings.TrimSpace(m[1])}
			continue
		}
		if current == nil {
			continue
		}
		if m := typeLineRE.FindStringSubmatch(line); len(m) == 2 {
			current.Type = strings.ToLower(strings.TrimSpace(m[1]))
		}
		if strings.Contains(line, "wiphy") {
			fields := strings.Fields(line)
			for i, f := range fields {
				if f == "wiphy" && i+1 < len(fields) {
					current.Phy = "phy" + fields[i+1]
					break
				}
			}
		}
	}
	flush()
	sort.Slice(all, func(i, j int) bool {
		return all[i].Iface < all[j].Iface
	})
	return all
}

func physWithAP(iwList string) map[string]bool {
	result := map[string]bool{}
	for phy, caps := range parsePHYCapabilities(iwList) {
		result[phy] = caps.AP
	}
	return result
}

func parsePHYCapabilities(iwList string) map[string]phyCapability {
	result := map[string]phyCapability{}
	var current string
	inCombinations := false
	for _, line := range strings.Split(iwList, "\n") {
		if m := phyLineRE.FindStringSubmatch(line); len(m) == 2 {
			current = m[1]
			inCombinations = false
			continue
		}
		if current == "" {
			continue
		}
		caps := result[current]
		if apCapRE.MatchString(line) {
			caps.AP = true
		}
		if strings.EqualFold(strings.TrimSpace(line), "valid interface combinations:") {
			caps.ConcurrentKnown = true
			inCombinations = true
		}
		if inCombinations {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "managed") && apWordRE.MatchString(line) {
				if match := maxInterfacesRE.FindStringSubmatch(line); len(match) == 2 && match[1] != "0" && match[1] != "1" {
					caps.ConcurrentManagedAP = true
				}
			}
		}
		result[current] = caps
	}
	return result
}

func linkState(ctx context.Context, runner wifiap.Runner, iface string) (ssid string, connected bool, err error) {
	out, err := runner.CombinedOutput(ctx, "iw", "dev", iface, "link")
	if err != nil {
		return "", false, nil
	}
	low := strings.ToLower(out)
	if strings.Contains(low, "not connected") {
		return "", false, nil
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SSID:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "SSID:")), true, nil
		}
	}
	if strings.Contains(low, "connected to") {
		return "", true, nil
	}
	return "", false, nil
}

func ipv4Addresses(ctx context.Context, runner wifiap.Runner, iface string) ([]string, error) {
	out, err := runner.CombinedOutput(ctx, "ip", "-4", "-o", "addr", "show", "dev", iface)
	if err != nil {
		return nil, nil
	}
	seen := map[string]bool{}
	var addrs []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		for i, field := range fields {
			if field == "inet" && i+1 < len(fields) {
				addr := fields[i+1]
				if slash := strings.Index(addr, "/"); slash > 0 {
					addr = addr[:slash]
				}
				addr = strings.TrimSpace(addr)
				if addr != "" && !seen[addr] {
					seen[addr] = true
					addrs = append(addrs, addr)
				}
			}
		}
	}
	sort.Strings(addrs)
	return addrs, nil
}

func scanNetworks(ctx context.Context, runner wifiap.Runner, iface string) ([]ScanNetwork, error) {
	out, err := runner.CombinedOutput(ctx, "iw", "dev", iface, "scan")
	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}
	type partial struct {
		ssid       string
		security   string
		requires   bool
		signal     int
		authSuites bool
	}
	best := map[string]partial{}
	var current partial
	commit := func() {
		if strings.TrimSpace(current.ssid) == "" {
			current = partial{}
			return
		}
		existing, ok := best[current.ssid]
		if !ok || current.signal > existing.signal {
			best[current.ssid] = current
		}
		current = partial{}
	}
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "BSS ") {
			commit()
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "SSID:"):
			current.ssid = strings.TrimSpace(strings.TrimPrefix(trimmed, "SSID:"))
		case strings.HasPrefix(trimmed, "signal:"):
			value := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "signal:"), "dBm"))
			if f, err := strconv.ParseFloat(strings.Fields(value)[0], 64); err == nil {
				current.signal = int(f)
			}
		case strings.HasPrefix(trimmed, "Authentication suites:"):
			current.authSuites = true
			if strings.Contains(trimmed, ":1") {
				current.security = SecurityEnterprise
				current.requires = true
			}
		case current.authSuites && strings.HasPrefix(trimmed, "*"):
			if strings.Contains(trimmed, ":1") {
				current.security = SecurityEnterprise
				current.requires = true
			} else if strings.Contains(trimmed, ":8") && current.security != SecurityEnterprise {
				current.security = SecurityWPA3SAE
				current.requires = true
			} else if strings.Contains(trimmed, ":2") && current.security != SecurityEnterprise {
				current.security = SecurityWPA2PSK
				current.requires = true
			}
		case strings.HasPrefix(trimmed, "RSN:"):
			current.authSuites = false
			current.security = SecurityWPA2PSK
			current.requires = true
		case strings.HasPrefix(trimmed, "WPA:"):
			current.authSuites = false
			if current.security == "" {
				current.security = SecurityWPAPSK
			}
			current.requires = true
		case strings.Contains(trimmed, "SAE"):
			current.authSuites = false
			current.security = SecurityWPA3SAE
			current.requires = true
		case strings.HasPrefix(trimmed, "capability:") && strings.Contains(trimmed, "Privacy"):
			current.authSuites = false
			if current.security == "" {
				current.security = SecuritySecured
			}
			current.requires = true
		default:
			current.authSuites = false
		}
	}
	commit()
	var networks []ScanNetwork
	for _, item := range best {
		security := item.security
		if security == "" {
			security = SecurityOpen
		}
		connectable := security != SecurityEnterprise && security != SecuritySecured && security != SecurityUnknown
		unsupportedDetail := ""
		if !connectable {
			unsupportedDetail = "This appliance supports open and WPA/WPA2/WPA3 personal Wi-Fi networks."
		}
		networks = append(networks, ScanNetwork{
			SSID:              item.ssid,
			Security:          security,
			RequiresPassword:  item.requires,
			Connectable:       connectable,
			UnsupportedDetail: unsupportedDetail,
			SignalDBM:         item.signal,
		})
	}
	sort.Slice(networks, func(i, j int) bool {
		if networks[i].SignalDBM == networks[j].SignalDBM {
			return networks[i].SSID < networks[j].SSID
		}
		return networks[i].SignalDBM > networks[j].SignalDBM
	})
	return networks, nil
}

func resolveSecurity(security, psk string) string {
	security = strings.TrimSpace(strings.ToLower(security))
	switch security {
	case SecurityOpen, SecuritySecured, SecurityWPAPSK, SecurityWPA2PSK, SecurityWPA3SAE:
		return security
	}
	if strings.TrimSpace(psk) == "" {
		return SecurityOpen
	}
	return SecurityWPA2PSK
}
