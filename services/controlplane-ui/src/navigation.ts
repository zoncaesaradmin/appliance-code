export type Mode = {
  id: string;
  label: string;
  shortLabel: string;
  icon: string;
  defaultPath: string;
  features: Array<{ label: string; path: string; description: string }>;
};

export const MODES: Mode[] = [
  {
    id: "home",
    label: "Home",
    shortLabel: "HM",
    icon: "H",
    defaultPath: "/home",
    features: []
  },
  {
    id: "manage",
    label: "Manage",
    shortLabel: "MG",
    icon: "M",
    defaultPath: "/manage/builder",
    features: [
      {
        label: "Builder",
        path: "/manage/builder",
        description: "Workspaces, Git access, and build submission"
      },
      {
        label: "DNS",
        path: "/manage/dns",
        description: "Manage LAN DNS records for the appliance zone"
      },
      {
        label: "Artifacts",
        path: "/manage/artifacts",
        description: "Review artifact catalog and registry grants"
      }
    ]
  },
  {
    id: "analyze",
    label: "Analyze",
    shortLabel: "AZ",
    icon: "A",
    defaultPath: "/analyze/workflows",
    features: [
      {
        label: "Workflows",
        path: "/analyze/workflows",
        description: "Operational view of current builder workflow activity"
      }
    ]
  },
  {
    id: "admin",
    label: "Admin",
    shortLabel: "AD",
    icon: "D",
    defaultPath: "/admin/system-status",
    features: [
      {
        label: "System Status",
        path: "/admin/system-status",
        description: "Version, readiness, and appliance identity"
      },
      {
        label: "Profiles",
        path: "/admin/profiles",
        description: "Profile management entry points for future rollout"
      },
      {
        label: "Licensing",
        path: "/admin/licensing",
        description: "License and entitlement placeholder"
      }
    ]
  }
];

export function currentMode(pathname: string): Mode {
  return (
    MODES.find((mode) => pathname === mode.defaultPath || pathname.startsWith(`/${mode.id}/`)) ??
    MODES[0]
  );
}

export function modeUsesFeatureSelector(mode: Mode): boolean {
  return mode.features.length > 0;
}
