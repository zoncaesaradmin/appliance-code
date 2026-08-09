package httpapi

import (
	"errors"
	"net/http"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/licensing"
	"appliance-code/services/controlplane/internal/metadatabundle"
	"appliance-code/services/controlplane/internal/notifications"
	"appliance-code/services/controlplane/internal/profiles"
	"appliance-code/services/controlplane/internal/storage"
)

type LicensingHandlers struct {
	Licensing *licensing.Service
}

func (h *LicensingHandlers) actor(r *http.Request) audit.Actor {
	principal, _ := PrincipalFromContext(r.Context())
	return audit.Actor{
		UserID:     principal.UserID,
		Type:       storage.AuditActorUser,
		AuthMethod: principal.AuthMethod,
	}
}

func (h *LicensingHandlers) Status(w http.ResponseWriter, r *http.Request) {
	st, err := h.Licensing.Status(r.Context())
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *LicensingHandlers) Entitlements(w http.ResponseWriter, r *http.Request) {
	caps, err := h.Licensing.Entitlements(r.Context())
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"capabilities": caps})
}

func (h *LicensingHandlers) AcceptBase(w http.ResponseWriter, r *http.Request) {
	st, err := h.Licensing.AcceptBaseEntitlement(r.Context(), h.actor(r))
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

type importLicenseRequest struct {
	Document string `json:"document"`
}

func (h *LicensingHandlers) ImportLicense(w http.ResponseWriter, r *http.Request) {
	var req importLicenseRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteValidationProblem(w, r, "invalid request body", nil)
		return
	}
	st, err := h.Licensing.ImportLicense(r.Context(), h.actor(r), req.Document)
	if errors.Is(err, licensing.ErrInvalidDocument) {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

type SetupStateHandlers struct {
	Licensing      *licensing.Service
	Profiles       *profiles.Service
	Metadata       metadatabundle.Runtime
	Notifications  *notifications.Service
	RuntimeProfile string
}

type setupStateResponse struct {
	ActiveProfile                     string   `json:"activeProfile"`
	DesiredProfile                    string   `json:"desiredProfile,omitempty"`
	ActivationStatus                  string   `json:"activationStatus,omitempty"`
	ActiveMetadataVersion             string   `json:"activeMetadataVersion,omitempty"`
	PreviousMetadataVersion           string   `json:"previousMetadataVersion,omitempty"`
	LicensingUnresolved               bool     `json:"licensingUnresolved"`
	LicensingState                    string   `json:"licensingState"`
	ProfileActivationAvailable        bool     `json:"profileActivationAvailable"`
	MetadataBundleManagementAvailable bool     `json:"metadataBundleManagementAvailable"`
	BlockingSetupActions              []string `json:"blockingSetupActions"`
	AlertNotificationIDs              []string `json:"alertNotificationIds"`
}

func (h *SetupStateHandlers) Get(w http.ResponseWriter, r *http.Request) {
	st, err := h.Licensing.Status(r.Context())
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	act, err := h.Profiles.ActivationState(r.Context())
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	var metadataSt metadatabundle.Status
	if h.Metadata != nil {
		metadataSt, err = h.Metadata.Status(r.Context())
		if err != nil {
			WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
			return
		}
	}
	principal, _ := PrincipalFromContext(r.Context())
	notes, err := h.Notifications.List(r.Context(), principal.UserID)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	var blocking []string
	var alertIDs []string
	if st.State == storage.LicensingUnresolved {
		blocking = append(blocking, "licensing")
	}
	for _, n := range notes {
		alertIDs = append(alertIDs, n.ID)
	}
	writeJSON(w, http.StatusOK, setupStateResponse{
		ActiveProfile:                     h.RuntimeProfile,
		DesiredProfile:                    act.DesiredProfileID,
		ActivationStatus:                  act.Status,
		ActiveMetadataVersion:             metadataSt.ActiveMetadataVersion,
		PreviousMetadataVersion:           metadataSt.PreviousMetadataVersion,
		LicensingUnresolved:               !st.Resolved,
		LicensingState:                    st.State,
		ProfileActivationAvailable:        st.Resolved,
		MetadataBundleManagementAvailable: true,
		BlockingSetupActions:              blocking,
		AlertNotificationIDs:              alertIDs,
	})
}

type NotificationHandlers struct {
	Notifications *notifications.Service
	Audit         *audit.Recorder
}

func (h *NotificationHandlers) List(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required", "")
		return
	}
	items, err := h.Notifications.List(r.Context(), principal.UserID)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	if items == nil {
		items = []notifications.Notification{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *NotificationHandlers) Acknowledge(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required", "")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		WriteValidationProblem(w, r, "notification id is required", nil)
		return
	}
	if err := h.Notifications.Acknowledge(r.Context(), principal.UserID, id); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	if h.Audit != nil {
		if err := h.Audit.Record(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), audit.Event{
			Action: "notifications.acknowledge", TargetType: "notification", TargetID: id,
			Outcome: storage.AuditOutcomeSuccess,
		}); err != nil {
			WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
