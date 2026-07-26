package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"appliance-code/services/controlplane/internal/authz"
	"appliance-code/services/controlplane/internal/dnsrecords"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/storage"
)

// DNSHandlers implements the LAN DNS records HTTP surface.
type DNSHandlers struct {
	DNS *dnsrecords.Service
}

type dnsRecordResponse struct {
	Name           string     `json:"name"`
	FQDN           string     `json:"fqdn"`
	IPv4           string     `json:"ipv4"`
	TTL            int        `json:"ttl"`
	Source         string     `json:"source"`
	Owner          string     `json:"owner,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	LeaseExpiresAt *time.Time `json:"leaseExpiresAt,omitempty"`
}

func (h *DNSHandlers) toResponse(rec storage.DNSRecord) dnsRecordResponse {
	zone := dnsrecords.DefaultZone
	if h.DNS != nil {
		zone = h.DNS.Zone()
	}
	return dnsRecordResponse{
		Name:           rec.Name,
		FQDN:           rec.Name + "." + zone,
		IPv4:           rec.IPv4,
		TTL:            rec.TTL,
		Source:         string(rec.Source),
		Owner:          rec.Owner,
		CreatedAt:      rec.CreatedAt,
		UpdatedAt:      rec.UpdatedAt,
		LeaseExpiresAt: rec.LeaseExpiresAt,
	}
}

func (h *DNSHandlers) List(w http.ResponseWriter, r *http.Request) {
	recs, err := h.DNS.List(r.Context())
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	items := make([]dnsRecordResponse, len(recs))
	for i, rec := range recs {
		items[i] = h.toResponse(rec)
	}
	writeJSON(w, http.StatusOK, struct {
		Zone  string              `json:"zone"`
		Items []dnsRecordResponse `json:"items"`
	}{Zone: h.DNS.Zone(), Items: items})
}

type upsertDNSRecordRequest struct {
	IPv4  string `json:"ipv4"`
	TTL   int    `json:"ttl"`
	Owner string `json:"owner"`
}

func (h *DNSHandlers) Upsert(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required", "")
		return
	}
	var req upsertDNSRecordRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteValidationProblem(w, r, "invalid request body", nil)
		return
	}
	name := r.PathValue("name")
	asAdmin := authz.HasPermission(principal.Permissions, roles.PermDNSRecordsWrite)
	asRegister := authz.HasPermission(principal.Permissions, roles.PermDNSRecordsRegister)
	if !asAdmin && !asRegister {
		WriteProblem(w, r, http.StatusForbidden, "forbidden", "Missing dns.records.write or dns.records.register", "")
		return
	}
	owner := strings.TrimSpace(req.Owner)
	if !asAdmin {
		if owner == "" {
			owner = principal.UserID
		}
		if owner != principal.UserID {
			WriteValidationProblem(w, r, "owner must match the authenticated principal for peer registration", nil)
			return
		}
	}
	rec, err := h.DNS.Upsert(r.Context(), dnsrecords.UpsertInput{
		Name:    name,
		IPv4:    req.IPv4,
		TTL:     req.TTL,
		Owner:   owner,
		AsAdmin: asAdmin,
		Actor:   principal.Actor(requestIDFromRequest(r), r.RemoteAddr),
	})
	if err != nil {
		switch {
		case errors.Is(err, dnsrecords.ErrInvalidName), errors.Is(err, dnsrecords.ErrInvalidIPv4):
			WriteValidationProblem(w, r, err.Error(), nil)
		case errors.Is(err, dnsrecords.ErrConflict):
			WriteProblem(w, r, http.StatusConflict, "conflict", err.Error(), "")
		default:
			WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		}
		return
	}
	writeJSON(w, http.StatusOK, h.toResponse(rec))
}

func (h *DNSHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required", "")
		return
	}
	err := h.DNS.Delete(r.Context(), r.PathValue("name"), principal.Actor(requestIDFromRequest(r), r.RemoteAddr))
	if err != nil {
		switch {
		case errors.Is(err, dnsrecords.ErrInvalidName):
			WriteValidationProblem(w, r, err.Error(), nil)
		case errors.Is(err, storage.ErrNotFound):
			WriteProblem(w, r, http.StatusNotFound, "not_found", "DNS record not found", "")
		default:
			WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
