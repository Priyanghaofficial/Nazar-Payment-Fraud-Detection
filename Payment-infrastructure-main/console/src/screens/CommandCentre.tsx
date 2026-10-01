import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api/client";
import type { JudgeSessionResponse, LatencyResponse, SimStatus } from "../api/types";
import { ExplainPanel } from "../components/ExplainPanel";
import { formatMinor, formatMinorCompact, formatMs, formatTimeMs, truncateMid } from "../lib/format";
import { useDecisionStream } from "../lib/useDecisionStream";

/** The room screen.
 *
 * Everything on it is live: the feed is the real SSE decision stream, the counters are
 * computed from the decisions actually in it, and the attack campaigns push real payment
 * events through the same POST /v1/decide path a phone does. Nothing here is pre-recorded,
 * which is why an attack that the system fails to catch would show up as exactly that. */

const ACTION_TONE: Record<string, string> = {
  ALLOW: "ok",
  ALLOW_MONITOR: "ok",
  STEP_UP: "warn",
  STEP_UP_INTERSTITIAL: "warn",
  HOLD: "stop",
  CAP: "hold",
  BLOCK: "stop",
};

const ACTION_SHORT: Record<string, string> = {
  ALLOW: "Allowed",
  ALLOW_MONITOR: "Watched",
  STEP_UP: "Verify",
  STEP_UP_INTERSTITIAL: "Warned",
  HOLD: "Held",
  CAP: "Capped",
  BLOCK: "Blocked",
};

/** The phone's five beats, mirrored on the big screen so the room can see where the judge is
 *  in the story without the presenter having to say it. */
const JUDGE_BEATS: { key: string; label: string }[] = [
  { key: "home", label: "Everyday" },
  { key: "call", label: "Approach" },
  { key: "scam_pay", label: "About to pay" },
  { key: "warned", label: "Warned" },
  { key: "reveal", label: "Proof" },
];

// The phone reports more acts than the track shows. Mapping every act onto a beat keeps the
// track from blanking out mid-story — "chai_done" and "cancelled" are real beats to a judge
// watching, even though they are not their own dot.
const ACT_TO_BEAT: Record<string, number> = {
  intro: 0, home: 0, chai_done: 0,
  call: 1,
  scam_pay: 2,
  warned: 3, cancelled: 3, override: 3,
  reveal: 4,
};

/** Counts up to a target so a number that jumps by 40 reads as motion rather than a redraw. */
function useTicker(target: number, ms = 450) {
  const [v, setV] = useState(target);
  const from = useRef(target);
  useEffect(() => {
    const start = performance.now();
    const a = from.current;
    let raf = 0;
    const step = (t: number) => {
      const k = Math.min(1, (t - start) / ms);
      const eased = 1 - Math.pow(1 - k, 3);
      setV(a + (target - a) * eased);
      if (k < 1) raf = requestAnimationFrame(step);
      else from.current = target;
    };
    raf = requestAnimationFrame(step);
    // requestAnimationFrame does not fire in a backgrounded tab, and a demo machine with the
    // console on a second screen is exactly that. Without this the headline number would sit
    // at its old value indefinitely — an animation detail turning into a wrong number.
    const settle = window.setTimeout(() => {
      setV(target);
      from.current = target;
    }, ms + 60);
    return () => {
      cancelAnimationFrame(raf);
      window.clearTimeout(settle);
    };
  }, [target, ms]);
  return v;
}

