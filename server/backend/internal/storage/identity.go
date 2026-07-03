package storage

import "context"
import "time"

// UserState is the lifecycle state of a local user account.
type UserState string

const (
	UserStateActive   UserState = "active"
	UserStateDisabled UserState = "disabled"
)

// User is a local appliance account. Usernames are immutable once created;
// DisplayName is the only mutable presentation attribute.
type User struct {
	ID                string
	Username          string
	DisplayName       string
	State             UserState
	CredentialVersion int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// PasswordCredential is the Argon2id-hashed local password for a user,
// stored separately from the user profile so a future external identity
// provider can be linked without touching this table.
type PasswordCredential struct {
	UserID    string
	Algorithm string
	Params    string // JSON-encoded algorithm parameters recorded with the hash
	Salt      []byte
	Hash      []byte
	UpdatedAt time.Time
}

// PasswordResetCredential is a single-use, time-limited, administrator-issued
// reset credential. Only its keyed digest is stored; LookupID is a
// non-secret value used to find the row before a constant-time digest
// comparison.
type PasswordResetCredential struct {
	ID        string
	UserID    string
	LookupID  string
	Digest    []byte
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// UserStore persists User and credential records.
type UserStore interface {
	Create(ctx context.Context, u User) error
	Get(ctx context.Context, id string) (User, error)
	GetByUsername(ctx context.Context, username string) (User, error)
	List(ctx context.Context) ([]User, error)
	UpdateDisplayName(ctx context.Context, id, displayName string) error
	SetState(ctx context.Context, id string, state UserState) error
	BumpCredentialVersion(ctx context.Context, id string) error
	CountEnabledAdministrators(ctx context.Context, adminRoleID string) (int, error)

	SetPassword(ctx context.Context, cred PasswordCredential) error
	GetPasswordCredential(ctx context.Context, userID string) (PasswordCredential, error)

	CreatePasswordReset(ctx context.Context, cred PasswordResetCredential) error
	GetPasswordResetByLookupID(ctx context.Context, lookupID string) (PasswordResetCredential, error)
	MarkPasswordResetUsed(ctx context.Context, id string) error
}

// Permission is one entry in the fixed, published permission catalog.
type Permission struct {
	Name        string
	Description string
}

// Role is a named bundle of permissions. Built-in roles have stable IDs and
// names; only their effective permission set may change across versions.
type Role struct {
	ID        string
	Name      string
	BuiltIn   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RoleStore persists the permission catalog, roles, role-permission
// assignments, and user-role assignments.
type RoleStore interface {
	UpsertPermission(ctx context.Context, p Permission) error
	ListPermissions(ctx context.Context) ([]Permission, error)

	UpsertRole(ctx context.Context, r Role) error
	Get(ctx context.Context, id string) (Role, error)
	GetByName(ctx context.Context, name string) (Role, error)
	List(ctx context.Context) ([]Role, error)
	Delete(ctx context.Context, id string) error

	SetRolePermissions(ctx context.Context, roleID string, permissionNames []string) error
	ListRolePermissions(ctx context.Context, roleID string) ([]string, error)

	AssignUserRole(ctx context.Context, userID, roleID string) error
	RemoveUserRole(ctx context.Context, userID, roleID string) error
	SetUserRoles(ctx context.Context, userID string, roleIDs []string) error
	ListUserRoles(ctx context.Context, userID string) ([]Role, error)
	ListUsersWithRole(ctx context.Context, roleID string) ([]string, error)
}

// APIToken is a long-lived, revocable, hashed-at-rest automation credential.
type APIToken struct {
	ID         string
	UserID     string
	Name       string
	LookupID   string
	Digest     []byte
	Scopes     []string // nil means "inherit all owner permissions"
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// TokenStore persists APIToken records.
type TokenStore interface {
	Create(ctx context.Context, t APIToken) error
	GetByLookupID(ctx context.Context, lookupID string) (APIToken, error)
	Get(ctx context.Context, id string) (APIToken, error)
	ListByUser(ctx context.Context, userID string) ([]APIToken, error)
	Revoke(ctx context.Context, id string) error
	RevokeAllForUser(ctx context.Context, userID string) error
	TouchLastUsed(ctx context.Context, id string, when time.Time) error
}

// SessionFamily is one interactive login's chain of rotating refresh
// credentials, bounded by an absolute lifetime regardless of activity.
type SessionFamily struct {
	ID                string
	UserID            string
	CreatedAt         time.Time
	LastUsedAt        time.Time
	AbsoluteExpiresAt time.Time
	RevokedAt         *time.Time
	RevokedReason     string
}

// RefreshCredential is the current and immediately-previous rotating
// refresh secret digest for one session family. Keeping one prior
// generation lets the store detect reuse of an already-rotated credential
// without keeping unbounded history.
type RefreshCredential struct {
	FamilyID       string
	CurrentDigest  []byte
	PreviousDigest []byte
	Version        int
	ExpiresAt      time.Time
	RotatedAt      time.Time
}

// SessionStore persists SessionFamily and RefreshCredential records.
type SessionStore interface {
	CreateFamily(ctx context.Context, family SessionFamily, refresh RefreshCredential) error
	GetFamily(ctx context.Context, id string) (SessionFamily, error)
	ListActiveFamiliesForUser(ctx context.Context, userID string) ([]SessionFamily, error)
	RevokeFamily(ctx context.Context, id, reason string) error
	RevokeAllFamiliesForUser(ctx context.Context, userID, reason string) error
	TouchFamily(ctx context.Context, id string, lastUsedAt time.Time) error

	GetRefresh(ctx context.Context, familyID string) (RefreshCredential, error)
	RotateRefresh(ctx context.Context, familyID string, newDigest []byte, expiresAt time.Time) error
}

// AuditActorType classifies who performed an audited action.
type AuditActorType string

const (
	AuditActorUser      AuditActorType = "user"
	AuditActorAPIToken  AuditActorType = "api_token"
	AuditActorSystem    AuditActorType = "system"
	AuditActorAnonymous AuditActorType = "anonymous"
)

// AuditOutcome is the result of the audited action.
type AuditOutcome string

const (
	AuditOutcomeSuccess AuditOutcome = "success"
	AuditOutcomeFailure AuditOutcome = "failure"
	AuditOutcomeDenied  AuditOutcome = "denied"
)

// AuditSeverity flags events that need elevated operator attention.
type AuditSeverity string

const (
	AuditSeverityInfo AuditSeverity = "info"
	AuditSeverityHigh AuditSeverity = "high"
)

// AuditEvent is one immutable, hash-chained audit record. Details must never
// contain passwords, raw tokens, authorization headers, or other secrets.
type AuditEvent struct {
	ID           string
	OccurredAt   time.Time
	ActorUserID  string
	ActorType    AuditActorType
	AuthMethod   string
	CredentialID string
	Action       string
	TargetType   string
	TargetID     string
	Outcome      AuditOutcome
	ReasonCode   string
	RequestID    string
	SourceAddr   string
	Severity     AuditSeverity
	Details      []byte // redacted JSON
}

// AuditFilter narrows AuditStore.List results.
type AuditFilter struct {
	ActorUserID string
	Action      string
	Since       time.Time
	Limit       int
}

// AuditStore appends and lists AuditEvent records. Appends are hash-chained
// so tampering with a stored record is detectable, though not tamper-proof
// against host root access.
type AuditStore interface {
	Append(ctx context.Context, event AuditEvent) error
	List(ctx context.Context, filter AuditFilter) ([]AuditEvent, error)
	VerifyChain(ctx context.Context) error
}

// ThrottleState is the durable per-account login-failure counter used to
// implement progressive delay and temporary lockout independent of process
// restarts.
type ThrottleState struct {
	Username       string
	FailureCount   int
	FirstFailureAt time.Time
	LastFailureAt  time.Time
	LockedUntil    time.Time
}

// ThrottleStore persists ThrottleState per username.
type ThrottleStore interface {
	Get(ctx context.Context, username string) (ThrottleState, error)
	RecordFailure(ctx context.Context, username string, now time.Time, windowReset time.Duration, lockDuration time.Duration, lockThreshold int) (ThrottleState, error)
	Reset(ctx context.Context, username string) error
}
