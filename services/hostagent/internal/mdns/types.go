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
	Message          string `json:"message,omitempty"`
}

// ApplyRequest is the shared install/API apply body.
type ApplyRequest struct {
	Desired bool `json:"desired"`
}

// Controller is the apply/status surface used by the HTTP API.
type Controller interface {
	Status(ctx context.Context) (Status, error)
	Apply(ctx context.Context, req ApplyRequest) (Status, error)
}

type persistedState struct {
	Desired bool `json:"desired"`
}
