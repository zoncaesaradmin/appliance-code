package identity

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"
)

func (s *PostgresStore) CreateDiscoveryLink(ctx context.Context, tokenHash [32]byte, principal Principal, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO identity_discovery_links (token_hash, user_id, tenant_id, channel_id, audience, expires_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`, tokenHash[:], principal.ApplicationUserID, principal.TenantID, principal.ChannelID, principal.Audience, expires, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("create discovery link: %w", err)
	}
	return nil
}

func (s *PostgresStore) LookupDiscoveryPrincipal(ctx context.Context, tokenHash [32]byte, now time.Time) (Principal, error) {
	var principal Principal
	err := s.db.QueryRowContext(ctx, `SELECT user_id, tenant_id, channel_id, audience FROM identity_discovery_links WHERE token_hash = $1 AND expires_at > $2 AND used_at IS NULL`, tokenHash[:], now).Scan(&principal.ApplicationUserID, &principal.TenantID, &principal.ChannelID, &principal.Audience)
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, fmt.Errorf("lookup discovery link: %w", err)
	}
	principal.AuthMethod = "channel_link"
	principal.Roles = []string{}
	principal.Permissions = []string{}
	principal.ZoneScopes = []string{}
	return principal, nil
}

func (s *PostgresStore) NearbyZones(ctx context.Context, tenantID string, latitude, longitude, radiusKM float64, limit int) ([]Zone, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, latitude, longitude FROM identity_zones WHERE tenant_id = $1 AND status = 'active'`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list nearby zones: %w", err)
	}
	defer rows.Close()
	type candidate struct {
		zone     Zone
		lat, lon float64
	}
	candidates := make([]candidate, 0)
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.zone.ID, &c.zone.Name, &c.lat, &c.lon); err != nil {
			return nil, fmt.Errorf("scan nearby zone: %w", err)
		}
		c.zone.Distance = haversineKM(latitude, longitude, c.lat, c.lon)
		if c.zone.Distance <= radiusKM {
			candidates = append(candidates, c)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list nearby zones: %w", err)
	}
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].zone.Distance < candidates[i].zone.Distance {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	zones := make([]Zone, len(candidates))
	for i := range candidates {
		zones[i] = candidates[i].zone
	}
	return zones, nil
}

