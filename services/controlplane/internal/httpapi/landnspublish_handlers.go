package httpapi

import (
	"net/http"
	"strings"

	"appliance-code/services/controlplane/internal/landnspublish"
)

// LANDNSPublishHandlers implements the base-capability outbound LAN DNS
// publish surface. Any appliance can call this; it proxies to a remote
// lan-dns appliance's PUT /api/v1/dns/records/{name}.
type LANDNSPublishHandlers struct {
	Client *landnspublish.Client
}

type lanDNSPublishRequest struct {
	DNSApplianceURL string `json:"dnsApplianceURL"`
	APIToken        string `json:"apiToken"`
	Name            string `json:"name"`
	IPv4            string `json:"ipv4"`
	TTL             int    `json:"ttl"`
	Owner           string `json:"owner"`
}

func (h *LANDNSPublishHandlers) Publish(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required", "")
		return
	}
	var req lanDNSPublishRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteValidationProblem(w, r, "invalid request body", nil)
		return
	}
	client := h.Client
	if client == nil {
		client = &landnspublish.Client{}
	}
	owner := strings.TrimSpace(req.Owner)
	if owner == "" {
		owner = principal.UserID
	}
	err := client.Publish(r.Context(), landnspublish.Request{
		DNSApplianceURL: req.DNSApplianceURL,
		APIToken:        req.APIToken,
		Name:            req.Name,
		IPv4:            req.IPv4,
		TTL:             req.TTL,
		Owner:           owner,
	})
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "required"),
			strings.Contains(msg, "must be"),
			strings.Contains(msg, "valid IPv4"):
			WriteValidationProblem(w, r, msg, nil)
		default:
			WriteProblem(w, r, http.StatusBadGateway, "upstream_error", msg, "")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": strings.ToLower(strings.TrimSpace(req.Name)),
		"ipv4": strings.TrimSpace(req.IPv4),
	})
}
