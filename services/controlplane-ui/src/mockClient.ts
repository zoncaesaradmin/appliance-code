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
  HostMDNSApplyRequest
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
  builderGitAccess: BuilderGitAccessStatus;
  buildTargets: BuildTarget[];
  latestJob: Job | null;
  repositories: string[];
  grants: RegistryGrant[];
  licensingState: "unresolved" | "base_free" | "licensed";
  entitledCapabilities: string[];
  profiles: ApplianceProfile[];
  acknowledgedNotifications: string[];
  wifiAP: HostWifiAPStatus;
  mdns: HostMDNSStatus;
};

const mockState: MockState = {
  initialized: true,
  capabilities: ["base", "build", "artifact", "dns"],
	session: {
    userId: "mock-admin",
    username: "admin",
    domain: "local",
    authMethod: "password",
    permissions: [
      "dns.records.write",
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
      scopes: ["artifacts.read", "artifacts.write"],
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
  builderGitAccess: {
    configured: true,
    host: "git.example.internal",
    username: "builder-bot",
    requiredHosts: ["git.example.internal"],
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
  licensingState: "unresolved",
  entitledCapabilities: [],
  profiles: [
    {
      id: "core",
      displayName: "Base (core)",
      description: "Default base appliance profile.",
      builtIn: true,
      active: true,
      capabilities: ["base", "host", "workflows"]
    }
  ],
  acknowledgedNotifications: [],
  wifiAP: {
    desired: false,
    actual: "inactive",
    reason: "desired_off",
    managementAddress: "10.42.0.1",
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
      permissions: ["dns.records.write", "artifacts.write", "artifacts.read"]
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

  async getBuilderGitAccess(): Promise<BuilderGitAccessStatus> {
    return mockState.builderGitAccess;
  }

  async updateBuilderGitAccess(
    request: UpdateBuilderGitAccessRequest
  ): Promise<BuilderGitAccessStatus> {
    mockState.builderGitAccess = {
      configured: true,
      host: request.host,
      username: request.username,
      requiredHosts: [request.host],
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
    window.setTimeout(() => {
      if (!mockState.latestJob || mockState.latestJob.id !== job.id) {
        return;
      }
      mockState.latestJob = {
        ...mockState.latestJob,
        status: "succeeded",
        updatedAt: now(),
        completedAt: now()
      };
    }, 3500);
    return job;
  }

  async getCurrentBuildStatus(): Promise<Job | null> {
    return mockState.latestJob;
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
    mockState.entitledCapabilities = ["base", "host", "workflows"];
    return this.getLicensingStatus();
  }

  async importLicense(document: string): Promise<LicensingStatus> {
    void document;
    mockState.licensingState = "licensed";
    mockState.entitledCapabilities = ["base", "host", "workflows", "build", "artifact", "dns"];
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
      containerHostname: "host-agent"
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
      security: "wpa2-psk",
      supportedCapable: true,
      message: "management wifi access point is active"
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
