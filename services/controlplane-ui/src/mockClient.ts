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
  ApplianceFileEntry,
  ApplianceFileListResult,
  ApplianceFileUploadResult
} from "./types";

function now(): string {
  return new Date().toISOString();
}

function uuid(): string {
  return crypto.randomUUID();
}

type MockState = {
  initialized: boolean;
  capabilities: string[];
  session: Session | null;
  tokens: APIToken[];
  dnsRecords: DNSRecord[];
  workProfiles: WorkProfile[];
  workspaces: Workspace[];
  currentWorkspaceId: string | null;
  builderCatalog: BuilderCatalogStatus;
  builderGitAccess: BuilderGitAccessStatus;
  buildTargets: BuildTarget[];
  latestJob: Job | null;
  jobs: Job[];
  repositories: string[];
  grants: RegistryGrant[];
  files: Record<string, { sizeBytes: number; modifiedAt: string; content: Uint8Array }>;
  licensingState: "unresolved" | "base_free" | "licensed";
  entitledCapabilities: string[];
  profiles: ApplianceProfile[];
  acknowledgedNotifications: string[];
  wifiAP: HostWifiAPStatus;
  mdns: HostMDNSStatus;
};

const mockState: MockState = {
  initialized: true,
  capabilities: ["base", "files", "build", "artifact", "dns"],
	session: {
    userId: "mock-admin",
    username: "admin",
    domain: "local",
    authMethod: "password",
    permissions: [
      "dns.records.write",
      "files.write",
      "files.read",
      "artifacts.write",
      "artifacts.read",
      "licensing.read",
      "licensing.manage",
      "metadata.read",
      "metadata.manage",
      "profiles.read",
      "profiles.activate",
      "notifications.read",
      "notifications.acknowledge",
      "host.read",
      "host.write"
    ]
  },
  tokens: [
    {
      id: uuid(),
      userId: "mock-admin",
      name: "automation-bot",
      scopes: ["files.read", "files.write", "artifacts.read", "artifacts.write"],
      createdAt: now(),
      expiresAt: new Date(Date.now() + 90 * 24 * 60 * 60 * 1000).toISOString()
    }
  ],
  dnsRecords: [
    {
      name: "builder",
      fqdn: "builder.appliance.internal",
      ipv4: "10.20.0.24",
      ttl: 300,
      source: "manual",
      createdAt: now(),
      updatedAt: now()
    }
  ],
  workProfiles: [
    {
      name: "builder-default",
      description: "Default build workspace profile",
      repos: [
        { name: "controlplane", enabledByDefault: true },
        { name: "appliance-ctl" }
      ]
    },
    {
      name: "builder-dns",
      description: "DNS-focused workspace profile",
      repos: [{ name: "appliance-dns", enabledByDefault: true }]
    }
  ],
  workspaces: [
    {
      id: uuid(),
      ownerId: "mock-admin",
      name: "primary-workspace",
      workProfile: "builder-default",
      sourceRepoUrl: "https://git.example.internal/appliance-code.git",
      sourceRef: "main",
      status: "ready",
      createdAt: now(),
      updatedAt: now()
    }
  ],
  currentWorkspaceId: null,
  builderCatalog: {
    configured: true,
    contentType: "application/yaml",
    catalog: {
      workProfiles: [{ name: "builder-default", repos: [{ name: "controlplane", enabledByDefault: true }] }],
      repos: [
        {
          name: "controlplane",
          url: "https://git.example.internal/appliance-code.git",
          defaultRef: "main"
        }
      ]
    },
    document:
      "workProfiles:\n  - name: builder-default\n    repos:\n      - name: controlplane\n        enabledByDefault: true\nrepos:\n  - name: controlplane\n    url: https://git.example.internal/appliance-code.git\n    defaultRef: main\n",
    canConfigure: true
  },
  builderGitAccess: {
    configured: true,
    requiredHosts: ["git.example.internal"],
    coveredHosts: ["git.example.internal"],
    missingHosts: [],
    credentials: [
      {
        name: "git-example-internal",
        host: "git.example.internal",
        username: "builder-bot"
      }
    ],
    canConfigure: true
  },
  buildTargets: [
    {
      name: "bundle-controlplane",
      description: "Build the appliance control plane bundle",
      repo: "appliance-code",
      execution: "container",
      containerfilePath: "services/controlplane/Containerfile",
      imageRepository: "registry.local/appliance-control-plane"
    }
  ],
  latestJob: null,
  jobs: [],
  repositories: ["appliance/controlplane", "appliance/ui"],
  grants: [
    {
      id: uuid(),
      subjectType: "user",
      subjectId: "admin",
      pathPrefix: "appliance/",
      actions: ["pull", "push"],
      createdAt: now()
    }
  ],
  files: {},
  licensingState: "unresolved",
  entitledCapabilities: [],
  profiles: [
    {
      id: "core",
      displayName: "Base (core)",
      description: "Default base appliance profile.",
      builtIn: true,
      active: true,
      capabilities: ["base", "host", "files", "workflows"]
    }
  ],
  acknowledgedNotifications: [],
  wifiAP: {
    desired: false,
    actual: "inactive",
    reason: "desired_off",
    managementAddress: "10.42.0.1",
    managementHostname: "manage.ap",
    managementURL: "https://manage.ap/",
    security: "wpa2-psk",
    supportedCapable: true,
    message: "wifi access point is not desired"
  },
  mdns: {
    desired: false,
    actual: "inactive",
    reason: "desired_off",
    service: "avahi-daemon.service",
    supportedCapable: true,
    message: "mdns is not desired"
  }
};

