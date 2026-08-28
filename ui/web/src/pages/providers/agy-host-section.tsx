import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { useHttp } from "@/hooks/use-ws";

interface AGYHostSectionProps {
  profile: string;
}

interface AGYStatus {
  authenticated: boolean;
}

export function AGYHostSection({ profile }: AGYHostSectionProps) {
  const { t } = useTranslation("providers");
  const http = useHttp();
  const terminalElement = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const [starting, setStarting] = useState(false);
  const [active, setActive] = useState(false);
  const [authenticated, setAuthenticated] = useState<boolean | null>(null);
  const [error, setError] = useState("");

  const checkStatus = async () => {
    try {
      const status = await http.get<AGYStatus>(`/v1/agy-host/${profile}/status`);
      setAuthenticated(status.authenticated);
      setError("");
    } catch {
      setAuthenticated(null);
      setError(t("agyHost.unavailable"));
    }
  };

  useEffect(() => { void checkStatus(); }, [profile]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => () => {
    socketRef.current?.close();
    terminalRef.current?.dispose();
  }, []);

  const startTerminal = async () => {
    setStarting(true);
    setError("");
    try {
      const ticket = await http.post<{ path: string }>(`/v1/agy-host/${profile}/terminal-ticket`);
      setActive(true);
      await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
      const term = new Terminal({ cursorBlink: true, fontSize: 13, fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace", theme: { background: "#111111", foreground: "#f2f2f2" } });
      const fit = new FitAddon();
      term.loadAddon(fit);
      terminalRef.current?.dispose();
      terminalRef.current = term;
      if (!terminalElement.current) throw new Error("terminal mount unavailable");
      term.open(terminalElement.current);
      fit.fit();
      const base = new URL(http.rawUrl(ticket.path));
      base.protocol = base.protocol === "https:" ? "wss:" : "ws:";
      const socket = new WebSocket(base);
      socket.binaryType = "arraybuffer";
      socketRef.current = socket;
      socket.onopen = () => term.focus();
      socket.onmessage = (event) => term.write(typeof event.data === "string" ? event.data : new Uint8Array(event.data));
      socket.onclose = () => { setActive(false); void checkStatus(); };
      socket.onerror = () => setError(t("agyHost.terminalFailed"));
      term.onData((data) => { if (socket.readyState === WebSocket.OPEN) socket.send(data); });
    } catch {
      setActive(false);
      setError(t("agyHost.terminalFailed"));
    } finally {
      setStarting(false);
    }
  };

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
      <div className="space-y-1">
        <h3 className="text-sm font-medium">{t("agyHost.title")}</h3>
        <p className="text-xs text-muted-foreground">{t("agyHost.description")}</p>
      </div>
      <p className="text-xs text-muted-foreground">{authenticated === true ? t("agyHost.authenticated") : authenticated === false ? t("agyHost.notAuthenticated") : t("agyHost.checking")}</p>
      <Button type="button" variant="outline" onClick={() => void startTerminal()} disabled={starting || active}>
        {starting ? t("agyHost.starting") : active ? t("agyHost.active") : t("agyHost.openTerminal")}
      </Button>
      <p className="text-xs text-muted-foreground">{t("agyHost.instructions")}</p>
      {error ? <p className="text-sm text-destructive">{error}</p> : null}
      {active ? <div ref={terminalElement} className="h-96 overflow-hidden rounded-md border bg-[#111] p-2" /> : null}
    </section>
  );
}
