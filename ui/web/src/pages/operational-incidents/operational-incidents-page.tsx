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

type FormState = Omit<OperationalIncident, "id"> & { id?: string };
const blank: FormState = { name: "", service: "", region: "", cidrs: [], severity: "temporary_issue", enabled: true, requires_live_check: true, allows_admin_handoff: false, customer_message: "", allowed_claims: [], forbidden_claims: [], agent_keys: [] };
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
  const isEditing = !!form.id;
  useEffect(() => { void api.load(); }, [api.load]);
  const cidrText = useMemo(() => form.cidrs.join("\n"), [form.cidrs]);
  const claimText = (values?: string[]) => (values ?? []).join("\n");
  const set = <K extends keyof FormState>(key: K, value: FormState[K]) => setForm((current) => ({ ...current, [key]: value }));
  const openNew = () => { setForm({ ...blank }); setDialogOpen(true); };
  const openEdit = (incident: OperationalIncident) => { setForm({ ...incident }); setDialogOpen(true); };
  const save = async () => {
    setSaving(true);
    try {
      const payload = { ...form, id: form.id || undefined, cidrs: form.cidrs, allowed_claims: form.allowed_claims ?? [], forbidden_claims: form.forbidden_claims ?? [], agent_keys: form.agent_keys ?? [] };
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
    <div className="mt-4 rounded-xl border bg-muted/20 p-3 text-sm text-muted-foreground">{t("help")}</div>
    <div className="mt-4">{api.loading && api.items.length === 0 ? <TableSkeleton rows={5} /> : api.items.length === 0 ? <EmptyState icon={AlertTriangle} title={t("emptyTitle")} description={t("emptyDescription")} /> : <div className="overflow-x-auto rounded-lg border"><table className="min-w-[900px] w-full text-sm"><thead className="bg-muted/50"><tr><th className="px-4 py-3 text-left">{t("columns.name")}</th><th className="px-4 py-3 text-left">{t("columns.service")}</th><th className="px-4 py-3 text-left">{t("columns.networks")}</th><th className="px-4 py-3 text-left">{t("columns.severity")}</th><th className="px-4 py-3 text-left">{t("columns.status")}</th><th className="px-4 py-3 text-right">{t("columns.actions")}</th></tr></thead><tbody>{api.items.map((incident) => <tr key={incident.id} className="border-t"><td className="px-4 py-3 font-medium">{incident.name}</td><td className="px-4 py-3">{incident.service}{incident.region ? ` · ${incident.region}` : ""}</td><td className="px-4 py-3 font-mono">{incident.cidrs.join(", ")}</td><td className="px-4 py-3"><Badge variant="outline">{t(`severity.${incident.severity}`)}</Badge></td><td className="px-4 py-3"><Badge variant={incident.enabled ? "success" : "secondary"}>{incident.enabled ? t("enabled") : t("disabled")}</Badge></td><td className="px-4 py-3"><div className="flex justify-end gap-1"><Button variant="ghost" size="icon" onClick={() => openEdit(incident)} title={t("edit")}><Pencil className="h-4 w-4" /></Button><Button variant="ghost" size="icon" onClick={() => void remove(incident)} title={t("delete")}><Trash2 className="h-4 w-4" /></Button></div></td></tr>)}</tbody></table></div>}</div>
    <Dialog open={dialogOpen} onOpenChange={setDialogOpen}><DialogContent className="max-sm:inset-0 sm:max-w-2xl"><DialogHeader><DialogTitle>{isEditing ? t("editTitle") : t("addTitle")}</DialogTitle><DialogDescription>{t("formDescription")}</DialogDescription></DialogHeader><div className="max-h-[70dvh] space-y-4 overflow-y-auto overscroll-contain pr-1"><div className="grid grid-cols-1 gap-3 sm:grid-cols-2"><label className="space-y-1 text-sm"><span>{t("fields.name")}</span><Input className="text-base md:text-sm" value={form.name} onChange={(e) => set("name", e.target.value)} /></label><label className="space-y-1 text-sm"><span>{t("fields.service")}</span><Input className="text-base md:text-sm" value={form.service} onChange={(e) => set("service", e.target.value)} /></label><label className="space-y-1 text-sm"><span>{t("fields.region")}</span><Input className="text-base md:text-sm" value={form.region ?? ""} onChange={(e) => set("region", e.target.value)} /></label><label className="space-y-1 text-sm"><span>{t("fields.severity")}</span><select className="h-9 w-full rounded-md border bg-background px-3 text-base md:text-sm" value={form.severity} onChange={(e) => set("severity", e.target.value as FormState["severity"])}><option value="temporary_issue">{t("severity.temporary_issue")}</option><option value="degraded">{t("severity.degraded")}</option><option value="permanent_outage">{t("severity.permanent_outage")}</option></select></label></div><label className="block space-y-1 text-sm"><span>{t("fields.cidrs")}</span><Textarea className="min-h-24 text-base md:text-sm" value={cidrText} onChange={(e) => set("cidrs", lines(e.target.value))} placeholder={t("fields.cidrsPlaceholder")} /></label><div className="grid grid-cols-1 gap-3 sm:grid-cols-2"><label className="space-y-1 text-sm"><span>{t("fields.startsAt")}</span><Input type="datetime-local" className="text-base md:text-sm" value={toLocalDateTimeInput(form.starts_at)} onChange={(e) => set("starts_at", e.target.value ? new Date(e.target.value).toISOString() : "")} /></label><label className="space-y-1 text-sm"><span>{t("fields.endsAt")}</span><Input type="datetime-local" className="text-base md:text-sm" value={toLocalDateTimeInput(form.ends_at)} onChange={(e) => set("ends_at", e.target.value ? new Date(e.target.value).toISOString() : "")} /></label></div><label className="block space-y-1 text-sm"><span>{t("fields.customerMessage")}</span><Textarea className="min-h-20 text-base md:text-sm" value={form.customer_message ?? ""} onChange={(e) => set("customer_message", e.target.value)} placeholder={t("fields.customerMessagePlaceholder")} /></label><label className="block space-y-1 text-sm"><span>{t("fields.allowedClaims")}</span><Textarea className="min-h-20 text-base md:text-sm" value={claimText(form.allowed_claims)} onChange={(e) => set("allowed_claims", lines(e.target.value))} /></label><label className="block space-y-1 text-sm"><span>{t("fields.forbiddenClaims")}</span><Textarea className="min-h-20 text-base md:text-sm" value={claimText(form.forbidden_claims)} onChange={(e) => set("forbidden_claims", lines(e.target.value))} /></label><label className="block space-y-1 text-sm"><span>{t("fields.agentKeys")}</span><Input className="text-base md:text-sm" value={(form.agent_keys ?? []).join(", ")} onChange={(e) => set("agent_keys", e.target.value.split(",").map((v) => v.trim()).filter(Boolean))} placeholder={t("fields.agentKeysPlaceholder")} /></label><div className="grid grid-cols-1 gap-2 sm:grid-cols-3"><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.enabled} onChange={(e) => set("enabled", e.target.checked)} />{t("fields.enabled")}</label><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.requires_live_check} onChange={(e) => set("requires_live_check", e.target.checked)} />{t("fields.requiresLive")}</label><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.allows_admin_handoff} onChange={(e) => set("allows_admin_handoff", e.target.checked)} />{t("fields.allowsHandoff")}</label></div></div><DialogFooter><Button variant="outline" onClick={() => setDialogOpen(false)}>{t("cancel")}</Button><Button disabled={saving || !form.name.trim() || !form.service.trim() || form.cidrs.length === 0} onClick={() => void save()}>{saving ? t("saving") : t("save")}</Button></DialogFooter></DialogContent></Dialog>
  </div>;
}
