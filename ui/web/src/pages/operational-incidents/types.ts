export interface OperationalIncident {
  id: string;
  name: string;
  service: string;
  region?: string;
  cidrs: string[];
  severity: "temporary_issue" | "degraded" | "permanent_outage";
  starts_at?: string;
  ends_at?: string;
  enabled: boolean;
  requires_live_check: boolean;
  allows_admin_handoff: boolean;
  customer_message?: string;
  allowed_claims?: string[];
  forbidden_claims?: string[];
  agent_keys?: string[];
  created_at?: string;
  updated_at?: string;
}
