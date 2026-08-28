import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, ClipboardCheck, Eye, FilePenLine, RefreshCw, Search, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { formatDate } from "@/lib/format";
import { toast } from "@/stores/use-toast-store";
import { ticketReference, type AdminHandoff, type AdminHandoffEvent } from "./types";
import { useAdminHandoffs } from "./use-admin-handoffs";

const PAGE_SIZE = 25;

export function AdminHandoffsPage() {
  const { t } = useTranslation("admin-handoffs");
  const api = useAdminHandoffs();
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("pending");
  const [priority, setPriority] = useState("");
  const [offset, setOffset] = useState(0);
  const [selected, setSelected] = useState<AdminHandoff | null>(null);
  const [events, setEvents] = useState<AdminHandoffEvent[]>([]);
  const [manualTarget, setManualTarget] = useState<AdminHandoff | null>(null);
  const [manualContent, setManualContent] = useState("");
  const [busy, setBusy] = useState<string | null>(null);

  const filters = { search, status, priority, offset, limit: PAGE_SIZE };
  useEffect(() => {
    const timer = setTimeout(() => api.load(filters), 350);
    return () => clearTimeout(timer);
  }, [search, status, priority, offset, api.load]);

  const refresh = () => api.load(filters);
  const run = async (id: string, action: () => Promise<void>, message: string) => {
    setBusy(id);
    try {
      await action();
      toast.success(message);
      setSelected(null);
      await refresh();
      return true;
    } catch (error) {
      toast.error(t("actionFailed"), error instanceof Error ? error.message : "");
      return false;
    } finally {
      setBusy(null);
    }
  };
  const openDetail = async (handoff: AdminHandoff) => {
    setSelected(handoff);
    try {
      const response = await api.detail(handoff.id);
      setSelected(response.handoff);
      setEvents(response.events ?? []);
    } catch {
      setEvents([]);
    }
  };

  return (
    <div className="p-4 pb-10 sm:p-6">
      <PageHeader title={t("title")} description={t("description")} actions={<Button variant="outline" size="sm" onClick={refresh}><RefreshCw className="mr-1 h-4 w-4" />{t("refresh")}</Button>} />
      <div className="mt-4 grid grid-cols-1 gap-3 rounded-xl border bg-muted/20 p-3 sm:grid-cols-[minmax(240px,1fr)_180px_180px]">
        <div className="relative"><Search className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" /><Input className="pl-9 text-base md:text-sm" value={search} onChange={(e) => { setSearch(e.target.value); setOffset(0); }} placeholder={t("searchPlaceholder")} /></div>
        <select className="h-9 rounded-md border bg-background px-3 text-base md:text-sm" value={status} onChange={(e) => { setStatus(e.target.value); setOffset(0); }}><option value="">{t("allStatuses")}</option><option value="pending">{t("statuses.pending")}</option><option value="completed">{t("statuses.completed")}</option><option value="delivery_failed">{t("statuses.delivery_failed")}</option><option value="dismissed">{t("statuses.dismissed")}</option></select>
        <select className="h-9 rounded-md border bg-background px-3 text-base md:text-sm" value={priority} onChange={(e) => { setPriority(e.target.value); setOffset(0); }}><option value="">{t("allPriorities")}</option><option value="normal">{t("priorities.normal")}</option><option value="high">{t("priorities.high")}</option><option value="urgent">{t("priorities.urgent")}</option></select>
      </div>
      <div className="mt-4">
        {api.loading && api.items.length === 0 ? <TableSkeleton rows={6} /> : api.items.length === 0 ? <EmptyState icon={ClipboardCheck} title={t("emptyTitle")} description={t("emptyDescription")} /> : (
          <div className="overflow-x-auto rounded-lg border"><table className="w-full min-w-[900px] text-sm"><thead className="bg-muted/50"><tr><th className="px-4 py-3 text-left">{t("columns.ticket")}</th><th className="px-4 py-3 text-left">{t("columns.service")}</th><th className="px-4 py-3 text-left">{t("columns.identifiers")}</th><th className="px-4 py-3 text-left">{t("columns.priority")}</th><th className="px-4 py-3 text-left">{t("columns.status")}</th><th className="px-4 py-3 text-left">{t("columns.created")}</th><th className="px-4 py-3 text-right">{t("columns.actions")}</th></tr></thead><tbody>{api.items.map((item) => <tr key={item.id} className="border-t hover:bg-muted/20"><td className="px-4 py-3 font-mono font-medium">{ticketReference(item.ticket_number)}</td><td className="px-4 py-3">{item.service || "-"}</td><td className="max-w-[280px] truncate px-4 py-3">{item.identifiers.join(", ") || "-"}</td><td className="px-4 py-3"><Badge variant="outline">{t(`priorities.${item.priority}`)}</Badge></td><td className="px-4 py-3"><Badge variant={item.status === "pending" ? "secondary" : item.status === "completed" ? "success" : "outline"}>{t(`statuses.${item.status}`)}</Badge></td><td className="px-4 py-3 text-muted-foreground">{formatDate(item.created_at)}</td><td className="px-4 py-3"><div className="flex justify-end gap-1"><Button variant="ghost" size="icon" onClick={() => openDetail(item)} title={t("view")}><Eye className="h-4 w-4" /></Button>{item.status === "delivery_failed" && <Button variant="ghost" size="icon" disabled={busy === item.id} onClick={() => run(item.id, () => api.complete(item.id), t("completedToast"))} title={t("retry")}><Check className="h-4 w-4" /></Button>}{item.status === "pending" && <><Button variant="ghost" size="icon" disabled={busy === item.id} onClick={() => run(item.id, () => api.complete(item.id), t("completedToast"))} title={t("complete")}><Check className="h-4 w-4" /></Button><Button variant="ghost" size="icon" onClick={() => { setManualTarget(item); setManualContent(""); }} title={t("manual")}><FilePenLine className="h-4 w-4" /></Button><Button variant="ghost" size="icon" disabled={busy === item.id} onClick={() => run(item.id, () => api.dismiss(item.id), t("dismissedToast"))} title={t("dismiss")}><X className="h-4 w-4" /></Button></>}</div></td></tr>)}</tbody></table></div>
        )}
      </div>
      <div className="mt-4 flex items-center justify-between text-sm text-muted-foreground"><span>{t("total", { count: api.total })}</span><div className="flex gap-2"><Button variant="outline" size="sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}>{t("previous")}</Button><Button variant="outline" size="sm" disabled={offset + PAGE_SIZE >= api.total} onClick={() => setOffset(offset + PAGE_SIZE)}>{t("next")}</Button></div></div>

      <Dialog open={!!selected} onOpenChange={(open) => !open && setSelected(null)}><DialogContent className="sm:max-w-2xl"><DialogHeader><DialogTitle>{selected && ticketReference(selected.ticket_number)}</DialogTitle><DialogDescription>{selected?.service}</DialogDescription></DialogHeader>{selected && <div className="space-y-4 overflow-y-auto"><div><div className="text-xs font-medium uppercase text-muted-foreground">{t("request")}</div><p className="mt-1 whitespace-pre-wrap rounded-md bg-muted p-3 text-sm">{selected.summary}</p></div><div><div className="text-xs font-medium uppercase text-muted-foreground">{t("history")}</div><div className="mt-2 space-y-2">{events.length === 0 ? <p className="text-sm text-muted-foreground">{t("noHistory")}</p> : events.map((event) => <div key={event.id} className="rounded-md border p-3 text-sm"><div className="flex justify-between gap-3"><span className="font-medium">{event.action}</span><span className="text-xs text-muted-foreground">{formatDate(event.created_at)}</span></div>{event.content && <p className="mt-2 whitespace-pre-wrap text-muted-foreground">{event.content}</p>}</div>)}</div></div></div>}</DialogContent></Dialog>

      <Dialog open={!!manualTarget} onOpenChange={(open) => !open && setManualTarget(null)}><DialogContent><DialogHeader><DialogTitle>{t("manualTitle", { ticket: manualTarget ? ticketReference(manualTarget.ticket_number) : "" })}</DialogTitle><DialogDescription>{t("manualDescription")}</DialogDescription></DialogHeader><Textarea className="min-h-36 text-base md:text-sm" maxLength={4000} value={manualContent} onChange={(e) => setManualContent(e.target.value)} placeholder={t("manualPlaceholder")} /><DialogFooter><Button variant="outline" onClick={() => setManualTarget(null)}>{t("cancel")}</Button><Button variant="secondary" disabled={!manualContent.trim() || !!busy} onClick={async () => { if (manualTarget && await run(manualTarget.id, () => api.manual(manualTarget.id, manualContent, false), t("manualSentToast"))) setManualTarget(null); }}>{t("manualKeepOpen")}</Button><Button disabled={!manualContent.trim() || !!busy} onClick={async () => { if (manualTarget && await run(manualTarget.id, () => api.manual(manualTarget.id, manualContent, true), t("manualClosedToast"))) setManualTarget(null); }}>{t("manualAndClose")}</Button></DialogFooter></DialogContent></Dialog>
    </div>
  );
}
