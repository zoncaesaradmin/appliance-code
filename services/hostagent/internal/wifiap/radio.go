package wifiap

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var (
	ifaceLineRE = regexp.MustCompile(`(?m)^\s*Interface\s+(\S+)`)
	typeLineRE  = regexp.MustCompile(`(?m)^\s*type\s+(\S+)`)
	phyLineRE   = regexp.MustCompile(`(?m)^Wiphy\s+(\S+)`)
	apCapRE     = regexp.MustCompile(`(?m)^\s*\*\s+AP\b`)
)

type radioCandidate struct {
	Iface      string
	Phy        string
	Type       string
	APCapable  bool
	InUseAsSTA bool
}

// selectInterface finds a free AP-capable wireless interface.
// Returns reason codes when none is available.
func selectInterface(ctx context.Context, runner Runner) (iface string, reason string, err error) {
	if _, err := runner.LookPath("iw"); err != nil {
		return "", ReasonPackagesMissing, nil
	}
	devs, err := runner.CombinedOutput(ctx, "iw", "dev")
	if err != nil {
		// No wireless subsystem is treated as no capable hardware.
		return "", ReasonNoHardware, nil
	}
	listOut, listErr := runner.CombinedOutput(ctx, "iw", "list")
	apPhys := map[string]bool{}
	if listErr == nil {
		apPhys = physWithAP(listOut)
	}

	candidates := parseIWDev(devs)
	if len(candidates) == 0 {
		return "", ReasonNoHardware, nil
	}

	var free []radioCandidate
	var inUse []radioCandidate
	for _, c := range candidates {
		c.APCapable = true
		if len(apPhys) > 0 {
			// When phy mapping is unknown, leave capable=true if any AP phy exists.
			if c.Phy != "" {
				c.APCapable = apPhys[c.Phy]
			} else {
				c.APCapable = len(apPhys) > 0
			}
		}
		if !c.APCapable {
			continue
		}
		c.InUseAsSTA = isSTAInUse(ctx, runner, c)
		if c.InUseAsSTA {
			inUse = append(inUse, c)
			continue
		}
		free = append(free, c)
	}

	if len(free) == 0 {
		if len(inUse) > 0 {
			return "", ReasonRadioInUse, nil
		}
		return "", ReasonNoHardware, nil
	}
	return free[0].Iface, ReasonNone, nil
}

func parseIWDev(out string) []radioCandidate {
	blocks := strings.Split(out, "\n")
	var current *radioCandidate
	var all []radioCandidate
	flush := func() {
		if current != nil && current.Iface != "" {
			all = append(all, *current)
		}
		current = nil
	}
	for _, line := range blocks {
		if m := ifaceLineRE.FindStringSubmatch(line); len(m) == 2 {
			flush()
			current = &radioCandidate{Iface: m[1]}
			continue
		}
		if current == nil {
			continue
		}
		if m := typeLineRE.FindStringSubmatch(line); len(m) == 2 {
			current.Type = strings.ToLower(m[1])
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

func isSTAInUse(ctx context.Context, runner Runner, c radioCandidate) bool {
	if c.Type == "managed" {
		if out, err := runner.CombinedOutput(ctx, "iw", "dev", c.Iface, "link"); err == nil {
			low := strings.ToLower(out)
			if strings.Contains(low, "connected to") || strings.Contains(low, "ssid:") {
				return true
			}
		}
	}
	// AP/monitor etc. free for our purposes if not managed+associated.
	return false
}

// packagesPresent reports whether hostapd (and preferably dnsmasq) are on PATH.
func packagesPresent(runner Runner) bool {
	if _, err := runner.LookPath("hostapd"); err != nil {
		return false
	}
	if _, err := runner.LookPath("dnsmasq"); err != nil {
		return false
	}
	return true
}

func formatProbeError(reason string) string {
	switch reason {
	case ReasonNoHardware:
		return "no AP-capable wireless interface detected"
	case ReasonRadioInUse:
		return "wireless interface is in use as a station; AP not started"
	case ReasonPackagesMissing:
		return "required packages (hostapd/dnsmasq/iw) are not installed"
	default:
		return fmt.Sprintf("wifi ap not active: %s", reason)
	}
}
