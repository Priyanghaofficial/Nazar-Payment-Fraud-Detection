import type { LatencyResponse } from "../api/types";
import { formatMs } from "../lib/format";

/** GET /v1/latency is a distribution of total_ms only (no queue/service split at the
 * aggregate level — that split exists per-decision, see LatencyBlock). Rendered as the
 * full percentile spread rather than one number, so it still isn't "a lone latency figure". */
export function PercentileLatency({ data, compact = false }: { data: LatencyResponse; compact?: boolean }) {
  const cells: { k: string; v: number; emphasize?: boolean }[] = [
    { k: "p50", v: data.p50 },
    { k: "p90", v: data.p90 },
    { k: "p99", v: data.p99 },
    { k: "p99.9", v: data.p999, emphasize: !compact },
    { k: "max", v: data.max },
  ];
  return (
    <div style={{ display: "flex", gap: compact ? 20 : 26, flexWrap: "wrap" }}>
      {cells.map((c) => (
        <div key={c.k} className="lat" style={{ flexDirection: "column", gap: 1 }}>
          <span className="k">{c.k}</span>
          <span className={`v num ${c.emphasize ? "" : ""}`} style={c.emphasize ? { color: "var(--ink)", fontWeight: 600 } : undefined}>
            {formatMs(c.v)}
          </span>
        </div>
      ))}
      <div className="lat" style={{ flexDirection: "column", gap: 1 }}>
        <span className="k">n</span>
        <span className="v num">{data.n}</span>
      </div>
    </div>
  );
}
