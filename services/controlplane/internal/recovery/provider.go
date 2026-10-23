// Package recovery defines the control-plane boundary for appliance recovery.
// Local appliances implement it through the authenticated host agent; cloud
// deployments can provide the same contract with managed database and object
// storage primitives.
package recovery

import (
	"context"
	"time"
)

// Provider never accepts a shell command.  Each operation is named and its
// input is validated by the implementation before it can affect appliance
// state.
type Provider interface {
	Create(context.Context, CreateRequest) (Operation, error)
	Restore(context.Context, RestoreRequest) (Operation, error)
	Get(context.Context, string) (Operation, error)
}

// CreateRequest contains an already sealed portable configuration archive.
// Its encryption key intentionally remains with the operator/browser.
type CreateRequest struct {
	ConfigurationArchive []byte
	RequestedBy          string
}

// RestoreRequest refers to a staged, verified recovery set.  Confirmation is
// explicit because restore replaces configuration and declared persistent
// data; it does not include the signed appliance software bundle.
type RestoreRequest struct {
	RecoverySetID string
	Confirm       bool
	RequestedBy   string
}

type Operation struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
