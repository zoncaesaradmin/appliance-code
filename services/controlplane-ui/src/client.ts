import { clearAuth, loadAuth, saveAuth } from "./auth";
import { MockControlPlaneClient } from "./mockClient";
import type {
  APIToken,
  ApplianceIdentity,
  BuilderCatalogStatus,
  BuilderGitAccessStatus,
  BuildTarget,
  CapabilitiesResponse,
  CreateRegistryGrantRequest,
  CreateTokenRequest,
  CreateTokenResponse,
  CreateWorkspaceRequest,
  DNSRecord,
  DNSRecordsResult,
  Health,
  Job,
  JobStep,
  LicensingStatus,
  LoginResponse,
  NotificationItem,
  ProfileActivationResponse,
  ProfileValidationResult,
  RegistryDescriptor,
  RegistryGrant,
  Session,
  SetupStatus,
  SubmitBuildRequest,
  UpdateBuilderGitAccessRequest,
  UpsertDNSRecordRequest,
  Version,
  WorkProfile,
  Workspace,
  ApplianceCapabilityInfo,
  ApplianceProfile,
  ApplianceMetadataBundleStatus,
  ApplianceSetupState,
  MetadataBundleInstallResponse,
  MetadataBundleValidationResult,
  HostInfo,
  HostHealth,
  HostWifiAPStatus,
  HostWifiAPApplyRequest,
  HostMDNSStatus,
  HostMDNSApplyRequest,
  ApplianceFileListResult,
  ApplianceFileUploadResult,
  AuditEventsResult
} from "./types";

function encodeApplianceFilePath(path: string): string {
  const trimmed = path.trim().replace(/^\/+/, "").replace(/\/+$/, "");
  if (!trimmed) {
    return "/api/v1/files";
  }
  return `/api/v1/files/${trimmed
    .split("/")
    .filter(Boolean)
    .map((segment) => encodeURIComponent(segment))
    .join("/")}`;
}

export class ApiError extends Error {
  status: number;
  detail: string;

  constructor(status: number, detail: string) {
    super(detail);
    this.status = status;
    this.detail = detail;
  }

  static async fromResponse(response: Response): Promise<ApiError> {
    let detail = response.statusText || "Request failed";
    const text = await response.text();
    if (!text.trim()) {
      return new ApiError(response.status, detail);
    }
    try {
      const body = JSON.parse(text) as { detail?: string; title?: string };
      detail = body.detail || body.title || detail;
    } catch {
      detail = text;
    }
    return new ApiError(response.status, detail);
  }
}