mockState.currentWorkspaceId = mockState.workspaces[0]?.id ?? null;

export class MockControlPlaneClient {
  async getSetupStatus(): Promise<SetupStatus> {
    return { initialized: mockState.initialized };
  }

  async getCapabilities(): Promise<CapabilitiesResponse> {
    return { capabilities: mockState.capabilities };
  }

  async createFirstAdmin(): Promise<void> {
    mockState.initialized = true;
  }

	async login(username: string): Promise<LoginResponse> {
    mockState.session = {
      userId: "mock-admin",
      username,
      domain: "local",
      authMethod: "password",
      permissions: ["dns.records.write", "files.write", "files.read", "artifacts.write", "artifacts.read"]
    };
    return {
      accessToken: uuid(),
      refreshToken: uuid(),
      accessExpiresAt: new Date(Date.now() + 60 * 60 * 1000).toISOString()
    };
  }

  async refresh(): Promise<LoginResponse> {
    return {
      accessToken: uuid(),
      refreshToken: uuid(),
      accessExpiresAt: new Date(Date.now() + 60 * 60 * 1000).toISOString()
    };
  }

  async logout(): Promise<void> {
    return;
  }

  async changePassword(): Promise<void> {
    mockState.session = null;
  }

  async getSession(): Promise<Session> {
    if (!mockState.session) {
      throw new Error("No mock session");
    }
    return mockState.session;
  }

  async getVersion(): Promise<Version> {
    return {
      version: "1.0.0",
      commit: "mock",
      buildTime: now(),
      goVersion: "go1.26"
    };
  }

  async getReady(): Promise<Health> {
    return { status: "ready" };
  }

  async getIdentity(): Promise<ApplianceIdentity> {
    return {
      applianceName: "zon-appliance",
      dnsZone: "appliance.internal",
      fqdn: "zon-appliance.appliance.internal",
      nodeIPv4: "10.20.0.12",
      canonicalOrigin: "https://zon-appliance.appliance.internal"
    };
  }

  async listTokens(): Promise<APIToken[]> {
    return mockState.tokens;
  }

  async createToken(request: CreateTokenRequest): Promise<CreateTokenResponse> {
    const created: CreateTokenResponse = {
      id: uuid(),
      userId: "mock-admin",
      name: request.name,
      scopes: request.scopes ?? [],
      createdAt: now(),
      expiresAt: new Date(Date.now() + (request.lifetimeSeconds ?? 0) * 1000).toISOString(),
      token: `mock-${Math.random().toString(36).slice(2, 18)}`
    };
    mockState.tokens = [...mockState.tokens, created];
    return created;
  }

