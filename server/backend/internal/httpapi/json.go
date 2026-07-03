package httpapi

import (
	"encoding/json"
	"net/http"
)

// decodeJSON decodes r's body into v, rejecting unknown fields and bodies
// larger than 1 MiB per the plan's default JSON body limit.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// writeJSON writes v as a JSON response with status, disabling caching
// since every response on this API may carry principal-scoped data.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
