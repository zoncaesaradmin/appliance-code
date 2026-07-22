package httpapi

import (
	"net/http"

	"appliance-code/services/controlplane/internal/appliance"
)

// CapabilitiesHandlers reports the appliance capability set the running
// instance resolved at startup, so callers (the UI, in particular) can
// gate what they show on the actual enabled capability set instead of
// duplicating the profile-to-capability mapping themselves.
type CapabilitiesHandlers struct {
	Capabilities appliance.Set
}

type capabilitiesResponse struct {
	Capabilities []appliance.Capability `json:"capabilities"`
}

func (h *CapabilitiesHandlers) Get(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, capabilitiesResponse{Capabilities: h.Capabilities.Names()})
}
