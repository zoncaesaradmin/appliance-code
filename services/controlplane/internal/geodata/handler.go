// Package geodata exposes the Geo editor's narrowly scoped persistence API.
// The caller must authenticate with a target-generated internal token; browser
// authentication and role checks remain the control plane's responsibility.
package geodata

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const InternalAuthHeader = "X-Appliance-Internal-Auth"

type SaveRequest struct {
	Name        string          `json:"p_name"`
	Description string          `json:"p_description"`
	Geometry    json.RawMessage `json:"p_geometry"`
	ID          *string         `json:"p_id"`
	Revision    *int            `json:"p_revision"`
}

type Store interface {
	List(context.Context) (json.RawMessage, error)
	Save(context.Context, SaveRequest) (string, error)
	Ready(context.Context) error
}

func NewHandler(store Store, internalToken string) (http.Handler, error) {
	if store == nil || strings.TrimSpace(internalToken) == "" {
		return nil, errors.New("geo data store and internal token are required")
	}
	expected := sha256.Sum256([]byte(internalToken))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := store.Ready(r.Context()); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	protect := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			actual := sha256.Sum256([]byte(r.Header.Get(InternalAuthHeader)))
			if subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /api/v1/geo/zones", protect(func(w http.ResponseWriter, r *http.Request) {
		zones, err := store.List(r.Context())
		if err != nil {
			http.Error(w, "zone data unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(zones)
	}))
	mux.HandleFunc("POST /api/v1/geo/editor/rpc/save_zone", protect(func(w http.ResponseWriter, r *http.Request) {
		var request SaveRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "invalid zone request", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			http.Error(w, "invalid zone request", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(request.Name) == "" || len(request.Name) > 120 || len(request.Geometry) == 0 || (request.ID == nil) != (request.Revision == nil) {
			http.Error(w, "invalid zone request", http.StatusBadRequest)
			return
		}
		id, err := store.Save(r.Context(), request)
		if err != nil {
			http.Error(w, "could not save zone", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(id)
	}))
	return mux, nil
}