export function CommandCentre() {
  const { rows, connState } = useDecisionStream();
  const [sim, setSim] = useState<SimStatus | null>(null);
  const [latency, setLatency] = useState<LatencyResponse | null>(null);
  const [judge, setJudge] = useState<JudgeSessionResponse | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [resetDone, setResetDone] = useState<string | null>(null);
  const [confirmReset, setConfirmReset] = useState(false);

  useEffect(() => {
    let cancelled = false;
    async function poll() {
      try {
        const [s, l, j] = await Promise.all([
          api.simStatus(),
          api.latency(),
          api.judgeSessionActive().catch(() => ({ session: null })),
        ]);
        if (cancelled) return;
        setSim(s);
        setLatency(l);
        setJudge(j.session);
      } catch {
        // the feed is the source of truth; this strip is decoration and may fail quietly
      }
    }
    poll();
    const t = window.setInterval(poll, 700);
    return () => {
      cancelled = true;
      window.clearInterval(t);
    };
  }, []);

  const metrics = useMemo(() => {
    const challenged = rows.filter((r) => r.action !== "ALLOW" && r.action !== "ALLOW_MONITOR");
    const value = challenged.reduce((s, r) => s + (r.amount_minor ?? 0), 0);
    const now = Date.now();
    const lastMin = rows.filter((r) => now - r.decided_at_ms < 60_000).length;
    return {
      value,
      challenged: challenged.length,
      total: rows.length,
      perMin: lastMin,
      rate: rows.length ? (challenged.length / rows.length) * 100 : 0,
    };
  }, [rows]);

  const tickValue = useTicker(metrics.value);
  const tickChallenged = useTicker(metrics.challenged);
  const tickPerMin = useTicker(metrics.perMin);

  const campaign = sim?.campaign ?? null;
  const campaignLive = campaign?.running ?? false;

  async function launch(kind: string) {
    setBusy(kind);
    try {
      await api.simAttack(kind);
    } finally {
      setBusy(null);
    }
  }

  /** Back to a plain state between scenarios. Two-step on purpose: it deletes real history,
   *  and a stray click during a demo would be worse than the bloat it fixes. */
  async function doReset() {
    setBusy("reset");
    setConfirmReset(false);
    try {
      const r = await api.adminReset();
      setSelected(null);
      setResetDone(
        `Cleared ${r.decisions_deleted.toLocaleString()} decisions, ${r.redis_keys_deleted.toLocaleString()} profile keys, the graph, the novelty reservoir and the latency window. Audit chain ${r.audit_chain}.`,
      );
      setSim(await api.simStatus());
      setLatency(await api.latency());
      setJudge(null);
      window.setTimeout(() => setResetDone(null), 9000);
    } catch (e) {
      setResetDone(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }

  async function toggleTraffic() {
    setBusy("traffic");
    try {
      await api.simTraffic(sim?.traffic_running ? "stop" : "start", 2);
      setSim(await api.simStatus());
    } finally {
      setBusy(null);
    }
  }

  const judgePayer = judge?.payer_account;

  return (
    <div className="cc">
      <div className="cc-head">
        <div>
          <h1>
            Command Centre
            <span className={`cc-live ${connState === "open" ? "on" : ""}`}>
              <i />
              {connState === "open" ? "LIVE" : connState === "error" ? "STREAM DOWN" : "CONNECTING"}
            </span>
          </h1>
          <p>
            Every row below is a real payment scored by the real engine. Click any one to see exactly why it was
            decided that way.
          </p>
        </div>
        <div className="sp" />
        <button className={`pill ${sim?.traffic_running ? "dan" : "pri"}`} onClick={toggleTraffic} disabled={busy === "traffic"}>
          {sim?.traffic_running ? "Stop background traffic" : "Start background traffic"}
        </button>
        {confirmReset ? (
          <>
            <span className="cc-reset-q">Clear all history and counters?</span>
            <button className="pill dan" onClick={doReset} disabled={busy === "reset"}>
              {busy === "reset" ? "Resetting…" : "Yes, reset"}
            </button>
            <button className="pill gh" onClick={() => setConfirmReset(false)}>
              Cancel
            </button>
          </>
        ) : (
          <button className="pill gh" onClick={() => setConfirmReset(true)} disabled={busy !== null}
            title="Return the instance to a plain state so the next scenario starts clean">
            Reset for next scenario
          </button>
        )}
      </div>

      {resetDone && <div className="cc-reset-done">{resetDone}</div>}

      {judge && judgePayer && (
        <div className="cc-judge">
          <div className="cc-judge-top">
            <span className="cc-judge-badge">
              <i />
              PHONE CONNECTED
            </span>
            <span className="cc-judge-who">
              <b>{judge.scenario?.persona_name ?? "Customer"}</b>
              <span className="mn">{truncateMid(judgePayer, 14, 6)}</span>
            </span>
            <div className="sp" />
            {judge.scenario?.scam_label && (
              <span className="cc-judge-scam">approached by “{judge.scenario.scam_label}”</span>
            )}
          </div>
          <div className="cc-judge-track">
            {JUDGE_BEATS.map((b, i) => {
              const at = ACT_TO_BEAT[judge.act] ?? 0;
              return (
                <span key={b.key} className={`cc-judge-beat ${i === at ? "on" : i < at ? "done" : ""}`}>
                  {b.label}
                </span>
              );
            })}
          </div>
          <div className="cc-judge-now">
            <b>{judge.act_label || "…"}</b>
            {judge.last_action && (
              <span className={`cc-judge-act t-${ACTION_TONE[judge.last_action] ?? "ok"}`}>
                {ACTION_SHORT[judge.last_action] ?? judge.last_action}
              </span>
            )}
            {judge.last_ref && <span className="cc-judge-ref mn">{truncateMid(judge.last_ref, 18, 8)}</span>}
          </div>
        </div>
      )}

      {campaign && campaignLive && (
        <div className="cc-attack">
          <div className="cc-attack-top">
            <span className="cc-attack-badge">
              <i />
              ATTACK IN PROGRESS
            </span>
            <span className="cc-attack-label">{campaign.label}</span>
            <div className="sp" />
            <span className="cc-attack-count">
              {campaign.sent}/{campaign.total} sent · <b>{campaign.challenged}</b> stopped
            </span>
            <button className="pill gh sm" onClick={() => api.simAttackStop()}>
              Abort
            </button>
          </div>
          <div className="cc-attack-bar">
            <span style={{ width: `${(campaign.sent / Math.max(1, campaign.total)) * 100}%` }} />
          </div>
          <div className="cc-attack-money">
            <span className="cc-attack-money-v mny">
              {formatMinorCompact(campaign.value_stopped_minor)} of{" "}
              {formatMinorCompact(campaign.value_at_risk_minor)}
            </span>
            <span className="cc-attack-money-l">
              of the money at risk stopped
              {campaign.value_at_risk_minor > 0 && (
                <b>
                  {" "}
                  ({Math.round((campaign.value_stopped_minor / campaign.value_at_risk_minor) * 100)}%)
                </b>
              )}
            </span>
          </div>
          <div className="cc-attack-narr">{campaign.narrative}</div>
          {campaign.sent > 0 &&
            campaign.challenged / campaign.sent < 0.5 &&
            campaign.value_at_risk_minor > 0 &&
            campaign.value_stopped_minor / campaign.value_at_risk_minor > 0.8 && (
              <div className="cc-attack-econ">
                Most of these payments were deliberately let through. They are trivial probes, and
                challenging one costs more than the fraud it prevents — so the system spends the
                customer's patience on the payment that carries the money, not on the noise.
              </div>
            )}
        </div>
      )}

      <div className="cc-metrics">
        <div className="cc-met big">
          <div className="lb">Value stopped or challenged</div>
          <div className="vl mny">{formatMinorCompact(Math.round(tickValue))}</div>
          <div className="ft">across {Math.round(tickChallenged)} interventions in this window</div>
        </div>
        <div className="cc-met">
          <div className="lb">Decisions / minute</div>
          <div className="vl mny">{Math.round(tickPerMin)}</div>
          <div className="ft">{metrics.total} in view</div>
        </div>
        <div className="cc-met">
          <div className="lb">Decision time p99</div>
          <div className="vl mny">
            {latency ? latency.p99.toFixed(1) : "—"}
            <span className="un"> ms</span>
          </div>
          <div className="ft">{latency ? `p50 ${latency.p50.toFixed(1)} ms · n=${latency.n}` : "no traffic yet"}</div>
        </div>
        <div className="cc-met">
          <div className="lb">Intervention rate</div>
          <div className="vl mny">
            {metrics.rate.toFixed(1)}
            <span className="un">%</span>
          </div>
          <div className="ft">friction is a cost, not a score</div>
        </div>
      </div>

      <div className="cc-attacks">
        <span className="cc-attacks-lb">Launch a live attack:</span>
        {(sim?.campaigns_available ?? []).map((c) => (
          <button
            key={c.kind}
            className="cc-atk"
            disabled={campaignLive || busy !== null}
            onClick={() => launch(c.kind)}
            title={`${c.description}\n\nExpected: ${c.expect}`}
          >
            {c.label}
          </button>
        ))}
      </div>

      <div className="cc-split">
        <div className="cc-feed card">
          <div className="ch">
            <h2>Live decisions</h2>
            <span className="badge">{rows.length}</span>
            <div className="sp" />
            <span className="sub">newest first</span>
          </div>
          <div className="cc-rows">
            {rows.length === 0 && (
              <div className="cc-idle">
                Nothing has come through yet. Start background traffic, launch an attack, or have someone scan the QR
                on the Payer App screen.
              </div>
            )}
            {rows.map((r) => {
              const tone = ACTION_TONE[r.action] ?? "ok";
              const isJudge = judgePayer && r.debtor_account === judgePayer;
              return (
                <button
                  key={r._id}
                  className={`cc-row t-${tone} ${r._fresh ? "fresh" : ""} ${selected === r.end_to_end_id ? "sel" : ""} ${
                    isJudge ? "judge" : ""
                  }`}
                  onClick={() => setSelected(r.end_to_end_id)}
                >
                  <span className="cc-time ts">{formatTimeMs(r.decided_at_ms)}</span>
                  <span className="cc-parties">
                    <span className="cc-acct mn">{truncateMid(r.debtor_account, 10, 4)}</span>
                    <span className="cc-arrow">→</span>
                    <span className="cc-acct mn">{truncateMid(r.creditor_account, 10, 4)}</span>
                    {r.source && r.source !== "api" && <span className={`cc-src s-${r.source}`}>{r.source}</span>}
                  </span>
                  <span className="cc-why">{r.top_reason}</span>
                  <span className="cc-amt mny">{formatMinor(r.amount_minor)}</span>
                  <span className={`cc-act t-${tone}`}>{ACTION_SHORT[r.action] ?? r.action}</span>
                  <span className="cc-ms ts">{formatMs(r.total_ms, 1)}</span>
                </button>
              );
            })}
          </div>
        </div>

        <div className="cc-side">
          {selected ? (
            <ExplainPanel id={selected} onClose={() => setSelected(null)} />
          ) : (
            <div className="card cc-hint">
              <div className="ch">
                <h2>Why did we decide that?</h2>
              </div>
              <div className="cc-hint-body">
                <p>Click any payment in the feed.</p>
                <p>
                  You'll get the ranked evidence in plain English, the verdict of each detector that looked at it, the
                  expected-cost arithmetic that chose the action, and a live re-execution proving the decision
                  reproduces bit-for-bit with its audit hash intact.
                </p>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