  async deleteToken(id: string): Promise<void> {
    mockState.tokens = mockState.tokens.filter((token) => token.id !== id);
  }

  async listDNSRecords(): Promise<DNSRecordsResult> {
    return {
      zone: "appliance.internal",
      items: mockState.dnsRecords
    };
  }

  async upsertDNSRecord(name: string, request: UpsertDNSRecordRequest): Promise<DNSRecord> {
    const next: DNSRecord = {
      name,
      fqdn: `${name}.appliance.internal`,
      ipv4: request.ipv4,
      ttl: request.ttl,
      source: "manual",
      createdAt: now(),
      updatedAt: now()
    };
    mockState.dnsRecords = [
      ...mockState.dnsRecords.filter((record) => record.name !== name),
      next
    ];
    return next;
  }

  async deleteDNSRecord(name: string): Promise<void> {
    mockState.dnsRecords = mockState.dnsRecords.filter((record) => record.name !== name);
  }

  async listWorkProfiles(): Promise<WorkProfile[]> {
    return mockState.workProfiles;
  }

  async listWorkspaces(): Promise<Workspace[]> {
    return mockState.workspaces;
  }

  async getCurrentWorkspace(): Promise<Workspace | null> {
    return (
      mockState.workspaces.find((workspace) => workspace.id === mockState.currentWorkspaceId) ??
      null
    );
  }

  async createWorkspace(request: CreateWorkspaceRequest): Promise<Workspace> {
    const workspace: Workspace = {
      id: uuid(),
      ownerId: "mock-admin",
      name: request.name,
      workProfile: request.workProfile,
      sourceRepoUrl: "https://git.example.internal/generated.git",
      sourceRef: "main",
      status: mockState.builderGitAccess.configured ? "ready" : "pending",
      createdAt: now(),
      updatedAt: now()
    };
    mockState.workspaces = [...mockState.workspaces, workspace];
    mockState.currentWorkspaceId = workspace.id;
    return workspace;
  }

  async setCurrentWorkspace(workspaceId: string): Promise<void> {
    mockState.currentWorkspaceId = workspaceId;
  }

  async deleteWorkspace(workspaceId: string): Promise<void> {
    mockState.workspaces = mockState.workspaces.filter((workspace) => workspace.id !== workspaceId);
    if (mockState.currentWorkspaceId === workspaceId) {
      mockState.currentWorkspaceId = mockState.workspaces[0]?.id ?? null;
    }
  }

  async getBuilderCatalog(): Promise<BuilderCatalogStatus> {
    return mockState.builderCatalog;
  }

  async putBuilderCatalog(document: string, contentType = "application/yaml"): Promise<BuilderCatalogStatus> {
    const trimmed = document.trim();
    if (!trimmed || trimmed === "{}") {
      throw new Error("catalog document must declare workProfiles and repos");
    }
    mockState.builderCatalog = {
      configured: true,
      updatedAt: now(),
      contentType,
      catalog: { uploaded: true },
      document: document.endsWith("\n") ? document : `${document}\n`,
      canConfigure: true
    };
    return mockState.builderCatalog;
  }

  async getBuilderGitAccess(): Promise<BuilderGitAccessStatus> {
    return mockState.builderGitAccess;
  }

  async updateBuilderGitAccess(
    request: UpdateBuilderGitAccessRequest
  ): Promise<BuilderGitAccessStatus> {
    const name = request.name.trim();
    if (!name) {
      throw new Error("credential name is required");
    }
    const credentials = [...(mockState.builderGitAccess.credentials || [])].filter(
      (credential) => credential.name !== name && credential.host !== request.host
    );
    credentials.push({
      name,
      host: request.host,
      username: request.username
    });
    const requiredHosts = mockState.builderGitAccess.requiredHosts || [];
    const coveredHosts = Array.from(new Set(credentials.map((credential) => credential.host))).sort();
    const missingHosts = requiredHosts.filter((host) => !coveredHosts.includes(host));
    mockState.builderGitAccess = {
      configured: requiredHosts.length === 0 ? credentials.length > 0 : missingHosts.length === 0,
      requiredHosts,
      coveredHosts,
      missingHosts,
      credentials: credentials.sort((a, b) => a.name.localeCompare(b.name)),
      canConfigure: true
    };
    return mockState.builderGitAccess;
  }

