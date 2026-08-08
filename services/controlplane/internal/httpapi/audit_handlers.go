package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"appliance-code/services/controlplane/internal/auditops"
	"appliance-code/services/controlplane/internal/listcursor"
	"appliance-code/services/controlplane/internal/storage"
)

const auditListCursorScope = "audit.events"

// AuditHandlers implements GET /api/v1/audit/events and the export surface.
type AuditHandlers struct {
	Store     storage.AuditStore
	Ops       *auditops.Service
	CursorKey []byte
}

type auditEventResponse struct {
	ID           string          `json:"id"`
	Sequence     int64           `json:"sequence"`
	OccurredAt   time.Time       `json:"occurredAt"`
	ActorUserID  string          `json:"actorUserId,omitempty"`
	ActorType    string          `json:"actorType"`
	AuthMethod   string          `json:"authMethod,omitempty"`
	CredentialID string          `json:"credentialId,omitempty"`
	Action       string          `json:"action"`
	TargetType   string          `json:"targetType,omitempty"`
	TargetID     string          `json:"targetId,omitempty"`
	Outcome      string          `json:"outcome"`
	ReasonCode   string          `json:"reasonCode,omitempty"`
	RequestID    string          `json:"requestId,omitempty"`
	SourceAddr   string          `json:"sourceAddr,omitempty"`
	Severity     string          `json:"severity"`
	Details      json.RawMessage `json:"details,omitempty"`
}

func toAuditEventResponse(e storage.AuditEvent) auditEventResponse {
	resp := auditEventResponse{
		ID: e.ID, Sequence: e.Sequence, OccurredAt: e.OccurredAt, ActorUserID: e.ActorUserID,
		ActorType: string(e.ActorType), AuthMethod: e.AuthMethod, CredentialID: e.CredentialID,
		Action: e.Action, TargetType: e.TargetType, TargetID: e.TargetID, Outcome: string(e.Outcome),
		ReasonCode: e.ReasonCode, RequestID: e.RequestID, SourceAddr: e.SourceAddr, Severity: string(e.Severity),
	}
	if len(e.Details) > 0 {
		resp.Details = json.RawMessage(e.Details)
	}
	return resp
}

func (h *AuditHandlers) ListEvents(w http.ResponseWriter, r *http.Request) {
	filter := storage.AuditFilter{
		ActorUserID: r.URL.Query().Get("actorUserId"),
		Action:      r.URL.Query().Get("action"),
		Limit:       50,
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			WriteValidationProblem(w, r, "limit must be a positive integer", nil)
			return
		}
		filter.Limit = n
	}
	if raw := r.URL.Query().Get("since"); raw != "" {
		since, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			WriteValidationProblem(w, r, "since must be an RFC3339 timestamp", nil)
			return
		}
		filter.Since = since
	}
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		before, err := listcursor.Decode(h.CursorKey, auditListCursorScope, cursor, time.Now().UTC())
		if errors.Is(err, listcursor.ErrExpired) {
			WriteValidationProblem(w, r, "cursor has expired", nil)
			return
		}
		if err != nil {
			WriteValidationProblem(w, r, "cursor is invalid", nil)
			return
		}
		filter.BeforeSequence = before
	}

	events, err := h.Store.List(r.Context(), filter)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	items := make([]auditEventResponse, len(events))
	for i, e := range events {
		items[i] = toAuditEventResponse(e)
	}

	resp := map[string]any{"items": items}
	if len(events) == filter.Limit && filter.Limit > 0 {
		next, err := listcursor.Encode(h.CursorKey, auditListCursorScope, events[len(events)-1].Sequence, time.Now().UTC(), listcursor.DefaultTTL)
		if err == nil {
			resp["nextCursor"] = next
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *AuditHandlers) CreateExport(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required", "")
		return
	}
	op, err := h.Ops.StartExport(r.Context(), principal.UserID)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	w.Header().Set("Location", "/api/v1/audit/exports/"+op.ID)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":        op.ID,
		"kind":      string(op.Kind),
		"status":    string(op.Status),
		"createdAt": op.CreatedAt,
	})
}

func (h *AuditHandlers) GetExport(w http.ResponseWriter, r *http.Request) {
	op, err := h.Ops.GetExport(r.Context(), r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Audit export not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	body := map[string]any{
		"id":        op.ID,
		"kind":      string(op.Kind),
		"status":    string(op.Status),
		"createdAt": op.CreatedAt,
		"updatedAt": op.UpdatedAt,
	}
	if len(op.ResultBody) > 0 {
		var result any
		if json.Unmarshal(op.ResultBody, &result) == nil {
			body["result"] = result
		}
	}
	if len(op.ProblemBody) > 0 {
		var problem any
		if json.Unmarshal(op.ProblemBody, &problem) == nil {
			body["problem"] = problem
		}
	}
	writeJSON(w, http.StatusOK, body)
}

func (h *AuditHandlers) GetExportContent(w http.ResponseWriter, r *http.Request) {
	path, err := h.Ops.ExportContentPath(r.Context(), r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Audit export not found", "")
		return
	}
	if errors.Is(err, auditops.ErrNotReady) {
		WriteProblem(w, r, http.StatusConflict, "not_ready", "Audit export is not ready", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, path)
}
