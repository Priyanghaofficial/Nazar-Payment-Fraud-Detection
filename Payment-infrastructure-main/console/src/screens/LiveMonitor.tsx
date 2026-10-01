import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import type { LatencyResponse, ResilienceResponse } from "../api/types";
import { ActionTag } from "../components/ActionTag";
import { DecisionBlock } from "../components/DecisionBlock";
import { PercentileLatency } from "../components/PercentileLatency";
import { useAdvisoryMaxRung } from "../lib/usePolicy";
import { formatMinorCompact, formatMinor, formatMs, formatTimeMs, truncateMid } from "../lib/format";
import { useDecisionStream } from "../lib/useDecisionStream";

const AV_COLORS = ["#6366F1", "#0EA5E9", "#F59E0B", "#14B8A6", "#8B5CF6", "#EC4899", "#64748B", "#F2695C"];
function avatarColor(id: string) {
  let h = 0;
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
  return AV_COLORS[h % AV_COLORS.length];
}
function initials(id: string) {
  const tail = id.split(/[-_]/).pop() ?? id;
  return tail.slice(0, 2).toUpperCase();
}

const ACTION_LABEL: Record<string, string> = {
  ALLOW: "Allowed",
  ALLOW_MONITOR: "Allow & monitor",
  STEP_UP: "Step-up",
  STEP_UP_INTERSTITIAL: "Step-up interstitial",
  HOLD: "Held",
  CAP: "Capped",
  BLOCK: "Blocked",
};