  async deleteBuilderGitAccess(name: string): Promise<BuilderGitAccessStatus> {
    const credentials = (mockState.builderGitAccess.credentials || []).filter(
      (credential) => credential.name !== name
    );
    const requiredHosts = mockState.builderGitAccess.requiredHosts || [];
    const coveredHosts = Array.from(new Set(credentials.map((credential) => credential.host))).sort();
    const missingHosts = requiredHosts.filter((host) => !coveredHosts.includes(host));
    mockState.builderGitAccess = {
      configured: requiredHosts.length === 0 ? credentials.length > 0 : missingHosts.length === 0,
      requiredHosts,
      coveredHosts,
      missingHosts,
      credentials,
      canConfigure: true
    };
    return mockState.builderGitAccess;
  }

  async listBuildTargets(): Promise<BuildTarget[]> {
    return mockState.buildTargets;
  }

  async submitBuild(request: SubmitBuildRequest): Promise<Job> {
    const current = await this.getCurrentWorkspace();
    const job: Job = {
      id: uuid(),
      ownerId: "mock-admin",
      workspaceId: current?.id,
      type: "build",
      status: "running",
      targetName: request.targetName,
      artifactRef: `${request.targetName}:${request.imageTag || "latest"}`,
      createdAt: now(),
      updatedAt: now(),
      startedAt: now()
    };
    mockState.latestJob = job;
    mockState.jobs = [job, ...mockState.jobs];
    window.setTimeout(() => {
      const completed = {
        ...job,
        status: "succeeded",
        updatedAt: now(),
        completedAt: now()
      };
      if (mockState.latestJob?.id === job.id) {
        mockState.latestJob = completed;
      }
      mockState.jobs = mockState.jobs.map((item) => (item.id === job.id ? completed : item));
    }, 3500);
    return job;
  }

  async getCurrentBuildStatus(): Promise<Job | null> {
    return mockState.latestJob;
  }

  async listJobs(): Promise<Job[]> {
    return mockState.jobs;
  }

  async getJob(jobId: string): Promise<Job> {
    const job = mockState.jobs.find((item) => item.id === jobId) || mockState.latestJob;
    if (!job || job.id !== jobId) {
      throw new Error("Job not found");
    }
    return job;
  }

  async listJobSteps(jobId: string): Promise<JobStep[]> {
    const job = await this.getJob(jobId);
    return [
      {
        id: `${job.id}-prepare`,
        jobId: job.id,
        name: "prepare",
        status: job.status === "running" ? "running" : "succeeded",
        createdAt: job.createdAt,
        startedAt: job.startedAt,
        completedAt: job.status === "running" ? undefined : job.completedAt
      },
      {
        id: `${job.id}-build`,
        jobId: job.id,
        name: "build",
        status: job.status,
        message: job.errorMessage,
        createdAt: job.createdAt,
        startedAt: job.startedAt,
        completedAt: job.completedAt
      }
    ];
  }

  async cancelJob(jobId: string): Promise<Job> {
    const job = await this.getJob(jobId);
    const cancelled = {
      ...job,
      status: "cancelled",
      updatedAt: now(),
      completedAt: now()
    };
    if (mockState.latestJob?.id === jobId) {
      mockState.latestJob = cancelled;
    }
    mockState.jobs = mockState.jobs.map((item) => (item.id === jobId ? cancelled : item));
    return cancelled;
  }

  async listRepositories(): Promise<string[]> {
    return mockState.repositories;
  }

  async listRepositoryTags(): Promise<string[]> {
    return ["latest", "2026.08.02", "stable"];
  }

