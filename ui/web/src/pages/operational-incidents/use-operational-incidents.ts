import { useCallback, useState } from "react";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import { useTranslation } from "react-i18next";
import type { OperationalIncident } from "./types";

export function useOperationalIncidents() {
  const http = useHttp();
  const { t } = useTranslation("operational-incidents");
  const [items, setItems] = useState<OperationalIncident[]>([]);
  const [loading, setLoading] = useState(false);
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const response = await http.get<{ items: OperationalIncident[] }>("/v1/cloudmini/operational-incidents");
      setItems(response.items ?? []);
    } catch (error) {
      toast.error(t("loadFailed", "Could not load incidents"), error instanceof Error ? error.message : "");
    } finally {
      setLoading(false);
    }
  }, [http, t]);
  const create = useCallback(async (incident: Omit<OperationalIncident, "id"> & { id?: string }) => {
    await http.post<OperationalIncident>("/v1/cloudmini/operational-incidents", incident);
  }, [http]);
  const update = useCallback(async (id: string, incident: Omit<OperationalIncident, "id">) => {
    await http.put<OperationalIncident>(`/v1/cloudmini/operational-incidents/${encodeURIComponent(id)}`, incident);
  }, [http]);
  const remove = useCallback(async (id: string) => {
    await http.delete(`/v1/cloudmini/operational-incidents/${encodeURIComponent(id)}`);
  }, [http]);
  return { items, loading, load, create, update, remove };
}
