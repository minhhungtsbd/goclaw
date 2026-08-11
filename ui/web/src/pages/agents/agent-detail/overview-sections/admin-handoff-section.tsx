import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { BellRing } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import type { AgentData } from "@/types/agent";

type AdminHandoff = { enabled: boolean; channel: string; chat_id: string };

function readConfig(agent: AgentData): AdminHandoff {
  const bag = (agent.other_config ?? {}) as Record<string, unknown>;
  const raw = (bag.admin_handoff ?? {}) as Partial<AdminHandoff>;
  return { enabled: raw.enabled === true, channel: raw.channel ?? "", chat_id: raw.chat_id ?? "" };
}

interface Props {
  agent: AgentData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

export function AdminHandoffSection({ agent, onUpdate }: Props) {
  const { t } = useTranslation("agents");
  const saved = readConfig(agent);
  const [config, setConfig] = useState(saved);
  const [saving, setSaving] = useState(false);

  useEffect(() => setConfig(readConfig(agent)), [agent.other_config]);

  const dirty = JSON.stringify(config) !== JSON.stringify(saved);
  const valid = !config.enabled || (config.channel.trim() !== "" && config.chat_id.trim() !== "");

  const save = async () => {
    setSaving(true);
    try {
      const bag = { ...((agent.other_config ?? {}) as Record<string, unknown>) };
      if (config.enabled) {
        bag.admin_handoff = { enabled: true, channel: config.channel.trim(), chat_id: config.chat_id.trim() };
      } else {
        delete bag.admin_handoff;
      }
      await onUpdate({ other_config: bag });
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <BellRing className="h-4 w-4 text-orange-500" />
          <h3 className="text-sm font-medium">{t("detail.adminHandoff.title", "Admin Handoff")}</h3>
        </div>
        {dirty && <Button size="sm" onClick={save} disabled={saving || !valid}>{saving ? t("saving", "Saving...") : t("save", "Save")}</Button>}
      </div>
      <p className="text-xs-plus text-muted-foreground">{t("detail.adminHandoff.hint", "Send verified manual-support cases to one fixed internal channel. The agent cannot choose a different destination.")}</p>
      <div className="flex items-center justify-between">
        <Label htmlFor="admin-handoff-enabled">{t("detail.adminHandoff.enabled", "Enable admin handoff")}</Label>
        <Switch id="admin-handoff-enabled" checked={config.enabled} onCheckedChange={(enabled) => setConfig((current) => ({ ...current, enabled }))} />
      </div>
      {config.enabled && (
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="admin-handoff-channel">{t("detail.adminHandoff.channel", "Channel name")}</Label>
            <Input id="admin-handoff-channel" className="text-base md:text-sm" value={config.channel} onChange={(event) => setConfig((current) => ({ ...current, channel: event.target.value }))} placeholder="telegram" />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="admin-handoff-chat">{t("detail.adminHandoff.chatId", "Group or chat ID")}</Label>
            <Input id="admin-handoff-chat" className="text-base md:text-sm" value={config.chat_id} onChange={(event) => setConfig((current) => ({ ...current, chat_id: event.target.value }))} placeholder="-100..." />
          </div>
        </div>
      )}
    </section>
  );
}
