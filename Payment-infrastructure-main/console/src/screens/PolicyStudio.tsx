import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { LivePolicyResponse, PolicyTuneRequest, PolicyTuneResponse } from "../api/types";
import { formatMinor, formatMinorCompact, truncateMid } from "../lib/format";

/** Live policy tuning with an honest preview.
 *
 * Moving a control here does nothing to production until "Make live" is pressed. What it
 * does immediately is re-price real, recently-decided payments under the candidate policy
 * and show precisely which ones would have gone differently — using the model probability,
 * amount and fired rails actually recorded at decision time, none of which depend on the
 * policy being changed. That's what makes the preview a measurement rather than a guess. */

interface Knob {
  key: keyof PolicyTuneRequest;
  label: string;
  help: string;
  min: number;
  max: number;
  step: number;
  money?: boolean;
  pct?: boolean;
}

const KNOBS: Knob[] = [
  {
    key: "hold_friction_minor",
    label: "Cost of holding a payment",
    help: "What it costs us operationally to stop a payment and make someone review it. Raise it and the system holds less.",
    min: 0, max: 400000, step: 5000, money: true,
  },
  {
    key: "interstitial_friction_minor",
    label: "Cost of warning the customer",
    help: "The cost of interrupting someone with a scam warning they must confirm through.",
    min: 0, max: 40000, step: 500, money: true,
  },
  {
    key: "margin_minor",
    label: "Value of a customer we annoy",
    help: "The business we lose when a genuine customer abandons a payment because we got in the way.",
    min: 0, max: 50000, step: 500, money: true,
  },
  {
    key: "loss_given_fraud_upi",
    label: "Share of a UPI fraud we actually eat",
    help: "How much of a fraudulent UPI payment the bank never recovers. Higher means fraud hurts more, so the system intervenes sooner.",
    min: 0, max: 1, step: 0.05, pct: true,
  },
  {
    key: "hold_stop_prob",
    label: "How often holding actually stops the fraud",
    help: "If holding a payment rarely prevents the loss, the system stops choosing it.",
    min: 0, max: 1, step: 0.05, pct: true,
  },
];

