import { formatMs } from "../lib/format";

interface LatencyBlockProps {
  queueMs: number | null | undefined;
  serviceMs: number | null | undefined;
  totalMs: number | null | undefined;
  /** Optional extra context shown next to the total label, e.g. "p99 9.4". */
  totalSuffix?: string;
}

/** Latency ALWAYS renders as three figures — a lone number is a bug (brand kit §6). */
export function LatencyBlock({ queueMs, serviceMs, totalMs, totalSuffix }: LatencyBlockProps) {
  return (
    <div className="lat" style={{ flexDirection: "column", gap: 1 }}>
      <div>
        <span className="k">queue</span>
        <span className="v num">{formatMs(queueMs)}</span>
      </div>
      <div>
        <span className="k">service</span>
        <span className="v num">{formatMs(serviceMs)}</span>
      </div>
      <div className="tot">
        <span className="k">total{totalSuffix ? ` · ${totalSuffix}` : ""}</span>
        <span className="v num">{formatMs(totalMs)}</span>
      </div>
    </div>
  );
}
