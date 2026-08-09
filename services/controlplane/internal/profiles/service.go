package profiles

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/licensing"
	"appliance-code/services/controlplane/internal/metadatabundle"
	"appliance-code/services/controlplane/internal/storage"
)

var (
	ErrLicensingUnresolved = errors.New("profiles: licensing must be resolved first")
	ErrNotFound            = errors.New("profiles: profile not found")
	ErrValidationFailed    = errors.New("profiles: activation validation failed")
)

// ProfileView is the API-facing appliance profile from the active metadata bundle.
type ProfileView struct {
	ID              string   `json:"id"`
	DisplayName     string   `json:"displayName"`
	Description     string   `json:"description"`
	BuiltIn         bool     `json:"builtIn"`
	Active          bool     `json:"active"`
	Capabilities    []string `json:"capabilities"`
	MetadataVersion string   `json:"metadataVersion,omitempty"`
}

type CapabilityInfo struct {
	ID                  string   `json:"id"`
	DisplayName         string   `json:"displayName,omitempty"`
	Dependencies        []string `json:"dependencies"`
	Conflicts           []string `json:"conflicts,omitempty"`
	RequiredArtifacts   []string `json:"requiredArtifacts,omitempty"`
	RequiredEntitlement string   `json:"requiredEntitlement,omitempty"`
}

