export interface LoginResponse {
  accessToken: string;
  refreshToken: string;
  accessExpiresAt: string;
}

export interface Session {
  userId: string;
  username: string;
  authMethod: string;
  permissions: string[];
}

export interface SetupStatus {
  initialized: boolean;
}

export interface CapabilitiesResponse {
  capabilities: string[];
}

export interface Version {
  version: string;
  commit: string;
  buildTime: string;
  goVersion: string;
}

export interface Health {
  status: string;
}

export interface ApplianceIdentity {
  applianceName: string;
  dnsZone: string;
  fqdn: string;
  nodeIPv4?: string;
  canonicalOrigin?: string;
}

export interface APIToken {
  id: string;
  userId: string;
  name: string;
  scopes?: string[];
  createdAt: string;
  expiresAt: string;
  lastUsedAt?: string;
  revokedAt?: string;
}

export interface CreateTokenRequest {
  name: string;
  lifetimeSeconds?: number;
  scopes?: string[];
}

export interface CreateTokenResponse {
  token: string;
  id: string;
  userId: string;
  name: string;
  scopes?: string[];
  createdAt: string;
  expiresAt: string;
}

export interface RegistryDescriptor {
  mediaType: string;
  digest: string;
  size: number;
  artifactType?: string;
}

export interface RegistryGrant {
  id: string;
  subjectType: string;
  subjectId: string;
  pathPrefix: string;
  actions: string[];
  createdAt: string;
}

export interface CreateRegistryGrantRequest {
  subjectType: string;
  subjectId: string;
  pathPrefix: string;
  actions: string[];
}

export interface DNSRecord {
  name: string;
  fqdn: string;
  ipv4: string;
  ttl: number;
  source: string;
  owner?: string;
  createdAt: string;
  updatedAt: string;
  leaseExpiresAt?: string;
}

export interface DNSRecordsResult {
  zone: string;
  items: DNSRecord[];
}

export interface UpsertDNSRecordRequest {
  ipv4: string;
  ttl: number;
  owner?: string;
}

export interface WorkProfileRepo {
  name: string;
  enabledByDefault?: boolean;
}

export interface WorkProfile {
  name: string;
  description?: string;
  repos?: WorkProfileRepo[];
}

export interface Workspace {
  id: string;
  ownerId: string;
  name: string;
  workProfile: string;
  sourceRepoUrl: string;
  sourceRef: string;
  status: string;
  reasonCode?: string;
  errorMessage?: string;
  createdAt: string;
  updatedAt: string;
  deletedAt?: string;
}

export interface CreateWorkspaceRequest {
  name: string;
  workProfile: string;
}

export interface BuilderGitAccessStatus {
  configured: boolean;
  host?: string;
  username?: string;
  requiredHosts?: string[];
  canConfigure: boolean;
}

export interface UpdateBuilderGitAccessRequest {
  host: string;
  username: string;
  token: string;
}

export interface BuildTarget {
  name: string;
  aliases?: string[];
  description?: string;
  repo: string;
  execution: string;
  args?: string[];
  containerfilePath: string;
  imageRepository: string;
}

export interface SubmitBuildRequest {
  targetName: string;
  imageTag?: string;
}

export interface Job {
  id: string;
  ownerId: string;
  workspaceId?: string;
  buildId?: string;
  type: string;
  status: string;
  targetName?: string;
  artifactRef?: string;
  reasonCode?: string;
  errorMessage?: string;
  createdAt: string;
  updatedAt: string;
  startedAt?: string;
  completedAt?: string;
}

export interface ListResponse<T> {
  items: T[];
}