export function LiveMonitor() {
  const navigate = useNavigate();
  const { rows, connState } = useDecisionStream();
  const [latency, setLatency] = useState<LatencyResponse | null>(null);
  const [latencyErr, setLatencyErr] = useState<string | null>(null);
  const [resilience, setResilience] = useState<ResilienceResponse | null>(null);
  const advisoryMaxRung = useAdvisoryMaxRung();

  useEffect(() => {
    let cancelled = false;
    async function poll() {
      try {
        const l = await api.latency();
        if (!cancelled) {
          setLatency(l);
          setLatencyErr(null);
        }
      } catch (e) {
        if (!cancelled) setLatencyErr(e instanceof Error ? e.message : String(e));
      }
      try {
        const r = await api.resilience();
        if (!cancelled) setResilience(r);
      } catch {
        // dependency strip is best-effort; the main table doesn't depend on it
      }
    }
    poll();
    const t = window.setInterval(poll, 4000);
    return () => {
      cancelled = true;
      window.clearInterval(t);
    };
  }, []);

  const metrics = useMemo(() => {
    const nonAllow = rows.filter((r) => r.action !== "ALLOW" && r.action !== "ALLOW_MONITOR");
    const valuePrevented = nonAllow.reduce((sum, r) => sum + (r.amount_minor ?? 0), 0);
    const challengeRate = rows.length > 0 ? (nonAllow.length / rows.length) * 100 : null;
    const byAction = new Map<string, number>();
    for (const r of rows) byAction.set(r.action, (byAction.get(r.action) ?? 0) + 1);
    return { valuePrevented, challengeRate, n: rows.length, byAction };
  }, [rows]);

  const latest = rows[0];

  return (
    <div>
      <div className="ph">
        <div>
          <h1>Live Monitor</h1>
          <p>
            Real-time decisions from <span className="mn">GET /v1/stream</span>, hydrated with real history from
            Postgres on load.
          </p>
        </div>
        <div className="sp" />
        <span className={`st ${connState === "open" ? "s-ok" : connState === "error" ? "s-sp" : "s-nt"}`}>
          <i />
          {connState === "open" ? "Stream open" : connState === "error" ? "Stream error" : "Connecting"}
        </span>
      </div>

      {connState === "error" && (
        <div className="deg" style={{ marginBottom: 14 }}>
          Stream connection error — the backend at {api.streamUrl()} is unreachable or dropped the connection. No
          data is being invented in its place.
        </div>
      )}

      <div className="row r4" style={{ marginBottom: 14 }}>
        <div className="card met">
          <div className="lb">Value prevented</div>
          <div className="vl mny">{formatMinorCompact(metrics.valuePrevented)}</div>
          <div className="ft">
            <div className="dd">across {metrics.n} decisions shown below</div>
          </div>
        </div>
        <div className="card met">
          <div className="lb">Decisions shown</div>
          <div className="vl mny">{metrics.n}</div>
          <div className="ft">
            <div className="dd">recent history + live tail</div>
          </div>
        </div>
        <div className="card met">
          <div className="lb">Latency p99</div>
          <div className="vl mny">
            {latency ? latency.p99.toFixed(1) : "—"}
            <span className="un"> ms</span>
          </div>
          <div className="ft">
            <div className="dd">{latency ? `p50 ${latency.p50.toFixed(1)} · max ${latency.max.toFixed(1)}` : "—"}</div>
          </div>
        </div>
        <div className="card met">
          <div className="lb">Challenge rate</div>
          <div className="vl mny">
            {metrics.challengeRate === null ? "—" : metrics.challengeRate.toFixed(1)}
            <span className="un">{metrics.challengeRate === null ? "" : "%"}</span>
          </div>
          <div className="ft">
            <div className="dd">non-ALLOW share of decisions shown</div>
          </div>
        </div>
      </div>

      <div className="sp1" style={{ marginBottom: 14 }}>
        <div className="card">
          <div className="ch">
            <svg className="ci" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
              <path d="M3 12h4l3 8 4-16 3 8h4" />
            </svg>
            <h2>Live decisions</h2>
            <span className="badge">{metrics.n} shown</span>
          </div>
          <div style={{ maxHeight: 560, overflowY: "auto" }}>
            <table>
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Payer</th>
                  <th>Beneficiary</th>
                  <th>Channel</th>
                  <th className="r">Amount</th>
                  <th>Decision</th>
                  <th className="r">Total</th>
                </tr>
              </thead>
              <tbody>
                {rows.length === 0 && (
                  <tr>
                    <td colSpan={7} style={{ color: "var(--ink4)" }}>
                      {connState === "connecting"
                        ? "Connecting to stream…"
                        : "No decisions recorded yet. Fire a transaction via POST /v1/decide or run a demo scenario."}
                    </td>
                  </tr>
                )}
                {rows.map((r) => (
                  <tr
                    key={r._id}
                    className={r._fresh ? "row-flash" : ""}
                    onClick={() => navigate(`/investigate?id=${encodeURIComponent(r.end_to_end_id)}`)}
                    title="Open in Investigation"
                  >
                    <td className="ts">{formatTimeMs(r.decided_at_ms)}</td>
                    <td>
                      <div className="ent">
                        <span className="av2" style={{ background: avatarColor(r.debtor_account) }}>
                          {initials(r.debtor_account)}
                        </span>
                        <div className="tx">
                          <span className="nm">{truncateMid(r.debtor_account, 6, 4)}</span>
                        </div>
                      </div>
                    </td>
                    <td>
                      <span className="id">{truncateMid(r.creditor_account, 6, 4)}</span>
                    </td>
                    <td>
                      <span className="tag">{r.rail}</span>
                    </td>
                    <td className="r">
                      <span className="mny amt">{formatMinor(r.amount_minor)}</span>
                    </td>
                    <td>
                      <ActionTag action={r.action} />
                    </td>
                    <td className="r ts">{formatMs(r.total_ms, 1)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div style={{ padding: "12px 18px", borderTop: "1px solid var(--bd)" }}>
            {latencyErr && <div className="deg">latency endpoint unreachable: {latencyErr}</div>}
            {!latencyErr && latency && <PercentileLatency data={latency} compact />}
          </div>
        </div>

        <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
          {latest ? (
            <DecisionBlock action={latest.action} advisoryMaxRung={advisoryMaxRung} caption="most recent" />
          ) : (
            <div className="card" style={{ padding: 18, color: "var(--ink4)", fontSize: 12.5 }}>
              No decision yet to show a ladder for.
            </div>
          )}
          <div className="card">
            <div className="ch">
              <h2>Session breakdown</h2>
            </div>
            {Object.keys(ACTION_LABEL).map((action) => {
              const count = metrics.byAction.get(action) ?? 0;
              if (count === 0) return null;
              return (
                <div key={action} className="lr2">
                  <div className="t">{ACTION_LABEL[action]}</div>
                  <span className="v mny" style={{ marginLeft: "auto" }}>
                    {count}
                  </span>
                </div>
              );
            })}
            {metrics.n === 0 && (
              <div style={{ padding: "10px 18px", color: "var(--ink4)", fontSize: 12.5 }}>no decisions yet</div>
            )}
          </div>
          <div className="card">
            <div className="ch">
              <h2>Dependencies</h2>
              <div className="sp" />
              <span className="sub">3s poll</span>
            </div>
            <div className="lr2">
              <div className="ic">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8">
                  <ellipse cx="12" cy="6" rx="8" ry="3" />
                  <path d="M4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6" />
                </svg>
              </div>
              <div>
                <div className="t">Redis</div>
              </div>
              <span className={`st ${resilience?.dependencies.redis.up ? "s-ok" : "s-sp"}`} style={{ marginLeft: "auto" }}>
                <i />
                {resilience ? `${resilience.dependencies.redis.latency_ms.toFixed(2)} ms` : "—"}
              </span>
            </div>
            <div className="lr2">
              <div className="ic">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8">
                  <ellipse cx="12" cy="6" rx="8" ry="3" />
                  <path d="M4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6" />
                </svg>
              </div>
              <div>
                <div className="t">Postgres</div>
              </div>
              <span className={`st ${resilience?.dependencies.postgres.up ? "s-ok" : "s-sp"}`} style={{ marginLeft: "auto" }}>
                <i />
                {resilience ? `${resilience.dependencies.postgres.latency_ms.toFixed(2)} ms` : "—"}
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
