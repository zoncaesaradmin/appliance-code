package httpapi

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"strings"
)

const applianceCADownloadName = "appliance-ca.pem"

// DownloadCA serves the installer-managed appliance CA certificate (public
// material only) so authenticated clients can trust HTTPS to this appliance.
// The private CA key is never mounted or returned.
func (h *IdentityHandlers) DownloadCA(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(h.CACertPath)
	if path == "" {
		WriteProblem(w, r, http.StatusServiceUnavailable, "tls_ca_unavailable",
			"Appliance CA certificate is not configured",
			"The control plane has no TLS CA certificate path configured.")
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		WriteProblem(w, r, http.StatusServiceUnavailable, "tls_ca_unavailable",
			"Appliance CA certificate is unavailable",
			"The control plane could not read the mounted appliance CA certificate.")
		return
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		WriteProblem(w, r, http.StatusInternalServerError, "tls_ca_invalid",
			"Appliance CA certificate is invalid",
			"The mounted CA file is not a PEM certificate.")
		return
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "tls_ca_invalid",
			"Appliance CA certificate is invalid",
			"The mounted CA file could not be parsed as an X.509 certificate.")
		return
	}
	pemBytes := pem.EncodeToMemory(block)
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", applianceCADownloadName))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(pemBytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pemBytes)
}