  async listRepositoryReferrers(digest: string): Promise<RegistryDescriptor[]> {
    if (!digest) {
      return [];
    }
    return [
      {
        mediaType: "application/vnd.oci.image.manifest.v1+json",
        digest: "sha256:mockreferrer1",
        size: 2048,
        artifactType: "sbom/spdx"
      }
    ];
  }

  async listRegistryGrants(): Promise<RegistryGrant[]> {
    return mockState.grants;
  }

  async createRegistryGrant(
    request: CreateRegistryGrantRequest
  ): Promise<RegistryGrant> {
    const grant: RegistryGrant = {
      id: uuid(),
      subjectType: request.subjectType,
      subjectId: request.subjectId,
      pathPrefix: request.pathPrefix,
      actions: request.actions,
      createdAt: now()
    };
    mockState.grants = [...mockState.grants, grant];
    return grant;
  }

  async deleteRegistryGrant(id: string): Promise<void> {
    mockState.grants = mockState.grants.filter((grant) => grant.id !== id);
  }

  async listApplianceFiles(path = ""): Promise<ApplianceFileListResult> {
    const prefix = path.trim().replace(/^\/+|\/+$/g, "");
    const itemsByName = new Map<string, ApplianceFileEntry>();
    for (const filePath of Object.keys(mockState.files)) {
      if (prefix) {
        if (filePath === prefix) {
          continue;
        }
        if (!filePath.startsWith(prefix + "/")) {
          continue;
        }
      }
      const remainder = prefix ? filePath.slice(prefix.length + 1) : filePath;
      const [name, ...rest] = remainder.split("/");
      if (!name) {
        continue;
      }
      if (rest.length > 0) {
        if (!itemsByName.has(name)) {
          itemsByName.set(name, {
            name,
            path: prefix ? `${prefix}/${name}` : name,
            type: "directory",
            sizeBytes: 0,
            modifiedAt: now()
          });
        }
        continue;
      }
      const stored = mockState.files[filePath];
      itemsByName.set(name, {
        name,
        path: filePath,
        type: "file",
        sizeBytes: stored.sizeBytes,
        modifiedAt: stored.modifiedAt
      });
    }
    return {
      path: prefix,
      items: [...itemsByName.values()].sort((a, b) => {
        if (a.type !== b.type) {
          return a.type === "directory" ? -1 : 1;
        }
        return a.name.localeCompare(b.name);
      })
    };
  }

  async uploadApplianceFile(path: string, file: File): Promise<ApplianceFileUploadResult> {
    const cleaned = path.trim().replace(/^\/+/, "");
    const overwritten = Boolean(mockState.files[cleaned]);
    const content = new Uint8Array(await file.arrayBuffer());
    mockState.files[cleaned] = {
      sizeBytes: content.byteLength,
      modifiedAt: now(),
      content
    };
    return { path: cleaned, size: content.byteLength, overwritten };
  }

  async downloadApplianceFile(path: string): Promise<Blob> {
    const cleaned = path.trim().replace(/^\/+/, "");
    const stored = mockState.files[cleaned];
    if (!stored) {
      throw new Error("File not found");
    }
    return new Blob([Uint8Array.from(stored.content)]);
  }

  async deleteApplianceFile(path: string): Promise<void> {
    const cleaned = path.trim().replace(/^\/+|\/+$/g, "");
    const exact = mockState.files[cleaned];
    if (exact) {
      delete mockState.files[cleaned];
      return;
    }
    const prefix = cleaned + "/";
    let removed = false;
    for (const key of Object.keys(mockState.files)) {
      if (key === cleaned || key.startsWith(prefix)) {
        delete mockState.files[key];
        removed = true;
      }
    }
    if (!removed) {
      throw new Error("File not found");
    }
  }

  async getLicensingStatus(): Promise<LicensingStatus> {
    return {
      state: mockState.licensingState,
      resolved: mockState.licensingState !== "unresolved",
      profileActivationAvailable: mockState.licensingState !== "unresolved",
      entitledCapabilities: mockState.entitledCapabilities
    };
  }

