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
	ifaceLineRE = regexp.MustCompile(`(?m)^\s*Interface\s+(\S+)`)
	typeLineRE  = regexp.MustCompile(`(?m)^\s*type\s+(\S+)`)
	phyLineRE   = regexp.MustCompile(`(?m)^Wiphy\s+(\S+)`)
	apCapRE     = regexp.MustCompile(`(?m)^\s*\*\s+AP\b`)
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
}

func inspectRadios(ctx context.Context, runner wifiap.Runner) (radioInventory, error) {
	if _, err := runner.LookPath("iw"); err != nil {
		return radioInventory{}, nil
	}
	devs, err := runner.CombinedOutput(ctx, "iw", "dev")
	if err != nil {
		return radioInventory{}, nil
	}
	inv := radioInventory{candidates: parseIWDev(devs)}
	if len(inv.candidates) == 0 {
		return inv, nil
	}
	listOut, _ := runner.CombinedOutput(ctx, "iw", "list")
	apPhys := physWithAP(listOut)
	for i := range inv.candidates {
		if len(apPhys) == 0 {
			inv.candidates[i].APCapable = true
		} else if inv.candidates[i].Phy != "" {
			inv.candidates[i].APCapable = apPhys[inv.candidates[i].Phy]
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

func (inv radioInventory) unavailableReason() string {
	if len(inv.apActive) > 0 {
		return ReasonRadioInUseByAP
	}
	return ReasonNoHardware
}

func (inv radioInventory) concurrentAPSupport() (bool, string) {
	if len(inv.managed) == 0 {
		return false, "No client-capable Wi-Fi interface is available."
	}
	apCapableIfaces := map[string]bool{}
	allIfaces := map[string]bool{}
	for _, candidate := range inv.candidates {
		allIfaces[candidate.Iface] = true
		if candidate.APCapable {
			apCapableIfaces[candidate.Iface] = true
		}
	}
	if len(apCapableIfaces) == 0 {
		return false, "No AP-capable Wi-Fi interface is available."
	}
	if len(allIfaces) < 2 {
		return false, "Client Wi-Fi and Wi-Fi AP need separate wireless interfaces on this appliance."
	}
	return true, "Separate wireless interfaces are available for client Wi-Fi and Wi-Fi AP at the same time."
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
	var current string
	for _, line := range strings.Split(iwList, "\n") {
		if m := phyLineRE.FindStringSubmatch(line); len(m) == 2 {
			current = m[1]
			continue
		}
		if current == "" {
			continue
		}
		if apCapRE.MatchString(line) {
			result[current] = true
		}
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
		ssid     string
		security string
		requires bool
		signal   int
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
		case strings.HasPrefix(trimmed, "RSN:"):
			current.security = SecurityWPA2PSK
			current.requires = true
		case strings.HasPrefix(trimmed, "WPA:"):
			if current.security == "" {
				current.security = SecurityWPAPSK
			}
			current.requires = true
		case strings.Contains(trimmed, "SAE"):
			current.security = SecurityWPA3SAE
			current.requires = true
		case strings.HasPrefix(trimmed, "capability:") && strings.Contains(trimmed, "Privacy"):
			if current.security == "" {
				current.security = SecuritySecured
			}
			current.requires = true
		}
	}
	commit()
	var networks []ScanNetwork
	for _, item := range best {
		security := item.security
		if security == "" {
			security = SecurityOpen
		}
		networks = append(networks, ScanNetwork{
			SSID:             item.ssid,
			Security:         security,
			RequiresPassword: item.requires,
			SignalDBM:        item.signal,
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
