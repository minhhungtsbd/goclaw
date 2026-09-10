import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { AlertTriangle, Pencil, Plus, RefreshCw, Trash2 } from "lucide-react";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { toast } from "@/stores/use-toast-store";
import { useOperationalIncidents } from "./use-operational-incidents";
import type { OperationalIncident } from "./types";
import { incidentDateTime, incidentGuidance, incidentStatus, severityOptions, validIncidentForm } from "./incident-form";

type FormState = Omit<OperationalIncident, "id"> & { id?: string };
const blank: FormState = { name: "", service: "", region: "", cidrs: [], severity: "temporary_issue", enabled: true, requires_live_check: true, allows_admin_handoff: false, approved_content: "", allowed_claims: [], forbidden_claims: [], agent_keys: [] };
const lines = (value: string) => value.split(/\r?\n/).map((v) => v.trim()).filter(Boolean);
const toLocalDateTimeInput = (value?: string) => {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
};

export function OperationalIncidentsPage() {
  const { t } = useTranslation("operational-incidents");
  const api = useOperationalIncidents();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [form, setForm] = useState<FormState>(blank);
  const [saving, setSaving] = useState(false);
  const [networkDraft, setNetworkDraft] = useState("");
  const [forbiddenDraft, setForbiddenDraft] = useState("");
  const [now, setNow] = useState(Date.now());
  useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), 30000); return () => window.clearInterval(timer); }, []);
  const isEditing = !!form.id;
  useEffect(() => { void api.load(); }, [api.load]);
  const valid = useMemo(() => validIncidentForm({ ...form, cidrs: lines(networkDraft) }), [form, networkDraft]);
  const severityText = (value: OperationalIncident["severity"]) => ["temporary_issue", "degraded", "permanent_outage"].includes(value) ? t(`severity.${value}`) : t(`editor.${value}`);
  const set = <K extends keyof FormState>(key: K, value: FormState[K]) => setForm((current) => ({ ...current, [key]: value }));
  const openNew = () => { setForm({ ...blank }); setNetworkDraft(""); setForbiddenDraft(""); setDialogOpen(true); };
  const openEdit = (incident: OperationalIncident) => { setForm({ ...incident, approved_content: incidentGuidance(incident) }); setNetworkDraft(incident.cidrs.join("\n")); setForbiddenDraft((incident.forbidden_claims ?? []).join("\n")); setDialogOpen(true); };
  const save = async () => {
    if (!valid) { toast.error(t("editor.invalidForm")); return; }
    setSaving(true);
    try {
      const payload = { ...form, id: form.id || undefined, cidrs: lines(networkDraft), customer_message: "", allowed_claims: [], forbidden_claims: lines(forbiddenDraft), agent_keys: form.agent_keys ?? [] };
      if (isEditing && form.id) await api.update(form.id, payload);
      else await api.create(payload);
      toast.success(t("saved")); setDialogOpen(false); await api.load();
    } catch (error) { toast.error(t("saveFailed"), error instanceof Error ? error.message : ""); }
    finally { setSaving(false); }
  };
  const remove = async (incident: OperationalIncident) => {
    if (!window.confirm(t("confirmDelete", { name: incident.name }))) return;
    try { await api.remove(incident.id); toast.success(t("deleted")); await api.load(); }
    catch (error) { toast.error(t("saveFailed"), error instanceof Error ? error.message : ""); }
  };
  return <div className="p-4 pb-10 sm:p-6">
    <PageHeader title={t("title")} description={t("description")} actions={<div className="flex gap-2"><Button variant="outline" size="sm" onClick={() => void api.load()}><RefreshCw className="mr-1 h-4 w-4" />{t("refresh")}</Button><Button size="sm" onClick={openNew}><Plus className="mr-1 h-4 w-4" />{t("add")}</Button></div>} />
    <div className="mt-4 rounded-xl border bg-muted/20 p-3 text-sm text-muted-foreground">{t("editor.guidanceHelp")}</div>
    <div className="mt-4">{api.loading && api.items.length === 0 ? <TableSkeleton rows={5} /> : api.items.length === 0 ? <EmptyState icon={AlertTriangle} title={t("emptyTitle")} description={t("emptyDescription")} /> : <div className="overflow-x-auto rounded-lg border"><table className="min-w-[900px] w-full text-sm"><thead className="bg-muted/50"><tr><th className="px-4 py-3 text-left">{t("columns.name")}</th><th className="px-4 py-3 text-left">{t("columns.service")}</th><th className="px-4 py-3 text-left">{t("columns.networks")}</th><th className="px-4 py-3 text-left">{t("columns.severity")}</th><th className="px-4 py-3 text-left">{t("columns.status")}</th><th className="px-4 py-3 text-right">{t("columns.actions")}</th></tr></thead><tbody>{api.items.map((incident) => <tr key={incident.id} className="border-t"><td className="px-4 py-3 font-medium">{incident.name}</td><td className="px-4 py-3">{incident.service}{incident.region ? ` · ${incident.region}` : ""}</td><td className="px-4 py-3 font-mono">{incident.cidrs.join(", ")}</td><td className="px-4 py-3"><Badge variant="outline">{incident.severity === "custom" ? incident.severity_label : severityText(incident.severity)}</Badge></td><td className="px-4 py-3"><Badge variant={incidentStatus(incident, now) === "active" ? "success" : "secondary"}>{t(`editor.${incidentStatus(incident, now)}`)}</Badge></td><td className="px-4 py-3"><div className="flex justify-end gap-1"><Button variant="ghost" size="icon" onClick={() => openEdit(incident)} title={t("edit")}><Pencil className="h-4 w-4" /></Button><Button variant="ghost" size="icon" onClick={() => void remove(incident)} title={t("delete")}><Trash2 className="h-4 w-4" /></Button></div></td></tr>)}</tbody></table></div>}</div>
    <Dialog open={dialogOpen} onOpenChange={setDialogOpen}><DialogContent className="max-sm:inset-0 sm:max-w-2xl"><DialogHeader><DialogTitle>{isEditing ? t("editTitle") : t("addTitle")}</DialogTitle><DialogDescription>{t("formDescription")}</DialogDescription></DialogHeader><div className="max-h-[70dvh] space-y-4 overflow-y-auto overscroll-contain pr-1"><div className="grid grid-cols-1 gap-3 sm:grid-cols-2"><label className="space-y-1 text-sm"><span>{t("fields.name")}</span><Input className="text-base md:text-sm" value={form.name} onChange={(e) => set("name", e.target.value)} /></label><label className="space-y-1 text-sm"><span>{t("fields.service")}</span><Input className="text-base md:text-sm" value={form.service} onChange={(e) => set("service", e.target.value)} /></label><label className="space-y-1 text-sm"><span>{t("fields.region")}</span><Input className="text-base md:text-sm" value={form.region ?? ""} onChange={(e) => set("region", e.target.value)} /></label><label className="space-y-1 text-sm"><span>{t("fields.severity")}</span><select className="h-9 w-full rounded-md border bg-background px-3 text-base md:text-sm" value={form.severity} onChange={(e) => set("severity", e.target.value as FormState["severity"])}>{severityOptions.map((value) => <option key={value} value={value}>{severityText(value)}</option>)}</select></label>{form.severity === "custom" && <label className="space-y-1 text-sm"><span>{t("editor.customLabel")}</span><Input maxLength={160} className="text-base md:text-sm" value={form.severity_label ?? ""} onChange={(e) => set("severity_label", e.target.value)} /></label>}</div><label className="block space-y-1 text-sm"><span>{t("fields.cidrs")}</span><Textarea className="min-h-24 text-base md:text-sm" value={networkDraft} onChange={(e) => setNetworkDraft(e.target.value)} placeholder={t("fields.cidrsPlaceholder")} /></label><div className="grid grid-cols-1 gap-3 sm:grid-cols-2"><label className="space-y-1 text-sm"><span>{t("editor.noticeStart")}</span><Input type="datetime-local" className="text-base md:text-sm" value={toLocalDateTimeInput(form.starts_at)} onChange={(e) => set("starts_at", incidentDateTime(e.target.value))} /></label><label className="space-y-1 text-sm"><span>{t("editor.noticeEnd")}</span><Input type="datetime-local" className="text-base md:text-sm" value={toLocalDateTimeInput(form.ends_at)} onChange={(e) => set("ends_at", incidentDateTime(e.target.value))} /></label></div><p className="text-sm text-muted-foreground">{t("editor.dateHelp")}</p><label className="block space-y-1 text-sm"><span>{t("editor.eventAt")}</span><Input type="datetime-local" className="text-base md:text-sm" value={toLocalDateTimeInput(form.event_at)} onChange={(e) => set("event_at", incidentDateTime(e.target.value))} /></label><p className="text-sm">{t("editor.preview")}: {t(`editor.${incidentStatus(form, now)}`)}</p><label className="block space-y-1 text-sm"><span>{t("editor.approvedContent")}</span><Textarea maxLength={6000} className="min-h-40 text-base md:text-sm" value={form.approved_content ?? ""} onChange={(e) => set("approved_content", e.target.value)} /><span className="block text-muted-foreground">{t("editor.guidanceHelp")}</span>{(form.customer_message || form.allowed_claims?.length) ? <span className="block text-amber-600">{t("editor.legacyHelp")}</span> : null}</label><label className="block space-y-1 text-sm"><span>{t("fields.forbiddenClaims")}</span><Textarea className="min-h-20 text-base md:text-sm" value={forbiddenDraft} onChange={(e) => setForbiddenDraft(e.target.value)} /></label><label className="block space-y-1 text-sm"><span>{t("fields.agentKeys")}</span><Input className="text-base md:text-sm" value={(form.agent_keys ?? []).join(", ")} onChange={(e) => set("agent_keys", e.target.value.split(",").map((v) => v.trim()).filter(Boolean))} placeholder={t("fields.agentKeysPlaceholder")} /></label><div className="grid grid-cols-1 gap-2 sm:grid-cols-3"><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.enabled} onChange={(e) => set("enabled", e.target.checked)} />{t("fields.enabled")}</label><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.requires_live_check} onChange={(e) => set("requires_live_check", e.target.checked)} />{t("fields.requiresLive")}</label><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.allows_admin_handoff} onChange={(e) => set("allows_admin_handoff", e.target.checked)} />{t("fields.allowsHandoff")}</label></div><p className="text-sm text-muted-foreground">{t("editor.handoffHelp")}</p></div><DialogFooter><Button variant="outline" onClick={() => setDialogOpen(false)}>{t("cancel")}</Button><Button disabled={saving || !valid} onClick={() => void save()}>{saving ? t("saving") : t("save")}</Button></DialogFooter></DialogContent></Dialog>
  </div>;
}