  async getLicensingEntitlements(): Promise<string[]> {
    return mockState.entitledCapabilities;
  }

  async acceptBaseEntitlement(): Promise<LicensingStatus> {
    mockState.licensingState = "base_free";
    mockState.entitledCapabilities = ["base", "host", "files", "workflows"];
    return this.getLicensingStatus();
  }

  async importLicense(document: string): Promise<LicensingStatus> {
    void document;
    mockState.licensingState = "licensed";
    mockState.entitledCapabilities = ["base", "host", "files", "workflows", "build", "artifact", "dns"];
    return this.getLicensingStatus();
  }

  async getApplianceSetupState(): Promise<ApplianceSetupState> {
    return {
      activeProfile: "core",
      activeMetadataVersion: "0.0.0.0",
      licensingUnresolved: mockState.licensingState === "unresolved",
      licensingState: mockState.licensingState,
      profileActivationAvailable: mockState.licensingState !== "unresolved",
      metadataBundleManagementAvailable: true,
      blockingSetupActions: mockState.licensingState === "unresolved" ? ["licensing"] : [],
      alertNotificationIds:
        mockState.licensingState === "unresolved" &&
        !mockState.acknowledgedNotifications.includes("licensing-unresolved")
          ? ["licensing-unresolved"]
          : []
    };
  }

  async listNotifications(): Promise<NotificationItem[]> {
    if (
      mockState.licensingState !== "unresolved" ||
      mockState.acknowledgedNotifications.includes("licensing-unresolved")
    ) {
      return [];
    }
    return [
      {
        id: "licensing-unresolved",
        kind: "licensing",
        title: "Licensing is not configured",
        body: "Configure licensing to unlock entitled capabilities, or continue with the base entitlement.",
        severity: "warning",
        actionUrl: "/admin/licensing",
        createdAt: now()
      }
    ];
  }

  async acknowledgeNotification(id: string): Promise<void> {
    mockState.acknowledgedNotifications = [...mockState.acknowledgedNotifications, id];
  }

  async listApplianceCapabilities(): Promise<ApplianceCapabilityInfo[]> {
    return [
      { id: "base", dependencies: [] },
      { id: "host", dependencies: ["base"] },
      { id: "files", dependencies: ["base"] },
      { id: "workflows", dependencies: ["base"] },
      { id: "artifact", dependencies: ["base"] },
      { id: "build", dependencies: ["base", "workflows", "artifact"] },
      { id: "dns", dependencies: ["base"] }
    ];
  }

  async listApplianceProfiles(): Promise<ApplianceProfile[]> {
    return mockState.profiles;
  }

  async validateApplianceProfile(id: string): Promise<ProfileValidationResult> {
    return {
      profileId: id,
      ok: true,
      groups: [
        { name: "profile_definition", ok: true, message: "Profile definition is valid" },
        { name: "bundle_availability", ok: true, message: "Required bundle artifacts are present" },
        {
          name: "license_entitlement",
          ok: mockState.licensingState !== "unresolved",
          message: "License entitlement allows requested capabilities"
        }
      ]
    };
  }

  async activateApplianceProfile(id: string): Promise<ProfileActivationResponse> {
    const validation = await this.validateApplianceProfile(id);
    return {
      activation: {
        profileId: id,
        status: "pending_restart",
        message: `Profile ${id} accepted; restart required.`,
        requiresRestart: true
      },
      validation
    };
  }

  async getMetadataBundleStatus(): Promise<ApplianceMetadataBundleStatus> {
    return {
      softwareVersion: "0.0.0-dev",
      activeMetadataVersion: "0.0.0.0",
      canRollback: false,
      directoryName: "appliance-metadata-bundle-0.0.0.0"
    };
  }

  async validateMetadataBundle(_file: File, _signature?: string): Promise<MetadataBundleValidationResult> {
    return {
      ok: true,
      groups: [{ name: "schema", ok: true, message: "Schema is valid" }]
    };
  }

