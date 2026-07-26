package httpapi

import (
	"net/http"
	"net/url"
	"strings"
)

// IdentityHandlers reports the product LAN identity for this appliance
// instance (name, zone, FQDN, node IP). Operators use this to register the
// A record on the landns appliance; install does not publish DNS itself.
type IdentityHandlers struct {
	ApplianceName   string
	DNSZone         string
	NodeIPv4        string
	CanonicalOrigin string
}

type identityResponse struct {
	ApplianceName   string `json:"applianceName"`
	DNSZone         string `json:"dnsZone"`
	FQDN            string `json:"fqdn"`
	NodeIPv4        string `json:"nodeIPv4,omitempty"`
	CanonicalOrigin string `json:"canonicalOrigin,omitempty"`
}

func (h *IdentityHandlers) Get(w http.ResponseWriter, r *http.Request) {
	name := strings.ToLower(strings.TrimSpace(h.ApplianceName))
	zone := strings.ToLower(strings.TrimSpace(h.DNSZone))
	fqdn := ""
	if name != "" && zone != "" {
		fqdn = name + "." + zone
	}
	origin := strings.TrimSpace(h.CanonicalOrigin)
	if origin == "" && fqdn != "" {
		origin = "https://" + fqdn
	}
	if u, err := url.Parse(origin); err == nil && u.Host != "" && fqdn == "" {
		fqdn = u.Hostname()
	}
	writeJSON(w, http.StatusOK, identityResponse{
		ApplianceName:   name,
		DNSZone:         zone,
		FQDN:            fqdn,
		NodeIPv4:        strings.TrimSpace(h.NodeIPv4),
		CanonicalOrigin: origin,
	})
}
