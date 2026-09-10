import type { OperationalIncident } from "./types";

export const severityOptions = ["notice", "maintenance", "scheduled_outage", "temporary_issue", "degraded", "permanent_outage", "resolved", "custom"] as const;

export function incidentDateTime(value: string): string {
  if (!value) return "";
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return "";
  const minutes = -date.getTimezoneOffset();
  const pad = (part: number) => String(part).padStart(2, "0");
  const offset = `${minutes >= 0 ? "+" : "-"}${pad(Math.floor(Math.abs(minutes) / 60))}:${pad(Math.abs(minutes) % 60)}`;
  return `${value.length === 16 ? value + ":00" : value}${offset}`;
}

export function incidentGuidance(item: OperationalIncident): string {
  if (item.approved_content?.trim()) return item.approved_content.trim();
  const message = item.customer_message?.trim() ?? "";
  return [...new Set([message, ...(item.allowed_claims ?? []).filter((claim) => !message.includes(claim.trim()))].map((s) => s.trim()).filter(Boolean))].join("\n");
}

export function incidentStatus(item: Pick<OperationalIncident, "enabled" | "starts_at" | "ends_at">, now = Date.now()) {
  if (!item.enabled) return "disabled";
  if (item.ends_at && Date.parse(item.ends_at) <= now) return "expired";
  if (item.starts_at && Date.parse(item.starts_at) > now) return "scheduled";
  return "active";
}

export function validIncidentForm(item: Omit<OperationalIncident, "id">) {
  if (!item.name.trim() || !item.service.trim() || !item.cidrs.length || !item.approved_content?.trim()) return false;
  if (item.severity === "custom" && !item.severity_label?.trim()) return false;
  if (item.severity === "scheduled_outage" && !item.event_at) return false;
  for (const date of [item.starts_at, item.ends_at, item.event_at]) {
    if (date && !Number.isFinite(Date.parse(date))) return false;
  }
  return !(item.starts_at && item.ends_at && Date.parse(item.ends_at) <= Date.parse(item.starts_at));
}