type ValidationGroup struct {
	Name    string   `json:"name"`
	OK      bool     `json:"ok"`
	Message string   `json:"message,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}

type ValidationResult struct {
	ProfileID       string            `json:"profileId"`
	MetadataVersion string            `json:"metadataVersion,omitempty"`
	OK              bool              `json:"ok"`
	Groups          []ValidationGroup `json:"groups"`
}

type ActivationResult struct {
	ProfileID       string `json:"profileId"`
	Status          string `json:"status"`
	Message         string `json:"message"`
	RequiresRestart bool   `json:"requiresRestart"`
}

type BundleChecker interface {
	ArtifactPresent(name string) bool
}

type CompleteBundleChecker struct{}

func (CompleteBundleChecker) ArtifactPresent(string) bool { return true }

type ManifestBundleChecker struct {
	Present map[string]struct{}
}

func (m ManifestBundleChecker) ArtifactPresent(name string) bool {
	_, ok := m.Present[name]
	return ok
}

// MetadataSource provides the active metadata bundle.
type MetadataSource interface {
	ActiveBundle(ctx context.Context) (*metadatabundle.Bundle, error)
}

type Service struct {
	store          storage.LicensingStore
	db             storage.DB
	audit          *audit.Recorder
	licensing      *licensing.Service
	metadata       MetadataSource
	runtimeProfile string
	bundle         BundleChecker
	now            func() time.Time
}

func NewService(
	db storage.DB,
	store storage.LicensingStore,
	licensingSvc *licensing.Service,
	metadata MetadataSource,
	recorder *audit.Recorder,
	runtimeProfile string,
	bundle BundleChecker,
) *Service {
	if bundle == nil {
		bundle = CompleteBundleChecker{}
	}
	return &Service{
		db:             db,
		store:          store,
		licensing:      licensingSvc,
		metadata:       metadata,
		audit:          recorder,
		runtimeProfile: strings.TrimSpace(runtimeProfile),
		bundle:         bundle,
		now:            time.Now,
	}
}

func (s *Service) ListCapabilities(ctx context.Context) []CapabilityInfo {
	b, err := s.metadata.ActiveBundle(ctx)
	if err != nil || b == nil {
		return nil
	}
	var out []CapabilityInfo
	ids := make([]string, 0, len(b.Capabilities.Capabilities))
	for id := range b.Capabilities.Capabilities {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		def := b.Capabilities.Capabilities[id]
		out = append(out, CapabilityInfo{
			ID:                  id,
			DisplayName:         def.DisplayName,
			Dependencies:        append([]string(nil), def.Requires...),
			Conflicts:           append([]string(nil), def.Conflicts...),
			RequiredArtifacts:   append([]string(nil), def.Artifacts.Required...),
			RequiredEntitlement: def.License.RequiredEntitlement,
		})
	}
	return out
}

func (s *Service) List(ctx context.Context) ([]ProfileView, error) {
	b, err := s.metadata.ActiveBundle(ctx)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, fmt.Errorf("profiles: no active metadata bundle")
	}
	activeID, err := s.activeProfileID(ctx)
	if err != nil {
		return nil, err
	}
	var out []ProfileView
	for _, id := range b.ProfileIDs() {
		def := b.Profiles.Profiles[id]
		out = append(out, ProfileView{
			ID:              id,
			DisplayName:     def.DisplayName,
			Description:     def.Description,
			BuiltIn:         true,
			Active:          id == activeID,
			Capabilities:    append([]string(nil), def.Capabilities...),
			MetadataVersion: b.Manifest.Metadata.MetadataVersion,
		})
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, id string) (ProfileView, error) {
	items, err := s.List(ctx)
	if err != nil {
		return ProfileView{}, err
	}
	for _, p := range items {
		if p.ID == id {
			return p, nil
		}
	}
	return ProfileView{}, ErrNotFound
}

func (s *Service) Validate(ctx context.Context, profileID string) (ValidationResult, error) {
	b, err := s.metadata.ActiveBundle(ctx)
	if err != nil {
		return ValidationResult{}, err
	}
	if b == nil {
		return ValidationResult{}, fmt.Errorf("profiles: no active metadata bundle")
	}
	def, ok := b.Profiles.Profiles[profileID]
	if !ok {
		return ValidationResult{}, ErrNotFound
	}
	return s.validateCapabilities(ctx, profileID, def.Capabilities, b), nil
}

func (s *Service) Activate(ctx context.Context, actor audit.Actor, profileID string) (ActivationResult, ValidationResult, error) {
	if err := s.requireResolved(ctx); err != nil {
		return ActivationResult{}, ValidationResult{}, err
	}
	validation, err := s.Validate(ctx, profileID)
	if err != nil {
		return ActivationResult{}, ValidationResult{}, err
	}
	if !validation.OK {
		return ActivationResult{}, validation, ErrValidationFailed
	}
	now := s.now().UTC()
	message := fmt.Sprintf("Profile %q accepted; restart the control plane with this appliance profile to complete activation.", profileID)
	err = s.db.WithTx(ctx, func(txCtx context.Context) error {
		rec := storage.ProfileActivationRecord{
			DesiredProfileID: profileID,
			Status:           storage.ProfileActivationPendingRestart,
			Message:          message,
			UpdatedAt:        now,
			UpdatedByUserID:  actor.UserID,
		}
		if profileID == s.runtimeProfile {
			rec.Status = storage.ProfileActivationActive
			rec.Message = "Profile is already active for this control-plane process."
		}
		if err := s.store.PutActivation(txCtx, rec); err != nil {
			return err
		}
		if s.audit != nil {
			return s.audit.Record(txCtx, actor, audit.Event{
				Action:     "profiles.activate",
				TargetType: "appliance_profile",
				TargetID:   profileID,
				Outcome:    storage.AuditOutcomeSuccess,
				Details: map[string]any{
					"status":          rec.Status,
					"requiresRestart": rec.Status == storage.ProfileActivationPendingRestart,
					"metadataVersion": validation.MetadataVersion,
				},
			})
		}
		return nil
	})
	if err != nil {
		return ActivationResult{}, validation, err
	}
	act, err := s.store.GetActivation(ctx)
	if err != nil {
		return ActivationResult{}, validation, err
	}
	return ActivationResult{
		ProfileID:       profileID,
		Status:          act.Status,
		Message:         act.Message,
		RequiresRestart: act.Status == storage.ProfileActivationPendingRestart,
	}, validation, nil
}

func (s *Service) ActivationState(ctx context.Context) (storage.ProfileActivationRecord, error) {
	return s.store.GetActivation(ctx)
}

func (s *Service) requireResolved(ctx context.Context) error {
	ok, err := s.licensing.IsResolved(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return ErrLicensingUnresolved
	}
	return nil
}

func (s *Service) activeProfileID(ctx context.Context) (string, error) {
	act, err := s.store.GetActivation(ctx)
	if err != nil {
		return "", err
	}
	if act.DesiredProfileID != "" && act.Status == storage.ProfileActivationActive {
		return act.DesiredProfileID, nil
	}
	return s.runtimeProfile, nil
}

func (s *Service) validateCapabilities(ctx context.Context, profileID string, caps []string, b *metadatabundle.Bundle) ValidationResult {
	result := ValidationResult{
		ProfileID:       profileID,
		MetadataVersion: b.Manifest.Metadata.MetadataVersion,
		OK:              true,
	}

	defGroup := ValidationGroup{Name: "profile_definition", OK: true, Message: "Profile definition is valid"}
	enabled := map[string]struct{}{}
	for _, c := range caps {
		enabled[c] = struct{}{}
		capDef, ok := b.Capabilities.Capabilities[c]
		if !ok {
			defGroup.OK = false
			defGroup.Errors = append(defGroup.Errors, fmt.Sprintf("unknown capability %q", c))
			continue
		}
		for _, dep := range capDef.Requires {
			if _, present := enabled[dep]; !present {
				// check full set
			}
		}
	}
	for _, c := range caps {
		capDef := b.Capabilities.Capabilities[c]
		for _, dep := range capDef.Requires {
			if _, present := enabled[dep]; !present {
				defGroup.OK = false
				defGroup.Errors = append(defGroup.Errors, fmt.Sprintf("capability %q missing dependency %q", c, dep))
			}
		}
	}
	if !defGroup.OK {
		defGroup.Message = "Profile definition is invalid"
		result.OK = false
	}
	result.Groups = append(result.Groups, defGroup)

	bundleGroup := ValidationGroup{Name: "bundle_availability", OK: true, Message: "Required bundle artifacts are present"}
	for _, c := range caps {
		for _, artifact := range b.Capabilities.Capabilities[c].Artifacts.Required {
			if !s.bundle.ArtifactPresent(artifact) {
				bundleGroup.OK = false
				bundleGroup.Errors = append(bundleGroup.Errors,
					fmt.Sprintf("required artifact %q for capability %q is not present in the installed software bundle", artifact, c))
			}
		}
	}
	if !bundleGroup.OK {
		bundleGroup.Message = "Required bundle artifacts are missing"
		result.OK = false
	}
	result.Groups = append(result.Groups, bundleGroup)

	licenseGroup := ValidationGroup{Name: "license_entitlement", OK: true, Message: "License entitlement allows requested capabilities"}
	entitled, err := s.licensing.Entitlements(ctx)
	if err != nil {
		licenseGroup.OK = false
		licenseGroup.Errors = append(licenseGroup.Errors, err.Error())
	} else {
		entitledSet := map[string]struct{}{}
		for _, e := range entitled {
			entitledSet[e] = struct{}{}
		}
		if len(entitled) == 0 {
			licenseGroup.OK = false
			licenseGroup.Errors = append(licenseGroup.Errors, "licensing is unresolved or grants no capabilities")
		}
		for _, c := range caps {
			if _, ok := entitledSet[c]; !ok {
				licenseGroup.OK = false
				licenseGroup.Errors = append(licenseGroup.Errors, fmt.Sprintf("capability %q is not licensed", c))
			}
		}
	}
	if !licenseGroup.OK {
		licenseGroup.Message = "License entitlement check failed"
		result.OK = false
	}
	result.Groups = append(result.Groups, licenseGroup)
	return result
}
