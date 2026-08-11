package wificlient

import "context"

const (
	ActualInactive = "inactive"
	ActualActive   = "active"
	ActualFailed   = "failed"

	ReasonNone             = ""
	ReasonNoHardware       = "no_capable_hardware"
	ReasonRadioInUseByAP   = "radio_in_use_by_ap"
	ReasonSSIDMissing      = "ssid_missing"
	ReasonPackagesMissing  = "packages_missing"
	ReasonConnectionFailed = "connection_failed"
	ReasonDHCPFailed       = "dhcp_failed"
	ReasonDesiredOff       = "desired_off"
	ReasonNotConfigured    = "not_configured"
	ReasonScanFailed       = "scan_failed"

	SecurityUnknown = "unknown"
	SecurityOpen    = "open"
	SecuritySecured = "secured"
	SecurityWPAPSK  = "wpa-psk"
	SecurityWPA2PSK = "wpa2-psk"
	SecurityWPA3SAE = "wpa3-sae"

	DefaultConfigDir  = "/etc/zon/wifi-client"
	DefaultStateDir   = "/var/lib/zon/wifi-client"
	DefaultRuntimeDir = "/run/zon/wifi-client"
)

type Status struct {
	Desired              bool     `json:"desired"`
	Actual               string   `json:"actual"`
	Reason               string   `json:"reason,omitempty"`
	SSID                 string   `json:"ssid,omitempty"`
	Iface                string   `json:"iface,omitempty"`
	IPv4Addresses        []string `json:"ipv4Addresses,omitempty"`
	Security             string   `json:"security"`
	SupportedCapable     bool     `json:"supportedCapable"`
	SupportsConcurrentAP bool     `json:"supportsConcurrentAP"`
	ConcurrentAPDetail   string   `json:"concurrentAPDetail,omitempty"`
	Message              string   `json:"message,omitempty"`
}

type ApplyRequest struct {
	Desired  bool   `json:"desired"`
	SSID     string `json:"ssid,omitempty"`
	PSK      string `json:"psk,omitempty"`
	Security string `json:"security,omitempty"`
}

type ScanNetwork struct {
	SSID             string `json:"ssid"`
	Security         string `json:"security"`
	RequiresPassword bool   `json:"requiresPassword"`
	SignalDBM        int    `json:"signalDBM"`
}

type ScanResult struct {
	Iface                string        `json:"iface,omitempty"`
	SupportedCapable     bool          `json:"supportedCapable"`
	SupportsConcurrentAP bool          `json:"supportsConcurrentAP"`
	ConcurrentAPDetail   string        `json:"concurrentAPDetail,omitempty"`
	Reason               string        `json:"reason,omitempty"`
	Message              string        `json:"message,omitempty"`
	Networks             []ScanNetwork `json:"networks,omitempty"`
}

type Controller interface {
	Status(ctx context.Context) (Status, error)
	Apply(ctx context.Context, req ApplyRequest) (Status, error)
	Scan(ctx context.Context) (ScanResult, error)
}

type persistedState struct {
	Desired  bool   `json:"desired"`
	SSID     string `json:"ssid,omitempty"`
	Iface    string `json:"iface,omitempty"`
	Security string `json:"security,omitempty"`
}
