// Package httpapi contains REST routing, request parsing, response
// formatting, and middleware. It does not contain business logic.
package httpapi

import (
	"encoding/json"
	"net/http"
)

// Problem is the RFC 9457 application/problem+json error envelope accepted
// as the plan's standard API error shape.
type Problem struct {
	Type      string       `json:"type"`
	Title     string       `json:"title"`
	Status    int          `json:"status"`
	Code      string       `json:"code"`
	Detail    string       `json:"detail,omitempty"`
	Instance  string       `json:"instance,omitempty"`
	RequestID string       `json:"requestId,omitempty"`
	Errors    []FieldError `json:"errors,omitempty"`
}

// FieldError describes one field-level validation failure.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

const problemBase = "https://appliance.local/problems/"

// WriteProblem writes p as an application/problem+json response with the
// given HTTP status, filling in the request ID from ctx and disabling
// caching, since problem responses may echo request details.
func WriteProblem(w http.ResponseWriter, r *http.Request, status int, code, title, detail string) {
	p := Problem{
		Type:      problemBase + code,
		Title:     title,
		Status:    status,
		Code:      code,
		Detail:    detail,
		Instance:  r.URL.Path,
		RequestID: requestIDFromRequest(r),
	}
	writeProblem(w, status, p)
}

// WriteValidationProblem writes a 400 validation-error problem with
// field-level detail.
func WriteValidationProblem(w http.ResponseWriter, r *http.Request, detail string, errs []FieldError) {
	p := Problem{
		Type:      problemBase + "validation-error",
		Title:     "Validation failed",
		Status:    http.StatusBadRequest,
		Code:      "validation_error",
		Detail:    detail,
		Instance:  r.URL.Path,
		RequestID: requestIDFromRequest(r),
		Errors:    errs,
	}
	writeProblem(w, http.StatusBadRequest, p)
}

func writeProblem(w http.ResponseWriter, status int, p Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}
