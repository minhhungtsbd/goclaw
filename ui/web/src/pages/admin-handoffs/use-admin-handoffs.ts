import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import type { AdminHandoff, AdminHandoffEvent } from "./types";

export interface HandoffFilters {
  search: string;
  status: string;
  priority: string;
  offset: number;
  limit: number;
}

export function useAdminHandoffs() {
  const { t } = useTranslation("admin-handoffs");
  const http = useHttp();
  const [items, setItems] = useState<AdminHandoff[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async (filters: HandoffFilters) => {
    setLoading(true);
    try {
      const response = await http.get<{ items: AdminHandoff[]; total: number }>("/v1/admin-handoffs", {
        search: filters.search,
        status: filters.status,
        priority: filters.priority,
        offset: String(filters.offset),
        limit: String(filters.limit),
      });
      setItems(response.items ?? []);
      setTotal(response.total ?? 0);
    } catch (error) {
      toast.error(t("loadFailed"), error instanceof Error ? error.message : "");
    } finally {
      setLoading(false);
    }
  }, [http, t]);

  const detail = useCallback(async (id: string) => {
    return http.get<{ handoff: AdminHandoff; events: AdminHandoffEvent[] }>(`/v1/admin-handoffs/${id}`);
  }, [http]);

  const complete = useCallback(async (id: string) => {
    await http.post(`/v1/admin-handoffs/${id}/complete`);
  }, [http]);

  const manual = useCallback(async (id: string, content: string, closeAfter: boolean) => {
    await http.post(`/v1/admin-handoffs/${id}/manual`, { content, close_after: closeAfter });
  }, [http]);

  const dismiss = useCallback(async (id: string) => {
    await http.post(`/v1/admin-handoffs/${id}/dismiss`);
  }, [http]);

  return { items, total, loading, load, detail, complete, manual, dismiss };
}
