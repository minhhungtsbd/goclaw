import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Loader2 } from "lucide-react";
import {
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const DEFAULT_TIMEOUT_SECONDS = 15;
const MIN_TIMEOUT_SECONDS = 3;
const MAX_TIMEOUT_SECONDS = 60;
const TOKEN_SECRET_KEY = "tools.cloudmini_proxy.api_token";

interface Props {
  initialSettings: Record<string, unknown>;
  secretsSet?: Record<string, boolean>;
  onSave: (settings: Record<string, unknown>) => Promise<void>;
  onCancel: () => void;
}

function resolveTimeoutSeconds(settings: Record<string, unknown>): number {
  const value = settings.timeout_seconds;
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return DEFAULT_TIMEOUT_SECONDS;
  }
  return Math.min(Math.max(Math.trunc(value), MIN_TIMEOUT_SECONDS), MAX_TIMEOUT_SECONDS);
}

export function CloudminiProxyCheckSettingsForm({
  initialSettings,
  secretsSet,
  onSave,
  onCancel,
}: Props) {
  const { t } = useTranslation("tools");
  const [timeoutSeconds, setTimeoutSeconds] = useState(() => resolveTimeoutSeconds(initialSettings));
  const [apiToken, setApiToken] = useState("");
  const [saving, setSaving] = useState(false);
  const tokenConfigured = secretsSet?.[TOKEN_SECRET_KEY] === true;

  useEffect(() => {
    setTimeoutSeconds(resolveTimeoutSeconds(initialSettings));
    setApiToken("");
  }, [initialSettings]);

  const handleSave = async () => {
    const timeout = Math.min(
      Math.max(Math.trunc(timeoutSeconds), MIN_TIMEOUT_SECONDS),
      MAX_TIMEOUT_SECONDS,
    );
    const settings: Record<string, unknown> = {
      timeout_seconds: timeout,
      allowed_agent_keys: ["linh-nhi-support-lead"],
    };
    if (apiToken.trim()) {
      settings.auth = { api_token: apiToken.trim() };
    }

    setSaving(true);
    try {
      await onSave(settings);
    } catch {
      // The page hook shows the request failure and keeps this dialog open.
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t("builtin.cloudminiProxyCheck.title")}</DialogTitle>
        <DialogDescription>{t("builtin.cloudminiProxyCheck.description")}</DialogDescription>
      </DialogHeader>

      <div className="space-y-4 py-2">
        <div className="grid gap-1.5">
          <Label htmlFor="cloudmini-api-token" className="text-sm">
            {t("builtin.cloudminiProxyCheck.apiToken")}
          </Label>
          <Input
            id="cloudmini-api-token"
            type="password"
            autoComplete="off"
            value={apiToken}
            onChange={(event) => setApiToken(event.target.value)}
            placeholder={tokenConfigured
              ? t("builtin.cloudminiProxyCheck.apiTokenReplace")
              : t("builtin.cloudminiProxyCheck.apiTokenPlaceholder")}
            className="text-base md:text-sm"
          />
          <p className="text-xs text-muted-foreground">
            {tokenConfigured
              ? t("builtin.cloudminiProxyCheck.apiTokenConfigured")
              : t("builtin.cloudminiProxyCheck.apiTokenHint")}
          </p>
        </div>

        <div className="grid gap-1.5">
          <Label htmlFor="cloudmini-timeout-seconds" className="text-sm">
            {t("builtin.cloudminiProxyCheck.timeoutSeconds")}
          </Label>
          <Input
            id="cloudmini-timeout-seconds"
            type="number"
            min={MIN_TIMEOUT_SECONDS}
            max={MAX_TIMEOUT_SECONDS}
            step={1}
            value={timeoutSeconds}
            onChange={(event) => setTimeoutSeconds(Number(event.target.value) || DEFAULT_TIMEOUT_SECONDS)}
            className="max-w-[140px] text-base md:text-sm"
          />
          <p className="text-xs text-muted-foreground">
            {t("builtin.cloudminiProxyCheck.timeoutHint", {
              min: MIN_TIMEOUT_SECONDS,
              max: MAX_TIMEOUT_SECONDS,
            })}
          </p>
        </div>

        <p className="text-xs text-muted-foreground">
          {t("builtin.cloudminiProxyCheck.scopeHint")}
        </p>
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onCancel}>
          {t("builtin.cloudminiProxyCheck.cancel")}
        </Button>
        <Button onClick={handleSave} disabled={saving}>
          {saving && <Loader2 className="h-4 w-4 animate-spin" />}
          {saving ? t("builtin.cloudminiProxyCheck.saving") : t("builtin.cloudminiProxyCheck.save")}
        </Button>
      </DialogFooter>
    </>
  );
}
