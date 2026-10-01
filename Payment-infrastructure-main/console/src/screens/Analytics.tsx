import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { AnalyticsResponse, CutRow } from "../api/types";
import { formatMinorCompact } from "../lib/format";

/** Where the fraud is, and what kind it is.
 *
 * Every figure is a group-by over decisions this instance actually made — no pre-aggregation,
 * no seeded totals. A cut with no data shows zero, because on a fresh instance zero is the
 * true answer and inventing a populated map would be the exact thing this project refuses to
 * do.
 *
 * Value is weighted throughout rather than payment counts. That is the same argument the
 * decision rule makes: a thousand two-rupee probes are not the problem a bank has, so a view
 * that ranks by count would point an analyst at the noise. */

const TIER_NOTE =
  "Attack-type labels describe traffic this system generated, so they measure the pipeline, not real-world fraud rates.";

function pctText(v: number): string {
  if (!Number.isFinite(v)) return "—";
  return `${Math.round(v * 100)}%`;
}

/** A cell's colour weight. Deliberately based on value-at-risk share, so the biggest block is
 *  where the most money was exposed, not where the most rows landed. */
function heat(value: number, max: number): number {
  if (max <= 0) return 0;
  return Math.max(0.04, Math.min(1, value / max));
}

function Bars({ rows, empty }: { rows: CutRow[]; empty: string }) {
  if (!rows.length) return <div className="an-empty">{empty}</div>;
  const max = Math.max(...rows.map((r) => r.value_minor), 1);
  return (
    <div className="an-bars">
      {rows.map((r) => (
        <div key={r.key} className="an-bar">
          <div className="an-bar-top">
            <span className="an-bar-l">{r.label}</span>
            <span className="an-bar-v mny">{formatMinorCompact(r.value_minor)}</span>
          </div>
          <div className="an-bar-track">
            <span className="an-bar-fill" style={{ width: `${(r.value_minor / max) * 100}%` }} />
            <span
              className="an-bar-fill ch"
              style={{ width: `${(r.value_challenged_minor / max) * 100}%` }}
            />
          </div>
          <div className="an-bar-sub">
            <b>{formatMinorCompact(r.value_challenged_minor)}</b> challenged ({pctText(r.value_rate)}) ·{" "}
            {r.challenged}/{r.total} payments
          </div>
        </div>
      ))}
    </div>
  );
}

export function Analytics() {
  const [d, setD] = useState<AnalyticsResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const load = () =>
      api
        .analytics()
        .then((r) => !cancelled && setD(r))
        .catch((e) => !cancelled && setError(e instanceof Error ? e.message : String(e)));
    load();
    const t = window.setInterval(load, 4000);
    return () => {
      cancelled = true;
      window.clearInterval(t);
    };
  }, []);

  if (error) return <div className="deg">Could not load analytics: {error}</div>;
  if (!d) return <div className="card an-load">Aggregating decisions…</div>;

  const placeMax = Math.max(...d.places.map((p) => p.value_minor), 1);

  return (
    <div>
      <div className="ph">
        <div>
          <h1>Fraud Analytics</h1>
          <p>
            Grouped from {d.decisions.toLocaleString()} decisions this instance actually made. Ranked by money at
            risk, not payment count — a thousand ₹2 probes are not the problem a bank has.
          </p>
        </div>
      </div>

      <div className="an-top">
        <div className="an-kpi">
          <div className="an-kpi-l">Decisions analysed</div>
          <div className="an-kpi-v mny">{d.decisions.toLocaleString()}</div>
        </div>
        <div className="an-kpi">
          <div className="an-kpi-l">Challenged</div>
          <div className="an-kpi-v mny">{d.challenged.toLocaleString()}</div>
          <div className="an-kpi-s">
            {d.decisions ? pctText(d.challenged / d.decisions) : "—"} of payments
          </div>
        </div>
        <div className="an-kpi">
          <div className="an-kpi-l">Value flowing</div>
          <div className="an-kpi-v mny">{formatMinorCompact(d.value_minor)}</div>
        </div>
        <div className="an-kpi hi">
          <div className="an-kpi-l">Value stopped or challenged</div>
          <div className="an-kpi-v mny">{formatMinorCompact(d.value_challenged_minor)}</div>
          <div className="an-kpi-s">
            {d.value_minor ? pctText(d.value_challenged_minor / d.value_minor) : "—"} of the money
          </div>
        </div>
      </div>

      <div className="an-sec">Where it is happening</div>
      {d.places.length === 0 ? (
        <div className="an-empty">
          No decisions carry a location yet. Start background traffic or launch an attack on Command Centre.
        </div>
      ) : (
        <div className="an-map">
          {d.places.map((p) => {
            const h = heat(p.value_minor, placeMax);
            return (
              <div key={p.key} className="an-cell" style={{ ["--h" as string]: h }}>
                <div className="an-cell-top">
                  <span className="an-cell-city">{p.label}</span>
                  <span className="an-cell-rate mny">{pctText(p.value_rate)}</span>
                </div>
                <div className="an-cell-v mny">{formatMinorCompact(p.value_challenged_minor)}</div>
                <div className="an-cell-s">
                  challenged of {formatMinorCompact(p.value_minor)} · {p.challenged}/{p.total} payments
                </div>
              </div>
            );
          })}
        </div>
      )}
      <p className="an-note">
        Shade is the money exposed in that place; the percentage is how much of it was stopped or challenged.
        Location is the payer's home cell as recorded on the transaction.
      </p>

      <div className="an-grid">
        <div className="card an-panel">
          <div className="ch">
            <h2>By attack type</h2>
            <span className="badge">RECOVERED</span>
          </div>
          <Bars rows={d.typologies} empty="No traffic yet." />
          <p className="an-note in">{TIER_NOTE}</p>
        </div>

        <div className="card an-panel">
          <div className="ch">
            <h2>By amount band</h2>
          </div>
          <Bars rows={d.bands} empty="No traffic yet." />
          <p className="an-note in">
            The bands are where the typologies live: probes, everyday spend, the impersonation-scam band, and
            the cash-out tier.
          </p>
        </div>

        <div className="card an-panel">
          <div className="ch">
            <h2>Which detector caught it</h2>
          </div>
          <Bars rows={d.signals} empty="No detector has fired yet." />
          <p className="an-note in">
            Three of the four never see a fraud label. A detector contributing nothing shows zero here rather
            than in a footnote.
          </p>
        </div>

        <div className="card an-panel">
          <div className="ch">
            <h2>By rail</h2>
          </div>
          <Bars rows={d.rails} empty="No traffic yet." />
        </div>
      </div>

      <div className="an-sec">By hour of day</div>
      {d.hours.length === 0 ? (
        <div className="an-empty">No traffic yet.</div>
      ) : (
        <div className="an-hours">
          {d.hours.map((h) => {
            const max = Math.max(...d.hours.map((x) => x.value_minor), 1);
            return (
              <div key={h.key} className="an-hour" title={`${h.label} — ${h.challenged}/${h.total} challenged`}>
                <div className="an-hour-track">
                  <span className="an-hour-fill" style={{ height: `${(h.value_minor / max) * 100}%` }} />
                  <span
                    className="an-hour-fill ch"
                    style={{ height: `${(h.value_challenged_minor / max) * 100}%` }}
                  />
                </div>
                <div className="an-hour-l">{h.key}</div>
              </div>
            );
          })}
        </div>
      )}

      <p className="an-note">{d.note}</p>
    </div>
  );
}
