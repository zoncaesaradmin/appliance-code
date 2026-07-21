package applianceclient

import "time"

// LoginResult is returned by Login and Refresh: a session access token plus
// its rotating opaque refresh credential.
type LoginResult struct {
	AccessToken     string    `json:"accessToken"`
	RefreshToken    string    `json:"refreshToken"`
	AccessExpiresAt time.Time `json:"accessExpiresAt"`
}

// SessionInfo describes the authenticated principal behind a bearer
// credential, as returned by GET /api/v1/auth/session.
type SessionInfo struct {
	UserID      string   `json:"userId"`
	AuthMethod  string   `json:"authMethod"`
	Permissions []string `json:"permissions"`
}

// User mirrors the appliance's public user representation. Password
// material is never present in any appliance API response.
type User struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	State       string    `json:"state"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Role mirrors the appliance's role representation.
type Role struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BuiltIn bool   `json:"builtIn"`
}

// Permission is one entry in the published permission catalog.
type Permission struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// PasswordResetResult carries the one-time reset credential returned by the
// administrator-triggered password-reset initiation API.
type PasswordResetResult struct {
	ResetCredential string `json:"resetCredential"`
}

// APIToken mirrors the appliance's token metadata representation. The raw
// secret is only ever present on CreateTokenResult, at creation time.
type APIToken struct {
	ID         string     `json:"id"`
	UserID     string     `json:"userId"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

// CreateTokenResult is returned only once, at token-creation time, and
// carries the raw secret alongside the token's metadata.
type CreateTokenResult struct {
	Token string `json:"token"`
	APIToken
}

// RegistryTokenResult is returned by RegistryToken: a short-lived signed
// token to present to the OCI registry data plane.
type RegistryTokenResult struct {
	Token     string    `json:"token"`
	ExpiresIn int       `json:"expires_in"`
	IssuedAt  time.Time `json:"issued_at"`
}

// RegistryGrant mirrors one repository-prefix grant.
type RegistryGrant struct {
	ID          string    `json:"id"`
	SubjectType string    `json:"subjectType"`
	SubjectID   string    `json:"subjectId"`
	PathPrefix  string    `json:"pathPrefix"`
	Actions     []string  `json:"actions"`
	CreatedAt   time.Time `json:"createdAt"`
}

// RegistryReferrer mirrors one referrer descriptor returned by the
// registry-catalog API.
type RegistryReferrer struct {
	MediaType    string `json:"mediaType"`
	Digest       string `json:"digest"`
	Size         int64  `json:"size"`
	ArtifactType string `json:"artifactType,omitempty"`
}

// Build mirrors the appliance's build representation.
type Build struct {
	ID                 string     `json:"id"`
	OwnerID            string     `json:"ownerId"`
	Status             string     `json:"status"`
	SourceRepoURL      string     `json:"sourceRepoUrl"`
	SourceCommitSHA    string     `json:"sourceCommitSha"`
	ContainerfilePath  string     `json:"containerfilePath"`
	ImageRepository    string     `json:"imageRepository"`
	ImageTag           string     `json:"imageTag"`
	BuilderImageDigest string     `json:"builderImageDigest"`
	ReasonCode         string     `json:"reasonCode,omitempty"`
	ErrorMessage       string     `json:"errorMessage,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	StartedAt          *time.Time `json:"startedAt,omitempty"`
	CompletedAt        *time.Time `json:"completedAt,omitempty"`
	DeadlineAt         time.Time  `json:"deadlineAt"`
}

// WorkProfile describes one configured developer workflow profile.
type WorkProfile struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Repos       []WorkProfileRepo `json:"repos,omitempty"`
}

type WorkProfileRepo struct {
	Name             string `json:"name"`
	EnabledByDefault bool   `json:"enabledByDefault,omitempty"`
}

// Workspace mirrors one server-side developer workflow workspace.
type Workspace struct {
	ID            string     `json:"id"`
	OwnerID       string     `json:"ownerId"`
	Name          string     `json:"name"`
	WorkProfile   string     `json:"workProfile"`
	SourceRepoURL string     `json:"sourceRepoUrl"`
	SourceRef     string     `json:"sourceRef"`
	Status        string     `json:"status"`
	ReasonCode    string     `json:"reasonCode,omitempty"`
	ErrorMessage  string     `json:"errorMessage,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	DeletedAt     *time.Time `json:"deletedAt,omitempty"`
}

// BuildTarget describes one configured target available for a workspace.
type BuildTarget struct {
	Name              string   `json:"name"`
	Aliases           []string `json:"aliases,omitempty"`
	Description       string   `json:"description,omitempty"`
	Repo              string   `json:"repo"`
	Execution         string   `json:"execution"`
	Args              []string `json:"args,omitempty"`
	ContainerfilePath string   `json:"containerfilePath"`
	ImageRepository   string   `json:"imageRepository"`
}

// BuilderGitAccessStatus mirrors the shared appliance-side HTTPS Git access
// configuration used by builder workflows.
type BuilderGitAccessStatus struct {
	Configured    bool     `json:"configured"`
	Host          string   `json:"host,omitempty"`
	Username      string   `json:"username,omitempty"`
	RequiredHosts []string `json:"requiredHosts,omitempty"`
	CanConfigure  bool     `json:"canConfigure"`
}

// Job mirrors one durable developer workflow job.
type Job struct {
	ID           string     `json:"id"`
	OwnerID      string     `json:"ownerId"`
	WorkspaceID  string     `json:"workspaceId,omitempty"`
	BuildID      string     `json:"buildId,omitempty"`
	Type         string     `json:"type"`
	Status       string     `json:"status"`
	TargetName   string     `json:"targetName,omitempty"`
	ArtifactRef  string     `json:"artifactRef,omitempty"`
	ReasonCode   string     `json:"reasonCode,omitempty"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
}

// JobStep mirrors one developer workflow job step.
type JobStep struct {
	ID          string     `json:"id"`
	JobID       string     `json:"jobId"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Message     string     `json:"message,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

// FieldError describes one field-level validation failure within a Problem.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Problem is the RFC 9457 application/problem+json error envelope every
// appliance API error response uses.
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

// Error implements the error interface so a Problem can be returned and
// compared/inspected directly by callers.
func (p *Problem) Error() string {
	if p.Detail != "" {
		return p.Title + ": " + p.Detail
	}
	return p.Title
}
