// Package wifiap implements host-side management WiFi access-point apply
// and status for the appliance host agent daemon.
//
// The AP fixed IPv4 is ManagementAddress (https://10.42.0.1/). The fixed
// management hostname is ManagementHostname (https://manage.ap/). Both are
// always TLS SANs. The address must be published on the Traefik Service as an
// externalIP so AP clients reach HTTPS on :443 — ServiceLB alone only covers
// the node LAN address (see appliance-ctl helm.EnsureTraefikManagementExternalIPs).
//
// Name resolution for manage.ap:
//   - appliance-dns (CoreDNS) always has A manage.ap → 10.42.0.1 when the chart
//     is installed (AP-unaware).
//   - On AP enable, host-agentd probes whether :53 can be bound on the management
//     address; if free, dnsmasq serves DNS for manage.ap (majority no-CoreDNS
//     installs). If busy (CoreDNS wildcard :53), dnsmasq stays DHCP-only.
package wifiap

import "context"

const (
	// ManagementAddress is the fixed IPv4 address on the AP interface.
	// It is always a TLS SAN so browsers can open https://10.42.0.1/.
	// This /24 must stay outside K3s pod CIDR (appliance uses 10.44.0.0/16).
	ManagementAddress = "10.42.0.1"
	// ManagementHostname is the fixed management DNS name for the AP URL
	// (https://manage.ap/). Always a TLS SAN; resolved by CoreDNS and/or
	// AP-local dnsmasq depending on who owns host :53.
	ManagementHostname = "manage.ap"
	// ManagementCIDR is the AP management subnet.
	ManagementCIDR = "10.42.0.1/24"
	// DHCPRangeStart / DHCPRangeEnd bound client addresses on the AP.
	DHCPRangeStart = "10.42.0.10"
	DHCPRangeEnd   = "10.42.0.50"

	ActualInactive = "inactive"
	ActualActive   = "active"
	ActualFailed   = "failed"

	ReasonNone               = ""
	ReasonNoHardware         = "no_capable_hardware"
	ReasonRadioInUse         = "radio_in_use"
	ReasonPSKMissing         = "psk_missing"
	ReasonPackagesMissing    = "packages_missing"
	ReasonHostapdFailed      = "hostapd_failed"
	ReasonConfigFailed       = "config_failed"
	ReasonDesiredOff         = "desired_off"
	ReasonNotConfigured      = "not_configured"
	ReasonInterfacePrepare   = "interface_prepare_failed"
	ReasonServiceStartFailed = "service_start_failed"

	SecurityWPA2PSK = "wpa2-psk"

	DefaultConfigDir  = "/etc/zon/wifi-ap"
	DefaultStateDir   = "/var/lib/zon/wifi-ap"
	DefaultRuntimeDir = "/run/zon/wifi-ap"
)

// Status is the queryable desired/actual state of the management AP.
// PSK is never included.
type Status struct {
	Desired            bool   `json:"desired"`
	Actual             string `json:"actual"`
	Reason             string `json:"reason,omitempty"`
	SSID               string `json:"ssid,omitempty"`
	Iface              string `json:"iface,omitempty"`
	ManagementAddress  string `json:"managementAddress"`
	ManagementHostname string `json:"managementHostname"`
	ManagementURL      string `json:"managementURL"`
	// LocalDNSServing is set when the AP is desired: true when this host's
	// dnsmasq serves manage.ap; false when host :53 was already owned and
	// resolution relies on existing host DNS (e.g. CoreDNS).
	LocalDNSServing  *bool  `json:"localDNSServing,omitempty"`
	Security         string `json:"security"`
	SupportedCapable bool   `json:"supportedCapable"`
	CapabilityState  string `json:"capabilityState"`
	CapabilityDetail string `json:"capabilityDetail,omitempty"`
	Message          string `json:"message,omitempty"`
}

// ApplyRequest is the shared install/API apply body.
type ApplyRequest struct {
	Desired  bool   `json:"desired"`
	PSK      string `json:"psk,omitempty"`
	SSIDBase string `json:"ssidBase,omitempty"`
}

// Controller is the apply/status surface used by the HTTP API.
type Controller interface {
	Status(ctx context.Context) (Status, error)
	Apply(ctx context.Context, req ApplyRequest) (Status, error)
}

// PortBindProber decides whether the host can reserve management DNS (:53 on
// ManagementAddress). Production uses a real bind attempt; tests inject.
type PortBindProber interface {
	CanBindManagementDNS() bool
}

// persistedState is host-local durable state (PSK stored separately).
type persistedState struct {
	Desired         bool   `json:"desired"`
	SSIDBase        string `json:"ssidBase,omitempty"`
	SSID            string `json:"ssid,omitempty"`
	Iface           string `json:"iface,omitempty"`
	LocalDNSServing bool   `json:"localDNSServing,omitempty"`
}