func (s *PostgresStore) ListActiveMemberships(ctx context.Context, userID, tenantID string) ([]ActiveMembership, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.tenant_id, m.zone_id, z.name
		FROM identity_zone_memberships m
		JOIN identity_zones z ON z.id = m.zone_id AND z.tenant_id = m.tenant_id
		WHERE m.user_id = $1 AND m.tenant_id = $2 AND m.status = 'active' AND z.status = 'active'
		ORDER BY m.joined_at ASC, m.zone_id ASC`, userID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list active memberships: %w", err)
	}
	defer rows.Close()
	memberships := make([]ActiveMembership, 0)
	for rows.Next() {
		var membership ActiveMembership
		if err := rows.Scan(&membership.TenantID, &membership.ZoneID, &membership.Name); err != nil {
			return nil, fmt.Errorf("scan active membership: %w", err)
		}
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active memberships: %w", err)
	}
	return memberships, nil
}

func (s *PostgresStore) CreateZoneJoinRequest(ctx context.Context, userID, tenantID, zoneID string, latitude, longitude float64, now time.Time) (JoinRequest, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return JoinRequest{}, fmt.Errorf("begin zone join request: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var zoneLat, zoneLon float64
	var joinRadiusM int
	err = tx.QueryRowContext(ctx, `SELECT latitude, longitude, join_radius_m FROM identity_zones WHERE id = $1 AND tenant_id = $2 AND status = 'active' FOR SHARE`, zoneID, tenantID).Scan(&zoneLat, &zoneLon, &joinRadiusM)
	if errors.Is(err, sql.ErrNoRows) {
		return JoinRequest{}, ErrInvalidZone
	}
	if err != nil {
		return JoinRequest{}, fmt.Errorf("load join zone: %w", err)
	}
	distance := haversineKM(latitude, longitude, zoneLat, zoneLon)
	if distance > float64(joinRadiusM)/1000 {
		return JoinRequest{}, ErrJoinTooFar
	}
	var hasAdmin bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM identity_zone_admins WHERE zone_id = $1 AND tenant_id = $2 AND status = 'active')`, zoneID, tenantID).Scan(&hasAdmin); err != nil {
		return JoinRequest{}, fmt.Errorf("check Zonity admins: %w", err)
	}
	status := "pending"
	approvalMode := "admin_review"
	if !hasAdmin {
		status = "approved"
		approvalMode = "auto_no_admin"
	}
	var request JoinRequest
	autoApproved := status == "approved"
	var storedApprovedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `INSERT INTO identity_zone_join_requests (user_id, tenant_id, zone_id, status, approval_mode, approved_at, requested_at) VALUES ($1, $2, $3, $4, $5, CASE WHEN $6 THEN $7 ELSE NULL END, $7) ON CONFLICT (user_id, zone_id) DO UPDATE SET requested_at = identity_zone_join_requests.requested_at RETURNING zone_id, status, approval_mode, approved_at`, userID, tenantID, zoneID, status, approvalMode, autoApproved, now).Scan(&request.ZoneID, &request.Status, &request.ApprovalMode, &storedApprovedAt)
	if err != nil {
		return JoinRequest{}, fmt.Errorf("create zone join request: %w", err)
	}
	request.ApprovalMode = approvalMode
	request.DistanceKM = distance
	if request.Status == "approved" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO identity_zone_memberships (user_id, tenant_id, zone_id, status, joined_at) VALUES ($1, $2, $3, 'active', $4) ON CONFLICT (user_id, zone_id) DO UPDATE SET status = 'active'`, userID, tenantID, zoneID, now); err != nil {
			return JoinRequest{}, fmt.Errorf("create zone membership: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE identity_users SET onboarding_state = 'onboarded' WHERE id = $1`, userID); err != nil {
			return JoinRequest{}, fmt.Errorf("update onboarded state: %w", err)
		}
	} else if _, err := tx.ExecContext(ctx, `UPDATE identity_users SET onboarding_state = CASE WHEN onboarding_state = 'identified' THEN 'join_pending' ELSE onboarding_state END WHERE id = $1`, userID); err != nil {
		return JoinRequest{}, fmt.Errorf("update onboarding state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return JoinRequest{}, fmt.Errorf("commit zone join request: %w", err)
	}
	return request, nil
}

func (s *PostgresStore) CreateZoneCreationRequest(ctx context.Context, userID, tenantID, name string, now time.Time) (ZoneCreationRequest, error) {
	var request ZoneCreationRequest
	err := s.db.QueryRowContext(ctx, `INSERT INTO identity_zone_creation_requests (user_id, tenant_id, requested_name, status, requested_at) VALUES ($1, $2, $3, 'pending', $4) ON CONFLICT (user_id, requested_name) DO UPDATE SET requested_at = identity_zone_creation_requests.requested_at RETURNING requested_name, status`, userID, tenantID, name, now).Scan(&request.Name, &request.Status)
	if err != nil {
		return ZoneCreationRequest{}, fmt.Errorf("create zone creation request: %w", err)
	}
	return request, nil
}

