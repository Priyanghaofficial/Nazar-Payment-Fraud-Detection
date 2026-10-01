import { useCallback, useEffect, useState } from "react";
import { api } from "../api/client";
import type { DependencyHealth, ResilienceResponse } from "../api/types";
import { formatMinor } from "../lib/format";

const DB_ICON = (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8">
    <ellipse cx="12" cy="6" rx="8" ry="3" />
    <path d="M4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6" />
  </svg>
);

export function Resilience() {
  const [data, setData] = useState<ResilienceResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<"kill" | "restore" | null>(null);
  const [actionResult, setActionResult] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const poll = useCallback(async () => {
    try {
      const d = await api.resilience();
      setData(d);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  useEffect(() => {
    poll();
    const t = window.setInterval(poll, 3000);
    return () => window.clearInterval(t);
  }, [poll]);

  async function doChaos(action: "kill" | "restore") {
    setBusy(action);
    setActionError(null);
    setActionResult(null);
    try {
      const res = await api.chaosRedis(action);
      setActionResult(`${res.action} -> ${res.container}`);
      // give the container a moment, then refresh dependency status
      window.setTimeout(poll, 800);
    } catch (e) {
      setActionError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }

  const degraded = data ? !(data.dependencies.redis.up && data.dependencies.postgres.up) : false;

  return (
    <div>
      <div className="ph">
        <div>
          <h1>Resilience</h1>
          <p>
            Dependency status via <span className="mn">GET /v1/resilience</span>, polled every 3s. Chaos controls
            actually stop/start the Redis container via the configured container engine.
          </p>
        </div>
        <div className="sp" />
        <span className={`st ${!data ? "s-nt" : degraded ? "s-sp" : "s-ok"}`}>
          <i />
          {!data ? "Checking…" : degraded ? "Degraded" : "Healthy"}
        </span>
        <button className="pill dan" disabled={busy !== null} onClick={() => doChaos("kill")}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
            <circle cx="12" cy="12" r="9" />
            <path d="M6 6l12 12" />
          </svg>
          {busy === "kill" ? "Killing…" : "Kill Redis"}
        </button>
        <button className="pill pri" disabled={busy !== null} onClick={() => doChaos("restore")}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
            <path d="M21 12a9 9 0 11-3-6.7" />
            <path d="M21 3v6h-6" />
          </svg>
          {busy === "restore" ? "Restoring…" : "Restore Redis"}
        </button>
      </div>

      {error && (
        <div className="deg" style={{ marginBottom: 14 }}>
          GET /v1/resilience failed: {error}
        </div>
      )}

      {degraded && data && (
        <div className="deg" style={{ marginBottom: 14 }}>
          DEGRADED · a dependency is unreachable · rails-only decisioning · value capped at{" "}
          {formatMinor(data.degradation_value_cap_minor)} · no BLOCK path active from any degraded path
        </div>
      )}

      {actionResult && (
        <div className="sub" style={{ marginBottom: 14 }}>
          {actionResult}
        </div>
      )}
      {actionError && (
        <div className="deg" style={{ marginBottom: 14 }}>
          chaos action failed: {actionError}
        </div>
      )}

      <div className="row r2" style={{ marginBottom: 14 }}>
        <DepCard name="Redis" dep={data?.dependencies.redis} />
        <DepCard name="Postgres" dep={data?.dependencies.postgres} />
      </div>

      <div className="card">
        <div className="ch">
          <h2>Shed &amp; caps</h2>
          <div className="sp" />
          <span className="sub">live</span>
        </div>
        <div className="kvg">
          <div>
            <div className="k">Async shed</div>
            <div className="v mny">{data ? data.async_shed_total : "—"}</div>
          </div>
          <div>
            <div className="k">Queue depth</div>
            <div className="v mny">{data ? data.async_queue_depth : "—"}</div>
          </div>
          <div>
            <div className="k">Value cap</div>
            <div className="v mny">{data ? formatMinor(data.degradation_value_cap_minor) : "—"}</div>
          </div>
        </div>
      </div>
    </div>
  );
}

function DepCard({ name, dep }: { name: string; dep?: DependencyHealth }) {
  return (
    <div className="card met">
      <div className="lb">
        {DB_ICON}
        {name}
      </div>
      <div className="vl mny">
        {dep ? dep.latency_ms.toFixed(2) : "—"}
        <span className="un"> ms</span>
      </div>
      <div className="ft">
        <div className="dd">{dep?.error ?? (dep ? (dep.up ? "reachable" : "unreachable") : "—")}</div>
        <span className={`st ${dep?.up ? "s-ok" : "s-sp"}`}>
          <i />
          {dep ? (dep.up ? "Up" : "Down") : "—"}
        </span>
      </div>
    </div>
  );
}
