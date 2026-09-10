import { describe, expect, it } from "vitest";
import { incidentDateTime, incidentGuidance, incidentStatus, validIncidentForm } from "./incident-form";
import type { OperationalIncident } from "./types";

const item: OperationalIncident = { id: "notice", name: "FPT", service: "PrivateV4", cidrs: ["103.161.96.0/24"], severity: "scheduled_outage", event_at: "2026-11-30T00:00:00Z", starts_at: "2026-09-08T00:00:00Z", enabled: true, requires_live_check: false, allows_admin_handoff: true, approved_content: "Replacement subject to Admin review" };
describe("operational notice form", () => {
  it("preserves the local event date and represents the same instant", () => {
    const local = "2026-11-30T00:00";
    const encoded = incidentDateTime(local);
    expect(encoded).toMatch(/^2026-11-30T00:00:00[+-]\d{2}:\d{2}$/);
    expect(Date.parse(encoded)).toBe(new Date(local).getTime());
    expect(incidentDateTime("")).toBe("");
    expect(incidentDateTime("invalid")).toBe("");
  });
  it("shows an advance notice as active before the event", () => {
    const now = Date.parse("2026-09-10T00:00:00Z");
    expect(incidentStatus(item, now)).toBe("active");
    expect(incidentStatus({ ...item, starts_at: "2026-09-11T00:00:00Z" }, now)).toBe("scheduled");
    expect(incidentStatus({ ...item, ends_at: "2026-09-09T00:00:00Z" }, now)).toBe("expired");
    expect(incidentStatus({ ...item, enabled: false }, now)).toBe("disabled");
  });
  it("preserves legacy guidance and gives unified content precedence", () => {
    expect(incidentGuidance({ ...item, approved_content: "", customer_message: "Notice", allowed_claims: ["Remedy", "Remedy"] })).toBe("Notice\nRemedy");
    expect(incidentGuidance({ ...item, customer_message: "Old" })).toBe(item.approved_content);
  });
  it("requires event dates and custom labels without hiding a future event", () => {
    expect(validIncidentForm(item)).toBe(true);
    expect(validIncidentForm({ ...item, event_at: "" })).toBe(false);
    expect(validIncidentForm({ ...item, severity: "custom", severity_label: "" })).toBe(false);
    expect(validIncidentForm({ ...item, severity: "custom", severity_label: "Network migration" })).toBe(true);
    expect(validIncidentForm({ ...item, ends_at: item.starts_at })).toBe(false);
  });
});
