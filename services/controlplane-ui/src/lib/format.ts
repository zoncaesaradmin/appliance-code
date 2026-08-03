export function formatTimestamp(value?: string): string {
  if (!value) {
    return "—";
  }
  const parsed = Date.parse(value);
  if (Number.isNaN(parsed)) {
    return value;
  }
  return new Date(parsed).toLocaleString();
}

export function capabilityBadge(capability: string): string {
  return capability.toUpperCase();
}
