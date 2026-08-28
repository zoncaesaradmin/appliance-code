// Package firewall manages the appliance-owned host firewall projection for
// catalog-approved direct application endpoints.
package firewall

import "context"

const (
	ActualActive   = "active"
	ActualInactive = "inactive"
	ActualFailed   = "failed"

	ReasonNone            = ""
	ReasonInvalidPolicy   = "invalid_policy"
	ReasonNftUnavailable  = "nft_unavailable"
	ReasonApplyFailed     = "apply_failed"
	DefaultStateDir       = "/var/lib/zon/application-firewall"
	DefaultManagementCIDR = "10.42.0.0/24"
)

type Endpoint struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
}

type ApplicationPolicy struct {
	Application string     `json:"application"`
	Endpoints   []Endpoint `json:"endpoints"`
}

type Status struct {
	Application string `json:"application"`
	Actual      string `json:"actual"`
	Reason      string `json:"reason,omitempty"`
	Message     string `json:"message,omitempty"`
}

// Controller is deliberately smaller than a generic host command API.
type Controller interface {
	Apply(ctx context.Context, policy ApplicationPolicy) (Status, error)
	Status(ctx context.Context, application string) (Status, error)
}
