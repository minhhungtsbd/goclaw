import { useCallback, useEffect, useState } from "react";
import { RefreshCw, UserRoundCheck } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { useHttp } from "@/hooks/use-ws";
import { formatDate } from "@/lib/format";
import { toast } from "@/stores/use-toast-store";
import { useUiStore } from "@/stores/use-ui-store";

interface ChannelAdminTakeover {
  id: string;
  channel_name: string;
  chat_id: string;
  agent_key?: string;
  last_admin_message?: string;
  taken_over_at: string;
  expires_at: string;
}

export function ChannelAdminTakeovers({ channelName }: { channelName: string }) {
  const { t } = useTranslation("channels");
  const http = useHttp();
  const timezone = useUiStore((state) => state.timezone);
  const [items, setItems] = useState<ChannelAdminTakeover[]>([]);
  const [loading, setLoading] = useState(false);
  const [releasing, setReleasing] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const response = await http.get<{ items: ChannelAdminTakeover[] }>(
        `/v1/channel-admin-takeovers?channel_name=${encodeURIComponent(channelName)}`,
      );
      setItems(response.items ?? []);
    } catch (error) {
      toast.error(t("detail.general.takeovers.loadFailed"), error instanceof Error ? error.message : "");
    } finally {
      setLoading(false);
    }
  }, [channelName, http, t]);

  useEffect(() => { void load(); }, [load]);

  const release = async (id: string) => {
    setReleasing(id);
    try {
      await http.post(`/v1/channel-admin-takeovers/${encodeURIComponent(id)}/release`, {
        reason: t("detail.general.takeovers.releaseReason"),
      });
      toast.success(t("detail.general.takeovers.released"));
      await load();
    } catch (error) {
      toast.error(t("detail.general.takeovers.releaseFailed"), error instanceof Error ? error.message : "");
    } finally {
      setReleasing(null);
    }
  };

  return (
    <section className="space-y-3 overflow-hidden rounded-lg border p-3 sm:p-4">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <h3 className="text-sm font-medium">{t("detail.general.takeovers.title")}</h3>
          <p className="mt-1 text-xs text-muted-foreground">{t("detail.general.takeovers.description")}</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
          <RefreshCw className={`mr-1 h-4 w-4 ${loading ? "animate-spin" : ""}`} />
          {t("detail.general.takeovers.refresh")}
        </Button>
      </div>

      {items.length === 0 ? (
        <div className="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">
          {t("detail.general.takeovers.empty")}
        </div>
      ) : (
        <div className="space-y-2">
          {items.map((item) => (
            <div key={item.id} className="flex flex-col gap-3 rounded-md border bg-muted/20 p-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="min-w-0 space-y-1">
                <div className="flex items-center gap-2 text-sm font-medium">
                  <UserRoundCheck className="h-4 w-4 shrink-0 text-amber-600" />
                  <span className="break-all">{item.chat_id}</span>
                </div>
                {item.last_admin_message && <p className="line-clamp-2 text-sm text-muted-foreground">{item.last_admin_message}</p>}
                <p className="text-xs text-muted-foreground">
                  {t("detail.general.takeovers.expires", { value: formatDate(item.expires_at, timezone) })}
                </p>
              </div>
              <Button
                variant="outline"
                size="sm"
                className="w-full sm:w-auto"
                disabled={releasing === item.id}
                onClick={() => void release(item.id)}
              >
                {releasing === item.id ? t("detail.general.takeovers.releasing") : t("detail.general.takeovers.release")}
              </Button>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