func haversineKM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKM = 6371.0088
	lat1 *= math.Pi / 180
	lat2 *= math.Pi / 180
	dLat := lat2 - lat1
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusKM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func (s *PostgresStore) LookupPrincipal(ctx context.Context, token [32]byte, now time.Time) (Principal, error) {
	var principal Principal
	var subjectID, sessionID sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT s.user_id, s.tenant_id, s.channel_id, s.audience, s.auth_method,
		       s.session_id, cs.id, s.expires_at
		FROM identity_sessions s
		LEFT JOIN identity_channel_subjects cs ON cs.id = s.subject_id AND cs.status = 'linked'
		WHERE s.token_hash = $1 AND s.expires_at > $2 AND s.revoked_at IS NULL
		ORDER BY cs.created_at ASC NULLS LAST
		LIMIT 1`, token[:], now).Scan(
		&principal.ApplicationUserID, &principal.TenantID, &principal.ChannelID,
		&principal.Audience, &principal.AuthMethod, &sessionID, &subjectID, &principal.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, fmt.Errorf("lookup principal: %w", err)
	}
	if sessionID.Valid {
		principal.SessionID = sessionID.String
	}
	if subjectID.Valid {
		principal.SubjectID = subjectID.String
	}
	principal.Roles = []string{}
	principal.Permissions = []string{}
	principal.ZoneScopes = []string{}
	return principal, nil
}

func (s *PostgresStore) ResolveChannelSession(ctx context.Context, request ChannelSessionRequest, tokenHash [32]byte, sessionID string, now, expires time.Time) (Principal, error) {
	if request.TenantID == "" || request.ChannelID == "" || request.SubjectID == "" || sessionID == "" || !expires.After(now) {
		return Principal{}, ErrSubjectNotLinked
	}
	subjectKey := s.rateDigest("channel-subject:"+request.ChannelID, request.SubjectID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Principal{}, fmt.Errorf("begin channel resolution: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var audience string
	var autoRegister, enabled bool
	if err := tx.QueryRowContext(ctx, `SELECT audience, auto_register, enabled FROM identity_channels WHERE id = $1 AND tenant_id = $2 FOR SHARE`, request.ChannelID, request.TenantID).Scan(&audience, &autoRegister, &enabled); errors.Is(err, sql.ErrNoRows) {
		return Principal{}, ErrUnknownChannel
	} else if err != nil {
		return Principal{}, fmt.Errorf("load channel policy: %w", err)
	}
	if !enabled {
		return Principal{}, ErrUnknownChannel
	}
	var subjectID, userID, status string
	err = tx.QueryRowContext(ctx, `SELECT id, COALESCE(user_id, ''), status FROM identity_channel_subjects WHERE channel_id = $1 AND external_subject_key = $2 FOR UPDATE`, request.ChannelID, subjectKey).Scan(&subjectID, &userID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		if !autoRegister || request.Phone == "" {
			return Principal{}, ErrSubjectNotLinked
		}
		userID, err = findOrCreateUser(ctx, tx, request.Phone, now)
		if err != nil {
			return Principal{}, err
		}
		rawID := make([]byte, 16)
		if _, err := rand.Read(rawID); err != nil {
			return Principal{}, fmt.Errorf("generate channel subject id: %w", err)
		}
		subjectID = hex.EncodeToString(rawID)
		status = ChannelSubjectLinked
		if _, err := tx.ExecContext(ctx, `INSERT INTO identity_channel_subjects (id, channel_id, external_subject_key, user_id, status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $6)`, subjectID, request.ChannelID, subjectKey, userID, status, now); err != nil {
			return Principal{}, fmt.Errorf("create channel subject: %w", err)
		}
	} else if err != nil {
		return Principal{}, fmt.Errorf("load channel subject: %w", err)
	}
	if status == ChannelSubjectBlocked {
		return Principal{}, ErrSubjectBlocked
	}
	if userID == "" {
		if request.Phone == "" || !autoRegister {
			return Principal{}, ErrSubjectNotLinked
		}
		userID, err = findOrCreateUser(ctx, tx, request.Phone, now)
		if err != nil {
			return Principal{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE identity_channel_subjects SET user_id = $2, status = $3, updated_at = $4 WHERE id = $1`, subjectID, userID, ChannelSubjectLinked, now); err != nil {
			return Principal{}, fmt.Errorf("link channel subject: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO identity_sessions (token_hash, session_id, user_id, tenant_id, channel_id, subject_id, audience, auth_method, expires_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, tokenHash[:], sessionID, userID, request.TenantID, request.ChannelID, subjectID, audience, "channel_proof", expires, now); err != nil {
		return Principal{}, fmt.Errorf("create channel session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Principal{}, fmt.Errorf("commit channel session: %w", err)
	}
	return Principal{ApplicationUserID: userID, TenantID: request.TenantID, ChannelID: request.ChannelID, Audience: audience, SubjectID: subjectID, SessionID: sessionID, AuthMethod: "channel_proof", ExpiresAt: expires, Roles: []string{}, Permissions: []string{}, ZoneScopes: []string{}}, nil
}

func findOrCreateUser(ctx context.Context, tx *sql.Tx, phone string, now time.Time) (string, error) {
	rawID := make([]byte, 16)
	if _, err := rand.Read(rawID); err != nil {
		return "", fmt.Errorf("generate user id: %w", err)
	}
	var userID string
	if err := tx.QueryRowContext(ctx, `INSERT INTO identity_users (id, phone, created_at) VALUES ($1, $2, $3) ON CONFLICT (phone) DO UPDATE SET phone = EXCLUDED.phone RETURNING id`, hex.EncodeToString(rawID), phone, now).Scan(&userID); err != nil {
		return "", fmt.Errorf("find or create user: %w", err)
	}
	return userID, nil
}