  async installMetadataBundle(file: File, signature?: string): Promise<MetadataBundleInstallResponse> {
    const validation = await this.validateMetadataBundle(file, signature);
    return {
      status: await this.getMetadataBundleStatus(),
      validation
    };
  }

  async rollbackMetadataBundle(): Promise<ApplianceMetadataBundleStatus> {
    return this.getMetadataBundleStatus();
  }

  async getHostInfo(): Promise<HostInfo> {
    return {
      hostname: "mock-host",
      operatingSystem: "Ubuntu 24.04 LTS",
      kernelVersion: "6.8.0-mock",
      architecture: "amd64",
      containerHostname: "host-agent",
      network: {
        primaryLANIPv4: "192.168.1.151",
        primaryLANSource: "ethernet",
        ethernet: {
          present: true,
          enabled: true,
          interfaces: ["enp1s0"],
          ipv4Addresses: ["192.168.1.151"]
        },
        wifi: {
          present: true,
          enabled: false,
          interfaces: ["wlp2s0"],
          ipv4Addresses: []
        },
        wifiAP: {
          present: true,
          enabled: true,
          interfaces: ["wlan0"],
          ipv4Addresses: ["10.42.0.1"]
        },
        links: [
          {
            name: "enp1s0",
            kind: "ethernet",
            state: "up",
            role: "lan",
            ipv4Addresses: ["192.168.1.151"]
          },
          {
            name: "wlan0",
            kind: "wifi",
            state: "up",
            role: "management-ap",
            ipv4Addresses: ["10.42.0.1"]
          }
        ]
      }
    };
  }

  async getHostHealth(): Promise<HostHealth> {
    return {
      status: "ok",
      hostRootAccessible: true,
      procMounted: true,
      hostnameReadable: true,
      osReleaseReadable: true
    };
  }

  async getHostWifiAP(): Promise<HostWifiAPStatus> {
    return { ...mockState.wifiAP };
  }

  async applyHostWifiAP(request: HostWifiAPApplyRequest): Promise<HostWifiAPStatus> {
    if (!request.desired) {
      mockState.wifiAP = {
        desired: false,
        actual: "inactive",
        reason: "desired_off",
        managementAddress: "10.42.0.1",
        managementHostname: "manage.ap",
        managementURL: "https://manage.ap/",
        security: "wpa2-psk",
        supportedCapable: true,
        message: "wifi access point is not desired"
      };
      return { ...mockState.wifiAP };
    }
    if (!request.psk || request.psk.length < 8) {
      mockState.wifiAP = {
        desired: true,
        actual: "inactive",
        reason: "psk_missing",
        managementAddress: "10.42.0.1",
        managementHostname: "manage.ap",
        managementURL: "https://manage.ap/",
        security: "wpa2-psk",
        supportedCapable: true,
        message: "valid WPA2 PSK is required to activate the access point",
        ssid: "mock-host-AP"
      };
      return { ...mockState.wifiAP };
    }
    mockState.wifiAP = {
      desired: true,
      actual: "active",
      ssid: "mock-host-AP",
      iface: "wlan0",
      managementAddress: "10.42.0.1",
      managementHostname: "manage.ap",
      managementURL: "https://manage.ap/",
      localDNSServing: true,
      security: "wpa2-psk",
      supportedCapable: true,
      message: "management wifi access point is active; open https://manage.ap/"
    };
    return { ...mockState.wifiAP };
  }

  async getHostMDNS(): Promise<HostMDNSStatus> {
    return { ...mockState.mdns };
  }

  async applyHostMDNS(request: HostMDNSApplyRequest): Promise<HostMDNSStatus> {
    if (!request.desired) {
      mockState.mdns = {
        desired: false,
        actual: "inactive",
        reason: "desired_off",
        service: "avahi-daemon.service",
        supportedCapable: true,
        message: "mdns is not desired"
      };
      return { ...mockState.mdns };
    }
    mockState.mdns = {
      desired: true,
      actual: "active",
      service: "avahi-daemon.service",
      supportedCapable: true,
      message: "mdns (avahi-daemon) is active"
    };
    return { ...mockState.mdns };
  }
}
