package identity

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalidPhone    = errors.New("invalid phone number")
	ErrUnauthenticated = errors.New("unauthenticated")
	phonePattern       = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
)

// DomainStore is the persistence contract for onboarding and channel identity.
// OTP challenges, sessions, and delivery are deliberately not part of this
// interface; Auth Service owns those concerns.
type DomainStore interface {
	OnboardingStore
	PrincipalStore
}

// Service contains only application-domain identity and onboarding behavior.
// Authentication is reached through principalResolver, which may point to an
// in-cluster Auth Service or a deployment-configured external endpoint.
type Service struct {
	store             DomainStore
	channelSessionTTL time.Duration
	origin            string
	principalResolver PrincipalResolver
	now               func() time.Time
}

func NewOnboardingService(store DomainStore, origin string, resolver PrincipalResolver) *Service {
	service := &Service{
		store:             store,
		channelSessionTTL: 10 * time.Minute,
		principalResolver: resolver,
		now:               time.Now,
	}
	service.SetOrigin(origin)
	return service
}

func (s *Service) SetOrigin(origin string) {
	s.origin = strings.TrimRight(strings.TrimSpace(origin), "/")
}
