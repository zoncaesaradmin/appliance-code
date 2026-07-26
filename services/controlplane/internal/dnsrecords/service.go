package dnsrecords

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/storage"
)

const (
	DefaultZone          = "appliance.internal"
	DefaultAdminTTL      = 300
	DefaultPeerTTL       = 60
	DefaultPeerLease     = 15 * time.Minute
	DefaultConfigMapNS   = "dns"
	DefaultConfigMapName = "appliance-dns-config"
)

var (
	ErrConflict    = errors.New("dnsrecords: ownership conflict")
	ErrInvalidName = errors.New("dnsrecords: invalid name")
	ErrInvalidIPv4 = errors.New("dnsrecords: invalid ipv4")
	namePattern    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// Config controls zone identity and bootstrap seeding.
type Config struct {
	Zone               string
	ConfigMapNamespace string
	ConfigMapName      string
	BootstrapHostname  string
	BootstrapIPv4      string
	PeerLease          time.Duration
}

// Service owns LAN DNS record CRUD, lease expiry, and zone sync.
type Service struct {
	store  storage.DNSRecordStore
	db     storage.DB
	syncer ZoneSyncer
	audit  *audit.Recorder
	cfg    Config
	serial atomic.Int64
}

func NewService(store storage.DNSRecordStore, db storage.DB, syncer ZoneSyncer, recorder *audit.Recorder, cfg Config) *Service {
	if strings.TrimSpace(cfg.Zone) == "" {
		cfg.Zone = DefaultZone
	}
	if strings.TrimSpace(cfg.ConfigMapNamespace) == "" {
		cfg.ConfigMapNamespace = DefaultConfigMapNS
	}
	if strings.TrimSpace(cfg.ConfigMapName) == "" {
		cfg.ConfigMapName = DefaultConfigMapName
	}
	if cfg.PeerLease <= 0 {
		cfg.PeerLease = DefaultPeerLease
	}
	svc := &Service{store: store, db: db, syncer: syncer, audit: recorder, cfg: cfg}
	svc.serial.Store(time.Now().UTC().Unix())
	return svc
}

func (s *Service) Zone() string { return s.cfg.Zone }

func NormalizeName(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimSuffix(name, ".")
	if i := strings.IndexByte(name, '.'); i >= 0 {
		// Accept FQDN under appliance.internal by stripping the zone suffix.
		name = name[:i]
	}
	if name == "" || name == "ns" || !namePattern.MatchString(name) {
		return "", fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	return name, nil
}

func validateIPv4(ip string) error {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil || parsed.To4() == nil {
		return fmt.Errorf("%w: %q", ErrInvalidIPv4, ip)
	}
	return nil
}

func (s *Service) List(ctx context.Context) ([]storage.DNSRecord, error) {
	if _, err := s.expireLocked(ctx); err != nil {
		return nil, err
	}
	return s.store.List(ctx)
}

type UpsertInput struct {
	Name    string
	IPv4    string
	TTL     int
	Source  storage.DNSRecordSource
	Owner   string
	Actor   audit.Actor
	AsAdmin bool
}

func (s *Service) Upsert(ctx context.Context, in UpsertInput) (storage.DNSRecord, error) {
	name, err := NormalizeName(in.Name)
	if err != nil {
		return storage.DNSRecord{}, err
	}
	if err := validateIPv4(in.IPv4); err != nil {
		return storage.DNSRecord{}, err
	}
	now := time.Now().UTC()
	ttl := in.TTL
	source := in.Source
	owner := strings.TrimSpace(in.Owner)

	var lease *time.Time
	if in.AsAdmin {
		if source == "" {
			source = storage.DNSRecordSourceAdmin
		}
		if ttl <= 0 {
			ttl = DefaultAdminTTL
		}
		lease = nil
	} else {
		source = storage.DNSRecordSourcePeer
		if ttl <= 0 {
			ttl = DefaultPeerTTL
		}
		if owner == "" {
			return storage.DNSRecord{}, fmt.Errorf("dnsrecords: owner is required for peer registration")
		}
		exp := now.Add(s.cfg.PeerLease)
		lease = &exp
	}

	existing, err := s.store.Get(ctx, name)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return storage.DNSRecord{}, err
	}
	createdAt := now
	if err == nil {
		createdAt = existing.CreatedAt
		if !in.AsAdmin {
			if existing.Source == storage.DNSRecordSourceAdmin || existing.Source == storage.DNSRecordSourceBootstrap {
				if existing.Owner != "" && existing.Owner != owner {
					return storage.DNSRecord{}, fmt.Errorf("%w: %s is owned by %s", ErrConflict, name, existing.Owner)
				}
				if existing.Owner == "" {
					return storage.DNSRecord{}, fmt.Errorf("%w: %s is an admin/bootstrap record", ErrConflict, name)
				}
			}
			if existing.Owner != "" && existing.Owner != owner {
				return storage.DNSRecord{}, fmt.Errorf("%w: %s is owned by %s", ErrConflict, name, existing.Owner)
			}
		}
	}

	rec := storage.DNSRecord{
		Name:           name,
		IPv4:           strings.TrimSpace(in.IPv4),
		TTL:            ttl,
		Source:         source,
		Owner:          owner,
		CreatedAt:      createdAt,
		UpdatedAt:      now,
		LeaseExpiresAt: lease,
	}
	if in.AsAdmin && owner == "" && existing.Owner != "" && source == storage.DNSRecordSourceAdmin {
		// Admin overwrite clears peer ownership unless explicitly set.
		rec.Owner = ""
	}

	err = s.db.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.store.Upsert(txCtx, rec); err != nil {
			return err
		}
		if s.audit != nil {
			return s.audit.Record(txCtx, in.Actor, audit.Event{
				Action:     "dns.record.upsert",
				TargetType: "dns_record",
				TargetID:   name,
				Outcome:    storage.AuditOutcomeSuccess,
				Details: map[string]any{
					"ipv4":   rec.IPv4,
					"ttl":    rec.TTL,
					"source": string(rec.Source),
					"owner":  rec.Owner,
				},
			})
		}
		return nil
	})
	if err != nil {
		return storage.DNSRecord{}, err
	}
	if err := s.ReconcileZone(ctx); err != nil {
		return rec, fmt.Errorf("dnsrecords: upserted %s but zone sync failed: %w", name, err)
	}
	return rec, nil
}

