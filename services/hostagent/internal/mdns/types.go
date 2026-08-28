// Package mdns implements host-side mDNS (avahi-daemon) apply and status
// for the appliance host agent daemon.
package mdns

import "context"

const (
	ServiceName = "avahi-daemon.service"

	ActualInactive = "inactive"
	ActualActive   = "active"
	ActualFailed   = "failed"

	ReasonNone               = ""
	ReasonPackagesMissing    = "packages_missing"
	ReasonDesiredOff         = "desired_off"
	ReasonNotConfigured      = "not_configured"
	ReasonServiceStartFailed = "service_start_failed"

	DefaultStateDir = "/var/lib/zon/mdns"
)

// Status is the queryable desired/actual state of host mDNS.
type Status struct {
	Desired          bool   `json:"desired"`
	Actual           string `json:"actual"`
	Reason           string `json:"reason,omitempty"`
	Service          string `json:"service"`
	SupportedCapable bool   `json:"supportedCapable"`
	AdvertisedName   string `json:"advertisedName,omitempty"`
	Message          string `json:"message,omitempty"`
}

// ApplyRequest is the shared install/API apply body.
type ApplyRequest struct {
	Desired bool `json:"desired"`
}

// ApplicationService is a catalog-approved mDNS advertisement. It contains
// only the small Avahi service shape an application needs, never raw XML.
type ApplicationService struct {
	Name        string `json:"name"`
	ServiceType string `json:"serviceType"`
	Port        int    `json:"port"`
}

type ApplicationRequest struct {
	Application string               `json:"application"`
	Services    []ApplicationService `json:"services"`
	// Aliases are reviewed application host names (for example
	// jellyfin.local). They are advertised by the host agent, never by the
	// application pod itself.
	Aliases []string `json:"aliases,omitempty"`
}

// Controller is the apply/status surface used by the HTTP API.
type Controller interface {
	Status(ctx context.Context) (Status, error)
	Apply(ctx context.Context, req ApplyRequest) (Status, error)
}

type persistedState struct {
	Desired             bool                            `json:"desired"`
	ApplicationServices map[string][]ApplicationService `json:"applicationServices,omitempty"`
	ApplicationAliases  map[string][]string             `json:"applicationAliases,omitempty"`
}
