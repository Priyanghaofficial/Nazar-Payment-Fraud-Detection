import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { LatencyResponse } from "../api/types";
import { formatMs } from "../lib/format";

export function LatencyView() {
  const [data, setData] = useState<LatencyResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    async function poll() {
      try {
        const d = await api.latency();
        if (!cancelled) {
          setData(d);
          setError(null);
        }
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      }
    }
    poll();
    const t = window.setInterval(poll, 5000);
    return () => {
      cancelled = true;
      window.clearInterval(t);
    };
  }, []);

  return (
    <div>
      <div className="ph">
        <div>
          <h1>Latency</h1>
          <p>
            <span className="mn">GET /v1/latency</span> — total_ms distribution over the last ~10k decisions, polled
            every 5s. Total only; queue delay and service time are per-decision and shown on Investigation.
          </p>
        </div>
        <div className="sp" />
        {data && (
          <span className="pill">
            <span className="mn">n = {data.n.toLocaleString("en-IN")}</span>
          </span>
        )}
      </div>

      {error && (
        <div className="deg" style={{ marginBottom: 14 }}>
          GET /v1/latency failed: {error}
        </div>
      )}

      {data && (
        <>
          <div className="row r4" style={{ marginBottom: 14 }}>
            <div className="card met">
              <div className="lb">p50 total</div>
              <div className="vl mny">{formatMs(data.p50)}</div>
              <div className="ft">
                <div className="dd">median of the window</div>
              </div>
            </div>
            <div className="card met">
              <div className="lb">p99 total</div>
              <div className="vl mny">{formatMs(data.p99)}</div>
              <div className="ft">
                <div className="dd">tail latency</div>
              </div>
            </div>
            <div className="card met">
              <div className="lb">p99.9 total</div>
              <div className="vl mny">{formatMs(data.p999)}</div>
              <div className="ft">
                <div className="dd">far tail</div>
              </div>
            </div>
            <div className="card met">
              <div className="lb">Max</div>
              <div className="vl mny">{formatMs(data.max)}</div>
              <div className="ft">
                <div className="dd">worst decision in the window</div>
              </div>
            </div>
          </div>

          <div className="card">
            <div className="ch">
              <h2>Percentiles</h2>
              <div className="sp" />
              <span className="sub">total_ms · last ~10k decisions</span>
            </div>
            <table>
              <thead>
                <tr>
                  <th>percentile</th>
                  <th className="r">total_ms</th>
                </tr>
              </thead>
              <tbody>
                {(
                  [
                    ["p50", data.p50],
                    ["p90", data.p90],
                    ["p99", data.p99],
                    ["p99.9", data.p999],
                    ["max", data.max],
                  ] as const
                ).map(([k, v]) => (
                  <tr key={k}>
                    <td className="mn">{k}</td>
                    <td className="r mn">{formatMs(v)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  );
}