export interface ControlPlaneClient {
  getSetupStatus(): Promise<SetupStatus>;
  getCapabilities(): Promise<CapabilitiesResponse>;
  createFirstAdmin(username: string, password: string, displayName: string): Promise<void>;
  login(username: string, password: string, domain?: string): Promise<LoginResponse>;
  refresh(): Promise<LoginResponse>;
  logout(): Promise<void>;
  changePassword(currentPassword: string, newPassword: string): Promise<void>;
  getSession(): Promise<Session>;
  getVersion(): Promise<Version>;
  getReady(): Promise<Health>;
  getIdentity(): Promise<ApplianceIdentity>;
  listTokens(): Promise<APIToken[]>;
  createToken(request: CreateTokenRequest): Promise<CreateTokenResponse>;
  deleteToken(id: string): Promise<void>;
  listDNSRecords(): Promise<DNSRecordsResult>;
  upsertDNSRecord(name: string, request: UpsertDNSRecordRequest): Promise<DNSRecord>;
  deleteDNSRecord(name: string): Promise<void>;
  listWorkProfiles(): Promise<WorkProfile[]>;
  listWorkspaces(): Promise<Workspace[]>;
  getCurrentWorkspace(): Promise<Workspace | null>;
  createWorkspace(request: CreateWorkspaceRequest): Promise<Workspace>;
  setCurrentWorkspace(workspaceId: string): Promise<void>;
  deleteWorkspace(workspaceId: string): Promise<void>;
  getBuilderCatalog(): Promise<BuilderCatalogStatus>;
  putBuilderCatalog(document: string, contentType?: string): Promise<BuilderCatalogStatus>;
  getBuilderGitAccess(): Promise<BuilderGitAccessStatus>;
  updateBuilderGitAccess(request: UpdateBuilderGitAccessRequest): Promise<BuilderGitAccessStatus>;
  deleteBuilderGitAccess(name: string): Promise<BuilderGitAccessStatus>;
  listBuildTargets(): Promise<BuildTarget[]>;
  submitBuild(request: SubmitBuildRequest): Promise<Job>;
  getCurrentBuildStatus(): Promise<Job | null>;
  listJobs(): Promise<Job[]>;
  getJob(jobId: string): Promise<Job>;
  listJobSteps(jobId: string): Promise<JobStep[]>;
  cancelJob(jobId: string): Promise<Job>;
  listRepositories(): Promise<string[]>;
  listRepositoryTags(repository: string): Promise<string[]>;
  listRepositoryReferrers(repository: string, digest: string): Promise<RegistryDescriptor[]>;
  listRegistryGrants(): Promise<RegistryGrant[]>;
  createRegistryGrant(request: CreateRegistryGrantRequest): Promise<RegistryGrant>;
  deleteRegistryGrant(id: string): Promise<void>;
  listApplianceFiles(path?: string): Promise<ApplianceFileListResult>;
  uploadApplianceFile(path: string, file: File): Promise<ApplianceFileUploadResult>;
  downloadApplianceFile(path: string): Promise<Blob>;
  deleteApplianceFile(path: string): Promise<void>;
  getLicensingStatus(): Promise<LicensingStatus>;
  getLicensingEntitlements(): Promise<string[]>;
  acceptBaseEntitlement(): Promise<LicensingStatus>;
  importLicense(document: string): Promise<LicensingStatus>;
  getApplianceSetupState(): Promise<ApplianceSetupState>;
  listNotifications(): Promise<NotificationItem[]>;
  acknowledgeNotification(id: string): Promise<void>;
  listApplianceCapabilities(): Promise<ApplianceCapabilityInfo[]>;
  listApplianceProfiles(): Promise<ApplianceProfile[]>;
  validateApplianceProfile(id: string): Promise<ProfileValidationResult>;
  activateApplianceProfile(id: string): Promise<ProfileActivationResponse>;
  getMetadataBundleStatus(): Promise<ApplianceMetadataBundleStatus>;
  validateMetadataBundle(file: File, signature?: string): Promise<MetadataBundleValidationResult>;
  installMetadataBundle(file: File, signature?: string): Promise<MetadataBundleInstallResponse>;
  rollbackMetadataBundle(): Promise<ApplianceMetadataBundleStatus>;
  getHostInfo(): Promise<HostInfo>;
  getHostHealth(): Promise<HostHealth>;
  getHostWifiAP(): Promise<HostWifiAPStatus>;
  applyHostWifiAP(request: HostWifiAPApplyRequest): Promise<HostWifiAPStatus>;
  getHostMDNS(): Promise<HostMDNSStatus>;
  applyHostMDNS(request: HostMDNSApplyRequest): Promise<HostMDNSStatus>;
  listAuditEvents(params?: { limit?: number; cursor?: string }): Promise<AuditEventsResult>;
}

type RequestOptions = {
  method?: string;
  body?: unknown;
  rawBody?: string;
  contentType?: string;
  auth?: boolean;
  retryAuth?: boolean;
};

export class RemoteControlPlaneClient implements ControlPlaneClient {
  private readonly baseUrl: string;

  constructor(baseUrl: string) {
    this.baseUrl = baseUrl.replace(/\/$/, "");
  }

  async getSetupStatus(): Promise<SetupStatus> {
    return this.request("/api/v1/setup/status");
  }

  async getCapabilities(): Promise<CapabilitiesResponse> {
    return this.request("/api/v1/capabilities");
  }

  async createFirstAdmin(username: string, password: string, displayName: string): Promise<void> {
    await this.request("/api/v1/setup/first-admin", {
      method: "POST",
      body: { username, password, displayName }
    });
  }

