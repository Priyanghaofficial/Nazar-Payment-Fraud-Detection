import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { CoverageResponse, ModelMetricsResponse } from "../api/types";

/** Every number about the model, with its provenance attached.
 *
 * The tiers are the point. Performance measured against our own generator's labels is real
 * evaluation of a real pipeline, but the labels are ours — calling that a detection rate
 * would be dishonest. Performance on an externally labelled dataset validates the method on
 * data nobody here produced. Latency is measured on this running process. Each is shown as
 * what it is, and any metric whose source file is missing renders as absent rather than
 * silently defaulting to something flattering. */

function Tier({ tier }: { tier: "MEASURED" | "RECOVERED" | "MODELLED" }) {
  return <span className={`me-tier t-${tier}`}>[{tier}]</span>;
}

function Stat({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="me-stat">
      <div className="me-stat-l">{label}</div>
      <div className="me-stat-v mny">{value}</div>
      {sub && <div className="me-stat-s">{sub}</div>}
    </div>
  );
}

const n = (v: unknown, d = 4): string =>
  typeof v === "number" && Number.isFinite(v) ? v.toFixed(d) : "—";

export function ModelEvidence() {
  const [m, setM] = useState<ModelMetricsResponse | null>(null);
  const [cov, setCov] = useState<CoverageResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .modelMetrics()
      .then(setM)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)));
    api.modelCoverage().then(setCov).catch(() => setCov(null));
  }, []);

  if (error) return <div className="deg">Could not load metrics: {error}</div>;
  if (!m) return <div className="card" style={{ padding: 20, color: "var(--ink4)" }}>Loading…</div>;

  const train = (m.training ?? {}) as Record<string, any>;
  const ext = (m.external_validation ?? {}) as Record<string, any>;
  const prev = (m.prevalence ?? {}) as Record<string, any>;
  const byTypology: Record<string, any> = train.per_typology_recall ?? {};
  const ops: any[] = train.operating_points ?? [];
  const ablation: any[] = train.ablation ?? [];

  return (
    <div>
      <div className="ph">
        <div>
          <h1>Model Evidence</h1>
          <p>
            Four detectors, not one model — and every number below carries the tier it was measured at. Nothing here
            is a slide value; delete the source file and the number disappears.
          </p>
        </div>
      </div>

      <div className="me-arg">
        <div className="me-arg-lead">
          A supervised model cannot generalise to fraud it has never seen. So we did not build the system around one.
        </div>
        <p>
          Ours is one of four detectors, and it is the only one that needs a fraud label to work. We measured exactly
          where it is strong and where it is weak — below, in public — and then made sure the attacks it is weakest on
          are precisely the ones the other three catch structurally.
        </p>
        <div className="me-layers">
          <div className="me-layer">
            <div className="me-layer-h">
              <span className="me-layer-n">1</span> Written rules &amp; regulatory rails
              <span className="me-nolabel">no labels</span>
            </div>
            <p>
              Deterministic and legally required. A beneficiary cooling period does not care whether we have seen the
              attack before — it is the law, evaluated in CEL, versioned and four-eyes approved.
            </p>
          </div>
          <div className="me-layer">
            <div className="me-layer-h">
              <span className="me-layer-n">2</span> Supervised model
              <span className="me-needslabel">needs labels</span>
            </div>
            <p>
              Gradient-boosted trees over 30 registry features, calibrated so a stated probability means what it says.
              Excellent on patterns resembling history. Blind, by construction, to anything genuinely new.
            </p>
          </div>
          <div className="me-layer">
            <div className="me-layer-h">
              <span className="me-layer-n">3</span> Behavioural anomaly detector
              <span className="me-nolabel">no labels</span>
            </div>
            <p>
              Conformal k-NN over recent traffic. It never learns what fraud looks like — only what normal looks like —
              so a first-of-its-kind attack is unusual to it on day one, with a calibrated p-value rather than a hunch.
            </p>
          </div>
          <div className="me-layer">
            <div className="me-layer-h">
              <span className="me-layer-n">4</span> Beneficiary network analysis
              <span className="me-nolabel">no labels</span>
            </div>
            <p>
              Structural. A collection account fed by many first-time payers sharing devices looks like a ring whatever
              the amounts are — and a busy legitimate merchant does not, which we test for explicitly.
            </p>
          </div>
        </div>
        <div className="me-arg-close">
          Three of the four never see a fraud label. That is the generalisation argument, and the ablation and coverage
          numbers below are the evidence for it — not the claim itself.
        </div>
      </div>

      <div className="me-sec">
        <Tier tier="MEASURED" /> Live decision latency, this process
      </div>
      <div className="me-stats">
        <Stat label="p50" value={`${m.live_latency.p50.toFixed(2)} ms`} />
        <Stat label="p99" value={`${m.live_latency.p99.toFixed(2)} ms`} />
        <Stat label="p99.9" value={`${m.live_latency.p999.toFixed(2)} ms`} />
        <Stat label="max" value={`${m.live_latency.max.toFixed(2)} ms`} />
        <Stat label="decisions measured" value={String(m.live_latency.n)} sub="since this process started" />
      </div>

      <div className="me-sec">
        <Tier tier="MEASURED" /> External validation — ULB credit-card fraud, real labelled data
      </div>
      {m.external_validation ? (
        <>
          <div className="me-stats">
            <Stat label="ROC-AUC" value={n(ext.roc_auc)} />
            <Stat label="PR-AUC" value={n(ext.pr_auc)} />
            <Stat label="test rows" value={String(ext.n_test ?? "—")} />
            <Stat label="fraud in test" value={String(ext.n_fraud_test ?? "—")} />
          </div>
          <p className="me-note">{ext.note}</p>
        </>
      ) : (
        <div className="me-empty">Not run — py/eval/output/ulb_validation_result.json is absent.</div>
      )}

      <div className="me-sec">
        <Tier tier="RECOVERED" /> Our own pipeline, time-forward held-out split
        <span className="me-sec-note">
          real evaluation, our generator's labels — this measures the pipeline, not a real-world detection rate
        </span>
      </div>
      {m.training ? (
        <>
          <div className="me-stats">
            <Stat label="ROC-AUC" value={n(train.roc_auc)} />
            <Stat label="PR-AUC" value={n(train.pr_auc)} />
            <Stat label="Brier" value={n(train.brier)} />
            <Stat label="ECE after calibration" value={n(train.ece)} sub="10 bins" />
            <Stat label="test rows" value={String(train.n_test ?? "—")} sub={`${train.n_test_pos ?? 0} fraud`} />
          </div>

          {ops.length > 0 && (
            <>
              <div className="me-sub">Operating points</div>
              <table className="me-table">
                <thead>
                  <tr>
                    <th>Threshold</th>
                    <th className="r">Precision</th>
                    <th className="r">Recall</th>
                    <th className="r">F1</th>
                    <th className="r">Alerts / 10k</th>
                  </tr>
                </thead>
                <tbody>
                  {ops.map((o: any, i: number) => (
                    <tr key={i}>
                      <td className="mn">{n(o.threshold, 3)}</td>
                      <td className="r">{n(o.precision, 3)}</td>
                      <td className="r">{n(o.recall, 3)}</td>
                      <td className="r">{n(o.f1, 3)}</td>
                      <td className="r">{n(o.alerts_per_10k, 1)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          )}

          {Object.keys(byTypology).length > 0 && (
            <>
              <div className="me-sub">
                Recall by attack type
                <span className="me-sec-note">the honest answer to "how does it generalise across payment behaviour?"</span>
              </div>
              <div className="me-typs">
                {Object.entries(byTypology).map(([k, v]: [string, any]) => (
                  <div key={k} className="me-typ">
                    <div className="me-typ-top">
                      <span>{k.replace(/_/g, " ")}</span>
                      <span className="mny">{n(v.recall, 2)}</span>
                    </div>
                    <div className="me-typ-bar">
                      <span style={{ width: `${Math.max(2, (v.recall ?? 0) * 100)}%` }} />
                    </div>
                    <div className="me-typ-n">{v.n} examples</div>
                  </div>
                ))}
              </div>
            </>
          )}

          {ablation.length > 0 && (
            <>
              <div className="me-sub">
                Does each layer earn its place?
                <span className="me-sec-note">same test set, components removed one at a time</span>
              </div>
              <table className="me-table">
                <thead>
                  <tr>
                    <th>Configuration</th>
                    <th className="r">Recall</th>
                    <th className="r">Precision</th>
                    <th className="r">PR-AUC</th>
                  </tr>
                </thead>
                <tbody>
                  {ablation.map((a: any, i: number) => (
                    <tr key={i}>
                      <td>{a.name}</td>
                      <td className="r">{n(a.recall, 3)}</td>
                      <td className="r">{n(a.precision, 3)}</td>
                      <td className="r">{n(a.pr_auc, 3)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          )}
        </>
      ) : (
        <div className="me-empty">
          Not generated yet — run <span className="mn">make eval</span> to produce py/training/output/metrics.json.
        </div>
      )}

      <div className="me-sec">
        <Tier tier="MEASURED" /> Defence in depth, measured on live attacks
        <span className="me-sec-note">which detector actually caught what, from decisions this system really made</span>
      </div>
      {cov && (cov.campaigns ?? []).length > 0 ? (
        <>
          <div className="me-covs">
            {(cov.campaigns ?? []).map((c) => (
              <div key={c.kind} className="me-cov">
                <div className="me-cov-h">
                  <span className="me-cov-label">{c.label}</span>
                  <span className="me-cov-rate mny">{Math.round(c.value_catch_rate * 100)}%</span>
                </div>
                <div className="me-cov-sub">
                  of the money at risk was stopped · {c.challenged} of {c.decisions} payments challenged
                </div>
                {c.detectors.map((d) => (
                  <div key={d.detector} className="me-cov-d">
                    <span className={`me-cov-name ${d.needs_labels ? "lbl" : ""}`}>{d.detector}</span>
                    <span className="me-cov-bar">
                      <span style={{ width: `${Math.round(d.rate * 100)}%` }} />
                    </span>
                    <span className="me-cov-pct">{Math.round(d.rate * 100)}%</span>
                  </div>
                ))}
              </div>
            ))}
          </div>
          <p className="me-note">{cov.why_it_matters}</p>
          <p className="me-note">{cov.read_the_value_rate}</p>
          <p className="me-note">{cov.note}</p>
        </>
      ) : (
        <div className="me-empty">
          No attack campaigns have run yet on this instance. Launch one from the Command Centre and this fills in with
          real measurements.
        </div>
      )}

      <div className="me-sec">Bundle in force right now</div>
      <div className="me-vers">
        <span><b>model</b> <span className="mn">{m.model_bundle}</span></span>
        <span><b>policy</b> <span className="mn">{m.policy_version}</span></span>
        <span><b>rules</b> <span className="mn">{m.rules_version}</span></span>
        <span><b>prevalence</b> <span className="mn">{prev.version ?? "—"}</span></span>
        <span>
          <b>natural prevalence</b> <span className="mn">{n(prev.natural_prevalence, 6)}</span>
        </span>
      </div>
    </div>
  );
}
