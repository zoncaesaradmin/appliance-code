import { clearAuth, loadAuth, saveAuth } from "./auth";
import { MockControlPlaneClient } from "./mockClient";
import type {
  APIToken,
  ApplianceIdentity,
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
  LoginResponse,
  RegistryDescriptor,
  RegistryGrant,
  Session,
  SetupStatus,
  SubmitBuildRequest,
  UpdateBuilderGitAccessRequest,
  UpsertDNSRecordRequest,
  Version,
  WorkProfile,
  Workspace
} from "./types";

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
  getBuilderGitAccess(): Promise<BuilderGitAccessStatus>;
  updateBuilderGitAccess(request: UpdateBuilderGitAccessRequest): Promise<BuilderGitAccessStatus>;
  listBuildTargets(): Promise<BuildTarget[]>;
  submitBuild(request: SubmitBuildRequest): Promise<Job>;
  getCurrentBuildStatus(): Promise<Job | null>;
  listRepositories(): Promise<string[]>;
  listRepositoryTags(repository: string): Promise<string[]>;
  listRepositoryReferrers(repository: string, digest: string): Promise<RegistryDescriptor[]>;
  listRegistryGrants(): Promise<RegistryGrant[]>;
  createRegistryGrant(request: CreateRegistryGrantRequest): Promise<RegistryGrant>;
  deleteRegistryGrant(id: string): Promise<void>;
}

type RequestOptions = {
  method?: string;
  body?: unknown;
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
    // Server also defaults omitted/empty to "local"; normalize client-side so
    // the request always carries the resolved V1 domain.
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
    return response.items;
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
    try {
      return await this.request("/api/v1/current-workspace");
    } catch (error) {
      if (error instanceof ApiError && error.status === 404) {
        return null;
      }
      throw error;
    }
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

  async getBuilderGitAccess(): Promise<BuilderGitAccessStatus> {
    return this.request("/api/v1/builder/git-access");
  }

  async updateBuilderGitAccess(
    request: UpdateBuilderGitAccessRequest
  ): Promise<BuilderGitAccessStatus> {
    return this.request("/api/v1/builder/git-access", {
      method: "PUT",
      body: request
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
    try {
      return await this.request("/api/v1/current-workspace/build-status");
    } catch (error) {
      if (error instanceof ApiError && error.status === 404) {
        return null;
      }
      throw error;
    }
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

  private async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const {
      method = "GET",
      body,
      auth = true,
      retryAuth = true
    } = options;
    const headers = new Headers();
    headers.set("Accept", "application/json");

    if (body !== undefined) {
      headers.set("Content-Type", "application/json");
    }
    if (auth) {
      const current = loadAuth();
      if (current?.accessToken) {
        headers.set("Authorization", `Bearer ${current.accessToken}`);
      }
    }

    const response = await fetch(`${this.baseUrl}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body)
    });

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

export function createControlPlaneClient(): ControlPlaneClient {
  const mode =
    import.meta.env.MODE === "mock" ||
    import.meta.env.VITE_CONTROL_PLANE_MODE === "mock";
  if (mode) {
    return new MockControlPlaneClient();
  }
  return new RemoteControlPlaneClient(import.meta.env.VITE_CONTROL_PLANE_BASE_URL || "");
}
