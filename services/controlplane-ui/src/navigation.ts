import type { IconName } from "./ui";

export type Mode = {
  id: string;
  label: string;
  shortLabel: string;
  icon: IconName;
  defaultPath: string;
  features: Array<{ label: string; path: string; description: string; icon: IconName }>;
};

export const MODES: Mode[] = [
  {
    id: "home",
    label: "Home",
    shortLabel: "HM",
    icon: "home",
    defaultPath: "/home",
    features: []
  },
  {
    id: "manage",
    label: "Manage",
    shortLabel: "MG",
    icon: "manage",
    defaultPath: "/manage/builder",
    features: [
      {
        label: "Builder",
        path: "/manage/builder",
        description: "Workspaces, Git access, and build submission",
        icon: "builder"
      },
      {
        label: "DNS",
        path: "/manage/dns",
        description: "Manage LAN DNS records for the appliance zone",
        icon: "dns"
      },
      {
        label: "Artifacts",
        path: "/manage/artifacts",
        description: "Review artifact catalog and registry grants",
        icon: "artifacts"
      }
    ]
  },
  {
    id: "analyze",
    label: "Analyze",
    shortLabel: "AZ",
    icon: "analyze",
    defaultPath: "/analyze/workflows",
    features: [
      {
        label: "Workflows",
        path: "/analyze/workflows",
        description: "Operational view of current builder workflow activity",
        icon: "workflows"
      }
    ]
  },
  {
    id: "admin",
    label: "Admin",
    shortLabel: "AD",
    icon: "admin",
    defaultPath: "/admin/system-status",
    features: [
      {
        label: "System Status",
        path: "/admin/system-status",
        description: "Version, readiness, and appliance identity",
        icon: "status"
      },
      {
        label: "Profiles",
        path: "/admin/profiles",
        description: "Profile management entry points for future rollout",
        icon: "profiles"
      },
      {
        label: "Licensing",
        path: "/admin/licensing",
        description: "License and entitlement placeholder",
        icon: "license"
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
