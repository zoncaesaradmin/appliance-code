package host

import (
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Management Wi-Fi AP fixed address / subnet (must stay out of LAN node selection).
const (
	wifiAPManagementIPv4 = "10.42.0.1"
	wifiAPNetworkPrefix  = "10.42.0."
)

// NetworkLink is one host L3-capable interface the operator may care about.
type NetworkLink struct {
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`  // ethernet | wifi | bridge | virtual | other
	State         string   `json:"state"` // up | down
	IPv4Addresses []string `json:"ipv4Addresses,omitempty"`
	Role          string   `json:"role"` // lan | management-ap | virtual
}

// MediaStatus summarizes presence/enablement for ethernet or client Wi-Fi.
type MediaStatus struct {
	Present       bool     `json:"present"`
	Enabled       bool     `json:"enabled"`
	Interfaces    []string `json:"interfaces,omitempty"`
	IPv4Addresses []string `json:"ipv4Addresses,omitempty"`
}

// NetworkStatus is host-side connectivity inventory for Host Services.
type NetworkStatus struct {
	// PrimaryLANIPv4 is the preferred non-AP LAN address (ethernet over Wi-Fi).
	PrimaryLANIPv4 string `json:"primaryLANIPv4,omitempty"`
	// PrimaryLANSource is ethernet | wifi | other when PrimaryLANIPv4 is set.
	PrimaryLANSource string        `json:"primaryLANSource,omitempty"`
	Ethernet         MediaStatus   `json:"ethernet"`
	Wifi             MediaStatus   `json:"wifi"`
	WifiAP           MediaStatus   `json:"wifiAP"`
	Links            []NetworkLink `json:"links,omitempty"`
}

// IsWifiAPManagementIPv4 reports the fixed management Wi-Fi AP address.
func IsWifiAPManagementIPv4(ip string) bool {
	ip = strings.TrimSpace(ip)
	if ip == wifiAPManagementIPv4 {
		return true
	}
	// Fixed AP /24 (host and DHCP client range share 10.42.0.0/24).
	return strings.HasPrefix(ip, wifiAPNetworkPrefix)
}

// CollectNetwork inventories host interfaces. Prefer calling from host-agentd
// (root "/") so net.Interfaces reflects the host network namespace.
func CollectNetwork(root string) NetworkStatus {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "/"
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return NetworkStatus{}
	}

	var links []NetworkLink
	for _, iface := range ifaces {
		name := strings.TrimSpace(iface.Name)
		if name == "" || shouldSkipInterface(name) {
			continue
		}
		state := "down"
		if iface.Flags&net.FlagUp != 0 {
			state = "up"
		}
		kind := classifyInterfaceKind(root, name)
		addrs := interfaceIPv4s(iface)
		role := "lan"
		if kind == "virtual" || kind == "bridge" {
			role = "virtual"
		}
		// Mark management AP when the iface holds the fixed AP address or is AP-only wireless with that CIDR.
		for _, ip := range addrs {
			if IsWifiAPManagementIPv4(ip) {
				role = "management-ap"
				if kind == "wifi" {
					// leave kind as wifi; role distinguishes AP management plane
				}
				break
			}
		}
		// AP host AP iface may only have 10.42.0.1 — treat as wifi + management-ap.
		if role == "management-ap" && kind == "other" {
			kind = "wifi"
		}
		links = append(links, NetworkLink{
			Name:          name,
			Kind:          kind,
			State:         state,
			IPv4Addresses: addrs,
			Role:          role,
		})
	}
	sort.Slice(links, func(i, j int) bool {
		return links[i].Name < links[j].Name
	})
	return summarizeNetwork(links)
}

func summarizeNetwork(links []NetworkLink) NetworkStatus {
	out := NetworkStatus{Links: links}
	for _, link := range links {
		switch {
		case link.Role == "management-ap":
			out.WifiAP.Present = true
			if link.State == "up" {
				out.WifiAP.Enabled = true
			}
			out.WifiAP.Interfaces = appendUnique(out.WifiAP.Interfaces, link.Name)
			for _, ip := range link.IPv4Addresses {
				if IsWifiAPManagementIPv4(ip) {
					out.WifiAP.IPv4Addresses = appendUnique(out.WifiAP.IPv4Addresses, ip)
				}
			}
		case link.Kind == "ethernet" && link.Role == "lan":
			out.Ethernet.Present = true
			if link.State == "up" {
				out.Ethernet.Enabled = true
			}
			out.Ethernet.Interfaces = appendUnique(out.Ethernet.Interfaces, link.Name)
			for _, ip := range link.IPv4Addresses {
				if !IsWifiAPManagementIPv4(ip) {
					out.Ethernet.IPv4Addresses = appendUnique(out.Ethernet.IPv4Addresses, ip)
				}
			}
		case link.Kind == "wifi" && link.Role == "lan":
			out.Wifi.Present = true
			if link.State == "up" {
				out.Wifi.Enabled = true
			}
			out.Wifi.Interfaces = appendUnique(out.Wifi.Interfaces, link.Name)
			for _, ip := range link.IPv4Addresses {
				if !IsWifiAPManagementIPv4(ip) {
					out.Wifi.IPv4Addresses = appendUnique(out.Wifi.IPv4Addresses, ip)
				}
			}
		case link.Kind == "wifi":
			// wifi kind with non-lan role already handled for management-ap above
			out.Wifi.Present = true
			if link.State == "up" && link.Role != "management-ap" {
				out.Wifi.Enabled = true
			}
			out.Wifi.Interfaces = appendUnique(out.Wifi.Interfaces, link.Name)
		}
	}

	// Prefer ethernet LAN address, then client Wi-Fi LAN, then any other non-AP IPv4.
	for _, ip := range out.Ethernet.IPv4Addresses {
		out.PrimaryLANIPv4 = ip
		out.PrimaryLANSource = "ethernet"
		return out
	}
	for _, ip := range out.Wifi.IPv4Addresses {
		out.PrimaryLANIPv4 = ip
		out.PrimaryLANSource = "wifi"
		return out
	}
	for _, link := range links {
		if link.Role != "lan" {
			continue
		}
		for _, ip := range link.IPv4Addresses {
			if IsWifiAPManagementIPv4(ip) {
				continue
			}
			out.PrimaryLANIPv4 = ip
			out.PrimaryLANSource = link.Kind
			if out.PrimaryLANSource == "" {
				out.PrimaryLANSource = "other"
			}
			return out
		}
	}
	return out
}

func interfaceIPv4s(iface net.Interface) []string {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}
	var out []string
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			out = appendUnique(out, v4.String())
		}
	}
	return out
}

func classifyInterfaceKind(root, name string) string {
	// Wireless: sysfs wireless or phy80211.
	if pathExists(filepath.Join(sysClassNet(root), name, "wireless")) ||
		pathExists(filepath.Join(sysClassNet(root), name, "phy80211")) {
		return "wifi"
	}
	// Kernel type: 1 = ARPHRD_ETHER.
	typePath := filepath.Join(sysClassNet(root), name, "type")
	if data, err := os.ReadFile(typePath); err == nil {
		switch strings.TrimSpace(string(data)) {
		case "1":
			if isVirtualEthernetName(name) {
				return "virtual"
			}
			return "ethernet"
		case "24", "772": // ARPHRD_IEEE80211 etc. sometimes used
			return "wifi"
		case "776", "778": // bridge variants when type reclassified
			return "bridge"
		}
	}
	if strings.HasPrefix(name, "br") || strings.HasPrefix(name, "bridge") {
		return "bridge"
	}
	if isVirtualEthernetName(name) {
		return "virtual"
	}
	return "other"
}

func sysClassNet(root string) string {
	if root == "/" {
		return "/sys/class/net"
	}
	return filepath.Join(root, "sys/class/net")
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func shouldSkipInterface(name string) bool {
	if name == "lo" {
		return true
	}
	prefixes := []string{
		"docker", "cni", "flannel", "cbr", "veth", "cali", "kube",
		"nodelocaldns", "tunl", "ip6tnl", "sit", "gre", "gretap",
		"erspan", "vti", "wg", "tailscale", "virbr",
	}
	lower := strings.ToLower(name)
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

func isVirtualEthernetName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "veth") ||
		strings.HasPrefix(lower, "docker") ||
		strings.HasPrefix(lower, "cni") ||
		strings.Contains(lower, "vlan")
}

func appendUnique(list []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return list
	}
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}
