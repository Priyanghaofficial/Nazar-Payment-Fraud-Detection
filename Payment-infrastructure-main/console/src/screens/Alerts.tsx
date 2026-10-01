import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import type { AlertRow } from "../api/types";
import { ActionTag } from "../components/ActionTag";
import { formatMinor, formatTimeMs, truncateMid } from "../lib/format";

type Filter = "open" | "resolved" | "all";

const SEVERITY_CLASS: Record<string, string> = {
  low: "s-nt",
  medium: "s-wn",
  high: "s-hd",
  critical: "s-sp",
};

/** Real alert management (problem_statement.txt): every LIVE decision that lands anywhere
 * other than ALLOW/ALLOW_MONITOR raises a persisted alert (go/internal/persist/alerts.go),
 * not a client-side filter over Live Monitor. Resolve is a real, persisted state change.
 * Polls every 3s rather than riding the SSE decision stream — alerts land a few hundred ms
 * after their decision (async shipper lane), so a short poll is the honest way to stay
 * current without inventing a second push channel for what's fundamentally a queue view. */
export function Alerts() {
  const navigate = useNavigate();
  const [filter, setFilter] = useState<Filter>("open");
  const [alerts, setAlerts] = useState<AlertRow[]>([]);
  const [openCount, setOpenCount] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [resolving, setResolving] = useState<number | null>(null);

  useEffect(() => {
    let cancelled = false;
    async function poll() {
      try {
        const res = await api.alerts(filter);
        if (!cancelled) {
          setAlerts(res.alerts ?? []);
          setOpenCount(res.open_count);
          setError(null);
        }
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      }
    }
    poll();
    const t = window.setInterval(poll, 3000);
    return () => {
      cancelled = true;
      window.clearInterval(t);
    };
  }, [filter]);

  async function resolve(id: number) {
    setResolving(id);
    try {
      await api.resolveAlert(id);
      setAlerts((prev) => prev.filter((a) => a.id !== id));
      setOpenCount((prev) => (prev !== null ? Math.max(0, prev - 1) : prev));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setResolving(null);
    }
  }

  return (
    <div>
      <div className="ph">
        <div>
          <h1>Alerts</h1>
          <p>
            Every decision other than ALLOW / ALLOW_MONITOR raises a real alert, persisted via{" "}
            <span className="mn">alerts</span>. Resolve is a real state change.
          </p>
        </div>
        <div className="sp" />
        <span className="badge i">{openCount ?? "—"} open</span>
        <div style={{ display: "flex", gap: 6 }}>
          {(["open", "resolved", "all"] as Filter[]).map((f) => (
            <button key={f} className={`pill gh sm ${filter === f ? "on" : ""}`} onClick={() => setFilter(f)}>
              {f}
            </button>
          ))}
        </div>
      </div>

      {error && (
        <div className="deg" style={{ marginBottom: 14 }}>
          {error}
        </div>
      )}

      <div className="card">
        <div className="ch">
          <h2>Queue</h2>
          <span className="badge">{alerts.length}</span>
        </div>
        <table>
          <thead>
            <tr>
              <th>Raised</th>
              <th>Severity</th>
              <th>Decision</th>
              <th>Payer</th>
              <th>Beneficiary</th>
              <th className="r">Amount</th>
              <th className="r">Action</th>
            </tr>
          </thead>
          <tbody>
            {alerts.length === 0 && (
              <tr>
                <td colSpan={7} style={{ color: "var(--ink4)" }}>
                  {filter === "open" ? "No open alerts." : "Nothing here yet."}
                </td>
              </tr>
            )}
            {alerts.map((a) => (
              <tr key={a.id}>
                <td className="ts" onClick={() => navigate(`/investigate?id=${encodeURIComponent(a.end_to_end_id)}`)}>
                  {formatTimeMs(a.raised_at_ms)}
                </td>
                <td>
                  <span className={`st ${SEVERITY_CLASS[a.severity] ?? "s-nt"}`}>
                    <i />
                    {a.severity}
                  </span>
                </td>
                <td>
                  <ActionTag action={a.action} />
                </td>
                <td className="mn">{truncateMid(a.debtor_account, 6, 4)}</td>
                <td className="mn">{truncateMid(a.creditor_account, 6, 4)}</td>
                <td className="r">
                  <span className="mny amt">{formatMinor(a.amount_minor)}</span>
                </td>
                <td className="r">
                  {a.status === "open" ? (
                    <button className="pill sm" disabled={resolving === a.id} onClick={() => resolve(a.id)}>
                      {resolving === a.id ? "…" : "Resolve"}
                    </button>
                  ) : (
                    <span className="sub">resolved</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
