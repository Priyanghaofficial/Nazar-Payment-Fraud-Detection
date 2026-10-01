import { useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { ActionTag } from "../components/ActionTag";
import { formatMinor, formatTimeMs, truncateMid } from "../lib/format";
import { useDecisionStream } from "../lib/useDecisionStream";

const NOVEL_THRESHOLD = 0.05;
const CHART_W = 720;
const CHART_H = 260;
const PAD = 34;

/** Live anomaly detection: go/internal/novelty's real feature-space k-NN + conformal
 * p-value, computed on every decision (shadow only — never influences the action). This
 * screen reads from the SAME live stream Live Monitor does, so every new decision that
 * lands updates the scatter, histogram, and counts immediately — nothing here polls. */
export function AnomalyDetection() {
  const navigate = useNavigate();
  const { rows, connState } = useDecisionStream();

  const evaluated = useMemo(() => rows.filter((r) => r.novelty_evaluated), [rows]);
  const warming = rows.length - evaluated.length;
  const novel = useMemo(() => evaluated.filter((r) => r.novelty_p_value < NOVEL_THRESHOLD), [evaluated]);

  const scatter = useMemo(() => {
    if (evaluated.length === 0) return [];
    const ordered = [...evaluated].reverse(); // oldest -> newest, left to right
    const n = ordered.length;
    return ordered.map((r, i) => {
      const x = n === 1 ? PAD : PAD + (i / (n - 1)) * (CHART_W - 2 * PAD);
      const p = r.novelty_p_value;
      const y = CHART_H - PAD - p * (CHART_H - 2 * PAD);
      return { x, y, r, p, novel: p < NOVEL_THRESHOLD };
    });
  }, [evaluated]);

  const histogram = useMemo(() => {
    const buckets = new Array(10).fill(0);
    for (const r of evaluated) {
      let idx = Math.floor(r.novelty_p_value * 10);
      if (idx >= 10) idx = 9;
      if (idx < 0) idx = 0;
      buckets[idx]++;
    }
    return buckets;
  }, [evaluated]);
  const histMax = Math.max(1, ...histogram);

  const mostAnomalous = useMemo(
    () => [...evaluated].sort((a, b) => a.novelty_p_value - b.novelty_p_value).slice(0, 8),
    [evaluated],
  );

  return (
    <div>
      <div className="ph">
        <div>
          <h1>Anomaly Detection</h1>
          <p>
            Feature-space k-NN + conformal p-value, computed on every decision (
            <span className="mn">go/internal/novelty</span>). Shadow only — informs investigation, never blocks or
            changes an action. Updates live from the same stream as Live Monitor.
          </p>
        </div>
        <div className="sp" />
        <span className={`st ${connState === "open" ? "s-ok" : connState === "error" ? "s-sp" : "s-nt"}`}>
          <i />
          {connState === "open" ? "Stream open" : connState === "error" ? "Stream error" : "Connecting"}
        </span>
      </div>

      <div className="row r4" style={{ marginBottom: 14 }}>
        <div className="card met">
          <div className="lb">Evaluated</div>
          <div className="vl mny">{evaluated.length}</div>
          <div className="ft">
            <div className="dd">of {rows.length} decisions shown</div>
          </div>
        </div>
        <div className="card met">
          <div className="lb">Novel (p &lt; {NOVEL_THRESHOLD})</div>
          <div className="vl mny">{novel.length}</div>
          <div className="ft">
            <div className="dd">as unusual as fewer than 5% of recent traffic</div>
          </div>
        </div>
        <div className="card met">
          <div className="lb">Novelty rate</div>
          <div className="vl mny">
            {evaluated.length > 0 ? ((novel.length / evaluated.length) * 100).toFixed(1) : "—"}
            <span className="un">{evaluated.length > 0 ? "%" : ""}</span>
          </div>
          <div className="ft">
            <div className="dd">share of evaluated decisions</div>
          </div>
        </div>
        <div className="card met">
          <div className="lb">Warming up</div>
          <div className="vl mny">{warming}</div>
          <div className="ft">
            <div className="dd">cold-start — reservoir needs 30+ points</div>
          </div>
        </div>
      </div>

      <div className="sp1" style={{ marginBottom: 14 }}>
        <div className="card">
          <div className="ch">
            <h2>How unusual each payment was</h2>
            <div className="sp" />
            <span className="sub">conformal p-value · lower means less like recent traffic</span>
          </div>
          <div style={{ padding: "4px 18px 16px" }}>
            {scatter.length === 0 ? (
              <div style={{ color: "var(--ink4)", fontSize: 12.5, padding: "40px 0", textAlign: "center" }}>
                No evaluated decisions yet — the reservoir needs 30+ observed transactions before novelty scoring
                turns on.
              </div>
            ) : (
              <svg viewBox={`0 0 ${CHART_W} ${CHART_H}`} style={{ width: "100%", height: "auto" }}>
                {/* The flagged band, drawn as a region rather than a line: anything inside it
                    is as unusual as the least usual 5% of recent traffic. */}
                <rect
                  x={PAD}
                  y={CHART_H - PAD - NOVEL_THRESHOLD * (CHART_H - 2 * PAD)}
                  width={CHART_W - 2 * PAD}
                  height={NOVEL_THRESHOLD * (CHART_H - 2 * PAD)}
                  fill="var(--stop)"
                  opacity="0.10"
                />
                {[0, 0.05, 0.25, 0.5, 1].map((t) => {
                  const y = CHART_H - PAD - t * (CHART_H - 2 * PAD);
                  return (
                    <g key={t}>
                      <line
                        x1={PAD}
                        y1={y}
                        x2={CHART_W - PAD}
                        y2={y}
                        stroke={t === NOVEL_THRESHOLD ? "var(--stop)" : "var(--bd)"}
                        strokeDasharray={t === NOVEL_THRESHOLD ? "5 4" : undefined}
                      />
                      <text
                        x={PAD - 7}
                        y={y + 4}
                        fontSize="11"
                        textAnchor="end"
                        fill={t === NOVEL_THRESHOLD ? "var(--stop)" : "var(--ink4)"}
                      >
                        {t}
                      </text>
                    </g>
                  );
                })}
                <text x={PAD} y={PAD - 12} fontSize="11" fill="var(--ink4)">
                  usual
                </text>
                <text x={PAD} y={CHART_H - PAD + 20} fontSize="11" fill="var(--stop)">
                  unusual — flagged below 0.05
                </text>
                <text x={CHART_W - PAD} y={CHART_H - PAD + 20} fontSize="11" fill="var(--ink4)" textAnchor="end">
                  newest →
                </text>
                {scatter.map((pt, i) => (
                  <circle
                    key={i}
                    cx={pt.x}
                    cy={pt.y}
                    r={pt.novel ? 4 : 2.5}
                    fill={pt.novel ? "var(--stop)" : "var(--indigo)"}
                    opacity={pt.novel ? 0.95 : 0.4}
                  />
                ))}
              </svg>
            )}
          </div>
        </div>

        <div className="card">
          <div className="ch">
            <h2>p-value distribution</h2>
          </div>
          <div style={{ padding: "4px 18px 16px" }}>
            {evaluated.length === 0 ? (
              <div style={{ color: "var(--ink4)", fontSize: 12.5 }}>no data yet</div>
            ) : (
              histogram.map((count, i) => (
                <div key={i} style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 4 }}>
                  <span className="mn" style={{ width: 40, fontSize: 10.5, color: "var(--ink4)" }}>
                    {(i / 10).toFixed(1)}
                  </span>
                  <div style={{ flex: 1, height: 12, background: "var(--hover)", borderRadius: 99, overflow: "hidden" }}>
                    <div
                      style={{
                        width: `${(count / histMax) * 100}%`,
                        height: "100%",
                        background: i === 0 ? "var(--warn)" : "var(--indigo)",
                        borderRadius: 99,
                      }}
                    />
                  </div>
                  <span className="mn" style={{ width: 24, fontSize: 10.5, color: "var(--ink3)", textAlign: "right" }}>
                    {count}
                  </span>
                </div>
              ))
            )}
          </div>
        </div>
      </div>

      <div className="card">
        <div className="ch">
          <h2>Most anomalous, right now</h2>
          <span className="badge">{mostAnomalous.length}</span>
        </div>
        <table>
          <thead>
            <tr>
              <th>Time</th>
              <th>Payer</th>
              <th>Beneficiary</th>
              <th className="r">Amount</th>
              <th>Decision</th>
              <th className="r">p-value</th>
            </tr>
          </thead>
          <tbody>
            {mostAnomalous.length === 0 && (
              <tr>
                <td colSpan={6} style={{ color: "var(--ink4)" }}>
                  Nothing evaluated yet.
                </td>
              </tr>
            )}
            {mostAnomalous.map((r) => (
              <tr key={r._id} onClick={() => navigate(`/investigate?id=${encodeURIComponent(r.end_to_end_id)}`)}>
                <td className="ts">{formatTimeMs(r.decided_at_ms)}</td>
                <td className="mn">{truncateMid(r.debtor_account, 6, 4)}</td>
                <td className="mn">{truncateMid(r.creditor_account, 6, 4)}</td>
                <td className="r">
                  <span className="mny amt">{formatMinor(r.amount_minor)}</span>
                </td>
                <td>
                  <ActionTag action={r.action} />
                </td>
                <td className="r mn" style={{ color: r.novelty_p_value < NOVEL_THRESHOLD ? "var(--warn)" : "var(--ink)" }}>
                  {r.novelty_p_value.toFixed(4)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