func (s *Service) Delete(ctx context.Context, name string, actor audit.Actor) error {
	name, err := NormalizeName(name)
	if err != nil {
		return err
	}
	err = s.db.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.store.Delete(txCtx, name); err != nil {
			return err
		}
		if s.audit != nil {
			return s.audit.Record(txCtx, actor, audit.Event{
				Action:     "dns.record.delete",
				TargetType: "dns_record",
				TargetID:   name,
				Outcome:    storage.AuditOutcomeSuccess,
			})
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.ReconcileZone(ctx)
}

func (s *Service) BootstrapSelf(ctx context.Context) error {
	host := strings.TrimSpace(s.cfg.BootstrapHostname)
	ip := strings.TrimSpace(s.cfg.BootstrapIPv4)
	if host == "" || ip == "" {
		return s.ReconcileZone(ctx)
	}
	name, err := NormalizeName(host)
	if err != nil {
		return err
	}
	if err := validateIPv4(ip); err != nil {
		return err
	}
	now := time.Now().UTC()
	existing, err := s.store.Get(ctx, name)
	createdAt := now
	if err == nil {
		createdAt = existing.CreatedAt
		// Do not clobber an admin override of the bootstrap name's ownership
		// semantics; still refresh IP for bootstrap/peer-owned self names.
		if existing.Source == storage.DNSRecordSourceAdmin && existing.Owner == "" {
			// Keep admin static IP unless bootstrap is re-asserting self.
			return s.ReconcileZone(ctx)
		}
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	rec := storage.DNSRecord{
		Name:      name,
		IPv4:      ip,
		TTL:       DefaultAdminTTL,
		Source:    storage.DNSRecordSourceBootstrap,
		Owner:     "self",
		CreatedAt: createdAt,
		UpdatedAt: now,
	}
	if err := s.store.Upsert(ctx, rec); err != nil {
		return err
	}
	return s.ReconcileZone(ctx)
}

func (s *Service) ExpireStale(ctx context.Context) (int, error) {
	n, err := s.expireLocked(ctx)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		if err := s.ReconcileZone(ctx); err != nil {
			return n, err
		}
	}
	return n, nil
}

func (s *Service) expireLocked(ctx context.Context) (int, error) {
	return s.store.DeleteExpired(ctx, time.Now().UTC())
}

func (s *Service) ReconcileZone(ctx context.Context) error {
	if _, err := s.expireLocked(ctx); err != nil {
		return err
	}
	records, err := s.store.List(ctx)
	if err != nil {
		return err
	}
	serial := s.serial.Add(1)
	nsIP := s.cfg.BootstrapIPv4
	zone := RenderZoneFile(s.cfg.Zone, records, serial, nsIP)
	if s.syncer == nil {
		return nil
	}
	return s.syncer.PatchZone(ctx, zone)
}