  async login(username: string, password: string, domain?: string): Promise<LoginResponse> {
    // Login UI omits domain; empty/omitted values become local here and on the server.
    const resolvedDomain = (domain ?? "").trim().toLowerCase() || "local";
    return this.request("/api/v1/auth/login", {
      method: "POST",
      body: { username, password, domain: resolvedDomain },
      auth: false,
      retryAuth: false
    });
  }

  async refresh(): Promise<LoginResponse> {
    const current = loadAuth();
    if (!current?.refreshToken) {
      throw new ApiError(401, "Refresh token is not available");
    }
    const refreshed = await this.request<LoginResponse>("/api/v1/auth/refresh", {
      method: "POST",
      body: { refreshToken: current.refreshToken },
      auth: false,
      retryAuth: false
    });
    saveAuth(refreshed);
    return refreshed;
  }

  async logout(): Promise<void> {
    try {
      await this.request("/api/v1/auth/logout", { method: "POST" });
    } finally {
      clearAuth();
    }
  }

  async changePassword(currentPassword: string, newPassword: string): Promise<void> {
    await this.request("/api/v1/auth/password", {
      method: "POST",
      body: { currentPassword, newPassword },
      retryAuth: false
    });
    clearAuth();
  }

  async getSession(): Promise<Session> {
    return this.request("/api/v1/auth/session");
  }

  async getVersion(): Promise<Version> {
    return this.request("/version", { auth: false, retryAuth: false });
  }

  async getReady(): Promise<Health> {
    return this.request("/health/ready", { auth: false, retryAuth: false });
  }

  async getIdentity(): Promise<ApplianceIdentity> {
    return this.request("/api/v1/appliance/identity");
  }

  async listTokens(): Promise<APIToken[]> {
    const response = await this.request<{ items: APIToken[] }>("/api/v1/tokens");
    return (response.items || []).filter((token) => !token.revokedAt);
  }

  async createToken(request: CreateTokenRequest): Promise<CreateTokenResponse> {
    return this.request("/api/v1/tokens", { method: "POST", body: request });
  }

  async deleteToken(id: string): Promise<void> {
    await this.request(`/api/v1/tokens/${encodeURIComponent(id)}`, { method: "DELETE" });
  }

  async listDNSRecords(): Promise<DNSRecordsResult> {
    return this.request("/api/v1/dns/records");
  }

  async upsertDNSRecord(name: string, request: UpsertDNSRecordRequest): Promise<DNSRecord> {
    return this.request(`/api/v1/dns/records/${encodeURIComponent(name)}`, {
      method: "PUT",
      body: request
    });
  }

  async deleteDNSRecord(name: string): Promise<void> {
    await this.request(`/api/v1/dns/records/${encodeURIComponent(name)}`, { method: "DELETE" });
  }

  async listWorkProfiles(): Promise<WorkProfile[]> {
    const response = await this.request<{ items: WorkProfile[] }>("/api/v1/work-profiles");
    return response.items;
  }

  async listWorkspaces(): Promise<Workspace[]> {
    const response = await this.request<{ items: Workspace[] }>("/api/v1/workspaces");
    return response.items;
  }

  async getCurrentWorkspace(): Promise<Workspace | null> {
    return this.request("/api/v1/current-workspace");
  }

  async createWorkspace(request: CreateWorkspaceRequest): Promise<Workspace> {
    return this.request("/api/v1/workspaces", { method: "POST", body: request });
  }

  async setCurrentWorkspace(workspaceId: string): Promise<void> {
    await this.request("/api/v1/current-workspace", {
      method: "POST",
      body: { workspaceId }
    });
  }

