import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { CalibrationResponse } from "../api/types";

export function Calibration() {
  const [data, setData] = useState<CalibrationResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .calibration()
      .then(setData)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)));
  }, []);

  return (
    <div>
      <div className="ph">
        <div>
          <h1>Calibration</h1>
          <p>
            Calibrator metadata and live score distribution from <span className="mn">GET /v1/calibration</span>.
          </p>
        </div>
        <div className="sp" />
        {data && (
          <span className="pill">
            <span className="mn">{data.model_bundle}</span>
          </span>
        )}
      </div>

      {error && (
        <div className="deg" style={{ marginBottom: 14 }}>
          GET /v1/calibration failed: {error}
        </div>
      )}
      {!data && !error && <div className="sub">Loading…</div>}

      {data && (
        <>
          <div className="row r4" style={{ marginBottom: 14 }}>
            <div className="card met">
              <div className="lb">Calibrator</div>
              <div className="vl mny" style={{ fontSize: 20 }}>
                {data.calibrator_method}
              </div>
              <div className="ft">
                <div className="dd">
                  version <span className="mn">{data.calibrator_version}</span>
                </div>
              </div>
            </div>
            <div className="card met">
              <div className="lb">Model bundle</div>
              <div className="vl mny" style={{ fontSize: 20 }}>
                {data.model_bundle}
              </div>
              <div className="ft">
                <div className="dd">
                  prevalence table <span className="mn">{data.prevalence.version}</span>
                </div>
              </div>
            </div>
            <div className="card met">
              <div className="lb">Train prevalence</div>
              <div className="vl mny">{formatPct(data.prevalence.train_prevalence)}</div>
              <div className="ft">
                <div className="dd">oversampled training slice</div>
              </div>
            </div>
            <div className="card met">
              <div className="lb">Natural prevalence</div>
              <div className="vl mny">{formatPct(data.prevalence.natural_prevalence)}</div>
              <div className="ft">
                <div className="dd">every ₹ threshold depends on this</div>
              </div>
            </div>
          </div>

          <div className="sp2">
            <div className="card">
              <div className="ch">
                <h2>Score distribution</h2>
                <div className="sp" />
                <span className="sub">n = {data.score_distribution.n}</span>
              </div>
              <ScoreHistogram buckets={data.score_distribution.buckets} counts={data.score_distribution.counts} />
            </div>

            <div className="card">
              <div className="ch">
                <h2>Reliability diagram</h2>
                <div className="sp" />
                <span className="sub">predicted vs observed</span>
              </div>
              {data.reliability_diagram_available ? (
                <div style={{ padding: "4px 18px 18px", fontSize: 12.5, color: "var(--ink3)" }}>
                  Available, not yet rendered in this build.
                </div>
              ) : (
                <div style={{ padding: "4px 18px 18px" }}>
                  <div className="deg">{data.reliability_diagram_note}</div>
                </div>
              )}
            </div>
          </div>
        </>
      )}
    </div>
  );
}

function formatPct(v: number): string {
  return `${(v * 100).toFixed(3)}%`;
}

function ScoreHistogram({ buckets, counts }: { buckets: string[]; counts: number[] }) {
  const max = Math.max(1, ...counts);
  return (
    <div className="stage" style={{ gridTemplateColumns: "80px 1fr 56px" }}>
      {buckets.map((b, i) => (
        <div key={b} style={{ display: "contents" }}>
          <span className="k mn">{b}</span>
          <div className="b">
            <i style={{ width: `${(counts[i] / max) * 100}%` }} />
          </div>
          <span className="v">{counts[i]}</span>
        </div>
      ))}
      {buckets.length === 0 && <div className="sub">no scored decisions yet</div>}
    </div>
  );
}
