# Operational notices

Manage tenant-scoped notices at `/operational-incidents`. Runtime context is loaded from the tenant's `cloudmini.operational_incidents` system configuration. This JSON-backed store is shared by PostgreSQL and SQLite; no SQL schema migration is required for these fields.

## Dates and scope

- `starts_at` / `ends_at`: notice publication window, inclusive start and exclusive end. Empty means no boundary. `enabled=false` always hides the notice.
- `event_at`: when the event is scheduled, independent of publication. Required for `scheduled_outage`. Keep the local RFC3339 offset when the calendar date matters.
- CIDRs and agent keys retain their existing scope. The runtime does not infer affected networks from customer claims or custom labels. A future event can be announced today by publishing the notice today.
- Once the scheduled date has passed, the schedule alone does not prove an outage. The agent describes the original schedule and asks for confirmation of the actual outcome. An operator can update the notice to the confirmed status.

The editor displays Active, Not yet effective, Expired or Disabled. The event date does not change that publication status.

## Content and severity

`approved_content` is the unified source for facts, conditions, and approved support options. Agents paraphrase it naturally; it is not a script. The editor merges legacy `customer_message` and `allowed_claims` on opening an old record, so operators can review duplicates and contradictions before saving. A nonempty `approved_content` supersedes and clears those legacy fields on save. Old clients/records without it remain readable.

Supported severity values: `notice`, `maintenance`, `scheduled_outage`, `temporary_issue`, `degraded`, `permanent_outage`, `resolved`, `custom`. A custom severity requires `severity_label` (up to 160 bytes). This label is descriptive and does not grant permissions or imply a permanent outage. `forbidden_claims` remains separate.

Verified service validity and connectivity are separate facts. A current LIVE result does not suppress a scheduled shutdown, general notice, maintenance or custom notice. Matched incident support options take precedence over normal change/refund fees.

For an explicit handling request covering verified IPs, matching notices must allow Admin handoff before the pipeline requires it. General policy questions do not trigger a ticket. A verified pending ticket covering the request prevents automatic duplicate routing. Actual tool success remains mandatory before claiming that a ticket was created or sent.

The response guard allows paraphrases and checks known safety constraints (service facts, outage tense/date, free-replacement/refund wording, forbidden phrases and ticket truth). It is not a general semantic proof for arbitrary operator prose. A failed check permits one rewrite retry; if that fails, the customer receives verified service facts and a neutral review message, never raw internal notes or invented completion.

## API and rollout

CRUD remains at `/v1/cloudmini/operational-incidents` and `/v1/cloudmini/operational-incidents/{id}`, under tenant-admin authorization. Additive fields are documented in OpenAPI. Deploy the updated server before saving new severity values or fields; older servers reject unknown fields/severities.

For the FPT advance notice previously configured with `starts_at=2026-11-30T00:00:00Z`, after deploying:

1. Preserve the existing ID, affected CIDRs, agent scope and handoff permission.
2. Set `starts_at` to the notice publication date (2026-09-08) and `event_at` to the existing November 30 timestamp.
3. Choose `scheduled_outage` and merge the content, correcting “đã ngưng” to “sẽ ngưng”. Preserve free replacement or refund review, without promising completed work.
4. Verify the agent's active managed skill contains the updated operational-notice rules. Bundled source files do not overwrite a customized managed skill automatically.

Surface parity: Web UI and HTTP contract updated. Gateway pipeline and the shared PostgreSQL/SQLite JSON store use the new fields. CLI/runtime package N/A: no dedicated incident commands or response parser in this repository. Desktop UI N/A: no operational-incidents screen in the desktop frontend; the shared SQLite backend supports the same representation.