export function PolicyStudio() {
  const [live, setLive] = useState<LivePolicyResponse | null>(null);
  const [values, setValues] = useState<Record<string, number>>({});
  const [result, setResult] = useState<PolicyTuneResponse | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function refresh() {
    try {
      const l = await api.livePolicy();
      setLive(l);
      const econ = (l.policy as Record<string, any>).Economics ?? (l.policy as Record<string, any>).economics ?? {};
      const friction = econ.FrictionCostMinor ?? econ.friction_cost_minor ?? {};
      const stop = econ.StopProb ?? econ.stop_prob ?? {};
      const lgf = econ.LossGivenFraud ?? econ.loss_given_fraud ?? {};
      setValues({
        hold_friction_minor: friction.hold ?? 90000,
        interstitial_friction_minor: friction.step_up_interstitial ?? 4000,
        margin_minor: econ.MarginMinor ?? econ.margin_minor ?? 3000,
        loss_given_fraud_upi: lgf.UPI ?? 0.95,
        hold_stop_prob: stop.hold ?? 0.97,
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  async function preview(apply: boolean) {
    setBusy(true);
    setError(null);
    try {
      const r = await api.tunePolicy({ ...values, apply, replay_limit: 300 } as PolicyTuneRequest);
      setResult(r);
      if (apply) await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function reset() {
    setBusy(true);
    try {
      await api.resetPolicy();
      setResult(null);
      await refresh();
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <div className="ph">
        <div>
          <h1>Policy Studio</h1>
          <p>
            Risk appetite is a set of prices, not a threshold. Change what being wrong costs and every subsequent
            decision re-prices immediately — after you've seen exactly what it would have done to real traffic.
          </p>
        </div>
        <div className="sp" />
        {live && (
          <span className={`st ${live.is_tuned ? "s-wn" : "s-ok"}`}>
            <i />
            {live.is_tuned ? `tuned: ${live.policy.version}` : `approved bundle ${live.base_version}`}
          </span>
        )}
      </div>

      {error && <div className="deg" style={{ marginBottom: 14 }}>{error}</div>}

      <div className="ps-split">
        <div className="card">
          <div className="ch">
            <h2>What being wrong costs</h2>
          </div>
          <div className="ps-knobs">
            {KNOBS.map((k) => {
              const v = values[k.key as string] ?? 0;
              return (
                <div key={k.key as string} className="ps-knob">
                  <div className="ps-knob-top">
                    <span className="ps-knob-label">{k.label}</span>
                    <span className="ps-knob-val mny">
                      {k.money ? formatMinor(v) : k.pct ? `${Math.round(v * 100)}%` : v}
                    </span>
                  </div>
                  <input
                    type="range"
                    min={k.min}
                    max={k.max}
                    step={k.step}
                    value={v}
                    onChange={(e) =>
                      setValues((prev) => ({ ...prev, [k.key as string]: Number(e.target.value) }))
                    }
                  />
                  <div className="ps-knob-help">{k.help}</div>
                </div>
              );
            })}
          </div>
          <div className="ps-actions">
            <button className="pill" onClick={() => preview(false)} disabled={busy}>
              {busy ? "Replaying…" : "Preview against real traffic"}
            </button>
            <button className="pill pri" onClick={() => preview(true)} disabled={busy}>
              Make live
            </button>
            <div className="sp" />
            <button className="pill gh" onClick={reset} disabled={busy}>
              Reset to approved
            </button>
          </div>
        </div>

        <div className="card">
          <div className="ch">
            <h2>What that would have changed</h2>
            {result && <span className="badge">{result.evaluated_against} decisions replayed</span>}
          </div>
          {!result && (
            <div className="ps-empty">
              Move a control, then preview. Nazar re-runs real recent decisions under the candidate policy and reports
              every one that would have gone differently.
            </div>
          )}
          {result && (
            <>
              <div className="ps-stats">
                <div className="ps-stat">
                  <div className="v" style={{ color: "var(--warn)" }}>{result.stricter}</div>
                  <div className="l">would now be challenged</div>
                </div>
                <div className="ps-stat">
                  <div className="v" style={{ color: "var(--ok)" }}>{result.looser}</div>
                  <div className="l">would now go through</div>
                </div>
                <div className="ps-stat">
                  <div className="v mny">{formatMinorCompact(result.value_newly_challenged_minor)}</div>
                  <div className="l">value newly stopped</div>
                </div>
                <div className="ps-stat">
                  <div className="v mny">{formatMinorCompact(result.value_newly_released_minor)}</div>
                  <div className="l">value newly released</div>
                </div>
              </div>

              {result.applied && (
                <div className="ps-applied">
                  Live now as <span className="mn">{result.policy_version}</span>. Every decision from this point
                  stamps that version, so the audit trail never claims the approved bundle produced it.
                </div>
              )}

              <div className="ps-flips">
                {(result.flips ?? []).length === 0 && (
                  <div className="ps-empty">Not one decision changes under this policy.</div>
                )}
                {(result.flips ?? []).slice(0, 40).map((f) => (
                  <div key={f.end_to_end_id} className={`ps-flip d-${f.direction}`}>
                    <span className="mn">{truncateMid(f.debtor_account, 8, 4)}</span>
                    <span className="ps-flip-arrow">→</span>
                    <span className="mn">{truncateMid(f.creditor_account, 8, 4)}</span>
                    <div className="sp" />
                    <span className="mny">{formatMinor(f.amount_minor)}</span>
                    <span className="ps-flip-change">
                      <b>{f.from}</b> → <b>{f.to}</b>
                    </span>
                  </div>
                ))}
              </div>
              <div className="ps-note">{result.note}</div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