  async deleteWorkspace(workspaceId: string): Promise<void> {
    await this.request(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}`, {
      method: "DELETE"
    });
  }

  async getBuilderCatalog(): Promise<BuilderCatalogStatus> {
    return this.request("/api/v1/builder/catalog");
  }

  async putBuilderCatalog(document: string, contentType = "application/yaml"): Promise<BuilderCatalogStatus> {
    return this.request("/api/v1/builder/catalog", {
      method: "PUT",
      rawBody: document,
      contentType
    });
  }

  async getBuilderGitAccess(): Promise<BuilderGitAccessStatus> {
    return this.request("/api/v1/builder/git-access");
  }

  async updateBuilderGitAccess(
    request: UpdateBuilderGitAccessRequest
  ): Promise<BuilderGitAccessStatus> {
    const name = request.name.trim();
    return this.request(`/api/v1/builder/git-access/${encodeURIComponent(name)}`, {
      method: "PUT",
      body: {
        host: request.host,
        username: request.username,
        token: request.token
      }
    });
  }

  async deleteBuilderGitAccess(name: string): Promise<BuilderGitAccessStatus> {
    return this.request(`/api/v1/builder/git-access/${encodeURIComponent(name)}`, {
      method: "DELETE"
    });
  }

  async listBuildTargets(): Promise<BuildTarget[]> {
    const response = await this.request<{ items: BuildTarget[] }>(
      "/api/v1/current-workspace/build-targets"
    );
    return response.items;
  }

  async submitBuild(request: SubmitBuildRequest): Promise<Job> {
    return this.request("/api/v1/current-workspace/builds", {
      method: "POST",
      body: request
    });
  }

  async getCurrentBuildStatus(): Promise<Job | null> {
    return this.request("/api/v1/current-workspace/build-status");
  }

  async listJobs(): Promise<Job[]> {
    const response = await this.request<{ items: Job[] }>("/api/v1/jobs");
    return response.items || [];
  }

  async getJob(jobId: string): Promise<Job> {
    return this.request(`/api/v1/jobs/${encodeURIComponent(jobId)}`);
  }

  async listJobSteps(jobId: string): Promise<JobStep[]> {
    const response = await this.request<{ items: JobStep[] }>(
      `/api/v1/jobs/${encodeURIComponent(jobId)}/steps`
    );
    return response.items || [];
  }

  async cancelJob(jobId: string): Promise<Job> {
    return this.request(`/api/v1/jobs/${encodeURIComponent(jobId)}/cancel`, {
      method: "POST"
    });
  }

  async listRepositories(): Promise<string[]> {
    const response = await this.request<{ items: string[] }>("/api/v1/registry/repositories");
    return response.items;
  }

  async listRepositoryTags(repository: string): Promise<string[]> {
    const response = await this.request<{ items: string[] }>(
      `/api/v1/registry/repositories/${repository
        .split("/")
        .map(encodeURIComponent)
        .join("/")}/tags`
    );
    return response.items;
  }

  async listRepositoryReferrers(
    repository: string,
    digest: string
  ): Promise<RegistryDescriptor[]> {
    const response = await this.request<{ items: RegistryDescriptor[] }>(
      `/api/v1/registry/repositories/${repository
        .split("/")
        .map(encodeURIComponent)
        .join("/")}/referrers?digest=${encodeURIComponent(digest)}`
    );
    return response.items;
  }

  async listRegistryGrants(): Promise<RegistryGrant[]> {
    const response = await this.request<{ items: RegistryGrant[] }>("/api/v1/registry/grants");
    return response.items;
  }

  async createRegistryGrant(
    request: CreateRegistryGrantRequest
  ): Promise<RegistryGrant> {
    return this.request("/api/v1/registry/grants", { method: "POST", body: request });
  }

  async deleteRegistryGrant(id: string): Promise<void> {
    await this.request(`/api/v1/registry/grants/${encodeURIComponent(id)}`, {
      method: "DELETE"
    });
  }

  async listApplianceFiles(path = ""): Promise<ApplianceFileListResult> {
    return this.request(encodeApplianceFilePath(path));
  }

  async uploadApplianceFile(path: string, file: File): Promise<ApplianceFileUploadResult> {
    const auth = loadAuth();
    const headers: Record<string, string> = {
      Accept: "application/json",
      "Content-Type": "application/octet-stream"
    };
    if (auth?.accessToken) {
      headers.Authorization = `Bearer ${auth.accessToken}`;
    }
    const response = await fetch(`${this.baseUrl}${encodeApplianceFilePath(path)}`, {
      method: "POST",
      headers,
      body: file
    });
    if (!response.ok) {
      throw await ApiError.fromResponse(response);
    }
    return (await response.json()) as ApplianceFileUploadResult;
  }

  async downloadApplianceFile(path: string): Promise<Blob> {
    const auth = loadAuth();
    const headers: Record<string, string> = {
      Accept: "application/octet-stream"
    };
    if (auth?.accessToken) {
      headers.Authorization = `Bearer ${auth.accessToken}`;
    }
    const response = await fetch(`${this.baseUrl}${encodeApplianceFilePath(path)}`, {
      method: "GET",
      headers
    });
    if (!response.ok) {
      throw await ApiError.fromResponse(response);
    }
    return response.blob();
  }

  async deleteApplianceFile(path: string): Promise<void> {
    await this.request(encodeApplianceFilePath(path), { method: "DELETE" });
  }

  async getLicensingStatus(): Promise<LicensingStatus> {
    return this.request("/api/v1/licensing/status");
  }

  async getLicensingEntitlements(): Promise<string[]> {
    const response = await this.request<{ capabilities: string[] }>("/api/v1/licensing/entitlements");
    return response.capabilities;
  }

  async acceptBaseEntitlement(): Promise<LicensingStatus> {
    return this.request("/api/v1/licensing/base-entitlement/accept", { method: "POST" });
  }

  async importLicense(document: string): Promise<LicensingStatus> {
    return this.request("/api/v1/licensing/license", {
      method: "PUT",
      body: { document }
    });
  }

  async getApplianceSetupState(): Promise<ApplianceSetupState> {
    return this.request("/api/v1/appliance/setup-state");
  }

  async listNotifications(): Promise<NotificationItem[]> {
    const response = await this.request<{ items: NotificationItem[] }>("/api/v1/notifications");
    return response.items;
  }

  async acknowledgeNotification(id: string): Promise<void> {
    await this.request(`/api/v1/notifications/${encodeURIComponent(id)}/acknowledge`, {
      method: "POST"
    });
  }

  async listApplianceCapabilities(): Promise<ApplianceCapabilityInfo[]> {
    const response = await this.request<{ items: ApplianceCapabilityInfo[] }>(
      "/api/v1/appliance/capabilities"
    );
    return response.items;
  }

  async listApplianceProfiles(): Promise<ApplianceProfile[]> {
    const response = await this.request<{ items: ApplianceProfile[] }>("/api/v1/appliance/profiles");
    return response.items;
  }

  async validateApplianceProfile(id: string): Promise<ProfileValidationResult> {
    return this.request(`/api/v1/appliance/profiles/${encodeURIComponent(id)}/validate`, {
      method: "POST"
    });
  }

  async activateApplianceProfile(id: string): Promise<ProfileActivationResponse> {
    return this.request(`/api/v1/appliance/profiles/${encodeURIComponent(id)}/activate`, {
      method: "POST"
    });
  }

  async getMetadataBundleStatus(): Promise<ApplianceMetadataBundleStatus> {
    return this.request("/api/v1/appliance/metadata-bundle");
  }

  async validateMetadataBundle(file: File, signature = "offline-dev"): Promise<MetadataBundleValidationResult> {
    return this.uploadMetadataBundle("/api/v1/appliance/metadata-bundle/validate", file, signature);
  }

  async installMetadataBundle(file: File, signature = "offline-dev"): Promise<MetadataBundleInstallResponse> {
    return this.uploadMetadataBundle("/api/v1/appliance/metadata-bundle/install", file, signature);
  }

  async rollbackMetadataBundle(): Promise<ApplianceMetadataBundleStatus> {
    return this.request("/api/v1/appliance/metadata-bundle/rollback", { method: "POST" });
  }

  async getHostInfo(): Promise<HostInfo> {
    return this.request("/api/v1/host/info");
  }

  async getHostHealth(): Promise<HostHealth> {
    return this.request("/api/v1/host/health");
  }

  async getHostWifiAP(): Promise<HostWifiAPStatus> {
    return this.request("/api/v1/host/wifi-ap");
  }

  async applyHostWifiAP(request: HostWifiAPApplyRequest): Promise<HostWifiAPStatus> {
    return this.request("/api/v1/host/wifi-ap", { method: "PUT", body: request });
  }

  async getHostMDNS(): Promise<HostMDNSStatus> {
    return this.request("/api/v1/host/mdns");
  }

  async applyHostMDNS(request: HostMDNSApplyRequest): Promise<HostMDNSStatus> {
    return this.request("/api/v1/host/mdns", { method: "PUT", body: request });
  }

  async listAuditEvents(params?: { limit?: number; cursor?: string }): Promise<AuditEventsResult> {
    const query = new URLSearchParams();
    query.set("limit", String(params?.limit ?? 10));
    if (params?.cursor) {
      query.set("cursor", params.cursor);
    }
    return this.request(`/api/v1/audit/events?${query.toString()}`);
  }

  private async uploadMetadataBundle<T>(path: string, file: File, signature: string): Promise<T> {
    const form = new FormData();
    form.append("archive", file);
    form.append("signature", signature);
    const auth = loadAuth();
    const headers: Record<string, string> = {};
    if (auth?.accessToken) {
      headers.Authorization = `Bearer ${auth.accessToken}`;
    }
    const response = await fetch(`${this.baseUrl}${path}`, {
      method: "POST",
      headers,
      body: form
    });
    if (!response.ok) {
      throw await ApiError.fromResponse(response);
    }
    return (await response.json()) as T;
  }

  private async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const {
      method = "GET",
      body,
      rawBody,
      contentType,
      auth = true,
      retryAuth = true
    } = options;
    const headers = new Headers();
    headers.set("Accept", "application/json");

    if (rawBody !== undefined) {
      headers.set("Content-Type", contentType || "application/yaml");
    } else if (body !== undefined) {
      headers.set("Content-Type", "application/json");
    }
    if (auth) {
      const current = loadAuth();
      if (current?.accessToken) {
        headers.set("Authorization", `Bearer ${current.accessToken}`);
      }
    }

    let response: Response;
    try {
      const init: RequestInit = {
        method,
        headers,
        body:
          rawBody !== undefined
            ? rawBody
            : body === undefined
              ? undefined
              : JSON.stringify(body)
      };
      if (typeof AbortSignal !== "undefined" && typeof AbortSignal.timeout === "function") {
        init.signal = AbortSignal.timeout(120_000);
      }
      response = await fetch(`${this.baseUrl}${path}`, init);
    } catch (error) {
      throw networkRequestError(error);
    }

    if (response.status === 401 && auth && retryAuth && loadAuth()?.refreshToken) {
      await this.refresh();
      return this.request(path, { ...options, retryAuth: false });
    }
    if (!response.ok) {
      throw await ApiError.fromResponse(response);
    }

    const text = await response.text();
    if (!text.trim()) {
      return undefined as T;
    }
    return JSON.parse(text) as T;
  }
}

function networkRequestError(error: unknown): Error {
  if (error instanceof DOMException && error.name === "TimeoutError") {
    return new ApiError(0, "The appliance took too long to respond. Please try again.");
  }
  const message = error instanceof Error ? error.message : String(error);
  if (/networkerror|failed to fetch|load failed|aborted/i.test(message)) {
    return new ApiError(
      0,
      "Could not reach the appliance API. Check network connectivity and that the control plane is healthy, then try again."
    );
  }
  return error instanceof Error ? error : new Error(message);
}

export function createControlPlaneClient(): ControlPlaneClient {
  const mode =
    import.meta.env.MODE === "mock" ||
    import.meta.env.VITE_CONTROL_PLANE_MODE === "mock";
  if (mode) {
    return new MockControlPlaneClient();
  }
  return new RemoteControlPlaneClient(import.meta.env.VITE_CONTROL_PLANE_BASE_URL || "");
}
