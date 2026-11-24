package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	maxNearbyRadiusKM = 10.0
	maxNearbyResults  = 5
)

var (
	ErrInvalidLocation = errors.New("invalid location")
	ErrInvalidZone     = errors.New("invalid zone")
	ErrInvalidZoneName = errors.New("invalid zone name")
	ErrJoinExists      = errors.New("join request already exists")
)

type Zone struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Distance float64 `json:"distance_km"`
}

type JoinRequest struct {
	ZoneID string `json:"zone_id"`
	Status string `json:"status"`
}

type ZoneCreationRequest struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type OnboardingStore interface {
	CreateDiscoveryLink(context.Context, [32]byte, Principal, time.Time) error
	LookupDiscoveryPrincipal(context.Context, [32]byte, time.Time) (Principal, error)
	NearbyZones(context.Context, string, float64, float64, float64, int) ([]Zone, error)
	CreateZoneJoinRequest(context.Context, string, string, string, time.Time) (JoinRequest, error)
	CreateZoneCreationRequest(context.Context, string, string, string, time.Time) (ZoneCreationRequest, error)
}

func (s *Service) SetOrigin(origin string) {
	s.origin = strings.TrimRight(strings.TrimSpace(origin), "/")
}

func (s *Service) ResolveChannelDiscovery(ctx context.Context, request ChannelSessionRequest) (string, Principal, string, error) {
	token, principal, err := s.ResolveChannelSession(ctx, request)
	if err != nil {
		return "", Principal{}, "", err
	}
	store, ok := s.store.(OnboardingStore)
	if !ok {
		return token, principal, "", nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", Principal{}, "", fmt.Errorf("generate discovery token: %w", err)
	}
	discoveryToken := base64.RawURLEncoding.EncodeToString(raw)
	expires := s.now().Add(15 * time.Minute)
	if err := store.CreateDiscoveryLink(ctx, sha256.Sum256([]byte(discoveryToken)), principal, expires); err != nil {
		return "", Principal{}, "", err
	}
	if s.origin == "" {
		return token, principal, "", nil
	}
	return token, principal, s.origin + "/app/discover?token=" + discoveryToken, nil
}

func (s *Service) Nearby(ctx context.Context, sessionToken, discoveryToken string, latitude, longitude, radiusKM float64) ([]Zone, Principal, error) {
	if !validCoordinates(latitude, longitude) || radiusKM <= 0 || radiusKM > maxNearbyRadiusKM {
		return nil, Principal{}, ErrInvalidLocation
	}
	principal, err := s.principalForOnboarding(ctx, sessionToken, discoveryToken)
	if err != nil {
		return nil, Principal{}, err
	}
	store, ok := s.store.(OnboardingStore)
	if !ok {
		return nil, Principal{}, errors.New("onboarding is unavailable")
	}
	zones, err := store.NearbyZones(ctx, principal.TenantID, latitude, longitude, radiusKM, maxNearbyResults)
	return zones, principal, err
}

func (s *Service) RequestJoin(ctx context.Context, sessionToken, discoveryToken, zoneID string) (JoinRequest, Principal, error) {
	zoneID = strings.TrimSpace(zoneID)
	if zoneID == "" || len(zoneID) > 128 {
		return JoinRequest{}, Principal{}, ErrInvalidZone
	}
	if strings.TrimSpace(discoveryToken) != "" {
		return JoinRequest{}, Principal{}, ErrUnauthenticated
	}
	principal, err := s.principalForOnboarding(ctx, sessionToken, discoveryToken)
	if err != nil {
		return JoinRequest{}, Principal{}, err
	}
	store, ok := s.store.(OnboardingStore)
	if !ok {
		return JoinRequest{}, Principal{}, errors.New("onboarding is unavailable")
	}
	request, err := store.CreateZoneJoinRequest(ctx, principal.ApplicationUserID, principal.TenantID, zoneID, s.now())
	return request, principal, err
}

// RequestZoneCreation records a request only. It deliberately has no method
// that creates a zone; creation remains a tenant/platform administrator task.
func (s *Service) RequestZoneCreation(ctx context.Context, sessionToken, name string) (ZoneCreationRequest, Principal, error) {
	name = strings.TrimSpace(name)
	if len([]rune(name)) < 2 || len([]rune(name)) > 100 {
		return ZoneCreationRequest{}, Principal{}, ErrInvalidZoneName
	}
	principal, err := s.Principal(ctx, sessionToken)
	if err != nil {
		return ZoneCreationRequest{}, Principal{}, err
	}
	store, ok := s.store.(OnboardingStore)
	if !ok {
		return ZoneCreationRequest{}, Principal{}, errors.New("onboarding is unavailable")
	}
	request, err := store.CreateZoneCreationRequest(ctx, principal.ApplicationUserID, principal.TenantID, name, s.now())
	return request, principal, err
}

func (s *Service) principalForOnboarding(ctx context.Context, sessionToken, discoveryToken string) (Principal, error) {
	if strings.TrimSpace(discoveryToken) != "" {
		raw, err := base64.RawURLEncoding.DecodeString(discoveryToken)
		if err != nil || len(raw) != 32 {
			return Principal{}, ErrUnauthenticated
		}
		store, ok := s.store.(OnboardingStore)
		if !ok {
			return Principal{}, errors.New("onboarding is unavailable")
		}
		return store.LookupDiscoveryPrincipal(ctx, sha256.Sum256([]byte(discoveryToken)), s.now())
	}
	return s.Principal(ctx, sessionToken)
}

func validCoordinates(latitude, longitude float64) bool {
	return !math.IsNaN(latitude) && !math.IsInf(latitude, 0) && !math.IsNaN(longitude) && !math.IsInf(longitude, 0) && latitude >= -90 && latitude <= 90 && longitude >= -180 && longitude <= 180
}
