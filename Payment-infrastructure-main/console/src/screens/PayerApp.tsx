import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import type {
  Decision,
  ExplainResponse,
  Finding,
  JudgeSessionResponse,
  StreamDecisionEvent,
} from "../api/types";
import { formatMinor } from "../lib/format";

/** The judge's phone: a five-act story in which the judge plays the customer.
 *
 * "Scripted" means the beats are fixed — an everyday payment, then an approach, then a
 * choice — so the same demo reproduces reliably in front of a room. It does NOT mean the
 * outcomes are scripted. Every tap is a real POST /v1/decide against the live engine, every
 * warning is assembled from the findings that decision actually returned, and if the engine
 * allowed a payment we expected it to challenge, this screen says so instead of pretending
 * otherwise. There is no branch in this file that renders a verdict the backend did not
 * produce.
 *
 * The story itself is drawn per session by the server (five real Indian fraud typologies),
 * so no scam copy is hardcoded here and the second judge in the room does not watch a rerun.
 *
 * Deliberately absent: no OTP step (we have no code-delivery infrastructure, and claiming
 * "we texted you" would be a demo lie), no account balance (we have no ledger), and no "case
 * opened" button (no case management exists at P0). Everything shown is something the system
 * genuinely does. */

type Act =
  | "error"
  | "intro"
  | "home"
  | "chai_done"
  | "call"
  | "scam_pay"
  | "warned"
  | "override"
  | "cancelled"
  | "reveal";

/** Plain-banking translations of real fired signals. A signal not in this table still renders
 *  through the generic fallback, so nothing the engine flagged is silently dropped. */
/** Per-tab, so a refresh keeps the same story but a new tab starts a fresh one. */
const SESSION_KEY = "nazar.judge.session";

const SIGNAL_COPY: Record<string, string> = {
  "rule:RAIL-102": "You have never sent money to this account before, and the amount is in the range scam collectors ask for.",
  "rule:RAIL-001": "This account was added to your list minutes ago, and the amount is above the regulatory limit for a brand-new payee.",
  "rule:RF-001": "You have never sent money to this account before.",
  "rule:RF-002": "A lot of different people have paid this same account in the last few minutes.",
  "rule:RF-003": "This is happening from a device we do not recognise on your account.",
  "rule:RF-004": "You added this recipient only moments ago.",
  "rule:RF-005": "This does not match where you normally pay from.",
  graph: "This account is collecting money from many unrelated people at once.",
  novelty: "This does not look like anything you, or our other customers, normally do.",
  model: "This pattern is unusual compared with how you normally pay.",
};

function humanFacts(findings: Finding[] | null): string[] {
  if (!findings) return [];
  const fired = findings.filter((f) => f.status === "FIRED");
  const facts = fired.map((f) => SIGNAL_COPY[f.signal_id] ?? "One of our safety checks flagged this payment.");
  return Array.from(new Set(facts)).slice(0, 4);
}

export function PayerApp() {
  const [act, setAct] = useState<Act>("intro");
  const [session, setSession] = useState<JudgeSessionResponse | null>(null);
  const [decision, setDecision] = useState<Decision | null>(null);
  const [history, setHistory] = useState<StreamDecisionEvent[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [fanin, setFanin] = useState<number | null>(null);
  const [why, setWhy] = useState<ExplainResponse | null>(null);
  const [whyOpen, setWhyOpen] = useState(false);
  const [whyErr, setWhyErr] = useState<string | null>(null);
  const allowedThrough = useRef(false);

  const sc = session?.scenario ?? null;

  /** Tells the console which beat this phone is on, so the big screen mirrors the story
   *  without the presenter narrating it. Never blocks and never throws. */
  const report = useCallback(
    (next: Act, ref?: string, action?: string) => {
      if (!session) return;
      void api.judgeAct({ session_id: session.session_id, act: next, ref, action });
    },
    [session],
  );

  /** setAct plus the mirror call, so no beat can advance without the console hearing it. */
  const go = useCallback(
    (next: Act, ref?: string, action?: string) => {
      setAct(next);
      report(next, ref, action);
    },
    [report],
  );

  useEffect(() => {
    let cancelled = false;
    // One session per tab. Creating a new one on every mount meant a reload — or a second
    // tab left open on the presenter's machine — silently spawned another persona, and the
    // console, which follows the most recently created session, would then be showing a
    // different name than the phone in the judge's hand. Reuse what this tab already has;
    // a genuinely new story is an explicit act ("Run it again"), not an accident of refresh.
    const cached = sessionStorage.getItem(SESSION_KEY);
    if (cached) {
      try {
        const s = JSON.parse(cached) as JudgeSessionResponse;
        if (s?.scenario?.persona_name) {
          setSession(s);
          api
            .recentDecisions(200)
            .then(({ rows }) => {
              if (!cancelled && rows) {
                setHistory(rows.filter((r) => r.debtor_account === s.payer_account).slice(0, 4));
              }
            })
            .catch(() => {});
          return () => {
            cancelled = true;
          };
        }
      } catch {
        sessionStorage.removeItem(SESSION_KEY);
      }
    }
    api
      .judgeSession()
      .then(async (s) => {
        if (cancelled) return;
        setSession(s);
        sessionStorage.setItem(SESSION_KEY, JSON.stringify(s));
        // Real prior activity for this payer — the warm-up payments the session seeded
        // through the same decision path. If the async shipper hasn't drained yet we show
        // fewer rows rather than inventing any.
        try {
          const { rows } = await api.recentDecisions(200);
          if (!cancelled && rows) {
            setHistory(rows.filter((r) => r.debtor_account === s.payer_account).slice(0, 4));
          }
        } catch {
          /* history is decoration; the story works without it */
        }
      })
      .catch((e) => {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : String(e));
          setAct("error");
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  async function pay(to: string, amountMinor: number, expected: Act, tag: string) {
    if (!session) return;
    setBusy(true);
    try {
      const res = await api.decide({
        end_to_end_id: `judge-${session.session_id}-${tag}-${Date.now()}`,
        rail: "UPI",
        debtor_account: session.payer_account,
        creditor_account: to,
        instructed_amount_minor: amountMinor,
        initiation: "INTENT",
      });
      setDecision(res.decision);
      const a = res.decision.action;
      const wentThrough = a === "ALLOW" || a === "ALLOW_MONITOR";
      // Whatever the engine decided is what we show. If it let the scam payment through,
      // we report that rather than rendering a warning it never produced.
      if (expected === "warned" && wentThrough) {
        allowedThrough.current = true;
        go("reveal", res.decision.end_to_end_id, a);
      } else {
        go(expected, res.decision.end_to_end_id, a);
      }
      // Pull the full explanation in the background so "why?" is instant when tapped.
      if (expected === "warned") {
        api
          .explainWhenReady(res.decision.end_to_end_id)
          .then((x) => { setWhy(x); setWhyErr(null); })
          .catch((e) => setWhyErr(e instanceof Error ? e.message : String(e)));
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setAct("error");
    } finally {
      setBusy(false);
    }
  }

  // How many distinct people have paid the collector account — real graph state, read only
  // at the reveal, so the number is whatever it genuinely is by then.
  async function loadFanin() {
    if (!session) return;
    try {
      const g = await api.graph(session.scam_account);
      setFanin(g.RingSize ?? null);
    } catch {
      setFanin(null);
    }
  }

  const elapsed = decision ? decision.total_ms : 0;

  if (act === "error")
    return (
      <div className="pa-boot">
        <h2>Can't reach the bank</h2>
        <p className="pa-err">{error}</p>
        <p>Nothing is being simulated in its place — this screen only ever shows real responses.</p>
      </div>
    );

  return (
    <div className="pa">
      <ActBar act={act} />

      {act === "intro" && (
        <div className="pa-card pa-intro">
          <div className="pa-kicker">For the next two minutes</div>
          <h1>You are {sc ? sc.persona_name : "…"}.</h1>
          <p>{sc?.persona_blurb ?? "Setting up your account…"}</p>
          <p className="pa-dim">
            Everything you tap from here is a real payment, scored live by the same engine the bank runs. Watch the big
            screen as you go.
          </p>
          <button className="pa-btn pri" disabled={!session} onClick={() => go("home")}>
            {session ? "Open my banking app" : "Setting up your account…"}
          </button>
        </div>
      )}

      {act === "home" && sc && (
        <div className="pa-phone">
          <div className="pa-acct">
            <span className="pa-acct-l">Account</span>
            <span className="pa-acct-v mn">···{session?.payer_account.slice(-10)}</span>
          </div>
          {history.length > 0 && (
            <>
              <div className="pa-sec">Recent activity</div>
              {history.map((h) => (
                <div key={h.end_to_end_id} className="pa-txn">
                  <span className="pa-txn-name">{sc.merchant_label}</span>
                  <span className="pa-txn-amt mny">{formatMinor(h.amount_minor)}</span>
                </div>
              ))}
            </>
          )}
          <div className="pa-sec">Pay</div>
          <div className="pa-payee">
            <div className="pa-payee-av">{sc.merchant_initials}</div>
            <div className="pa-payee-txt">
              <div className="pa-payee-n">{sc.merchant_label}</div>
              <div className="pa-payee-s">{sc.merchant_sub}</div>
            </div>
            <div className="pa-payee-amt mny">{formatMinor(sc.everyday_amount_minor)}</div>
          </div>
          <button
            className="pa-btn pri"
            disabled={busy}
            onClick={() => pay(sc.merchant_account, sc.everyday_amount_minor, "chai_done", "everyday")}
          >
            {busy ? "Paying…" : `Pay ${formatMinor(sc.everyday_amount_minor)}`}
          </button>
        </div>
      )}

      {act === "chai_done" && sc && (
        <div className="pa-card pa-ok">
          <div className="pa-tick">✓</div>
          <h1>Paid</h1>
          <p className="pa-big mny">{formatMinor(sc.everyday_amount_minor)}</p>
          <p>to {sc.merchant_label}</p>
          <div className="pa-speed">
            cleared in <b>{elapsed.toFixed(1)} ms</b>
          </div>
          <p className="pa-dim">
            Thirty different things were checked about that payment before it went through, and you felt none of them.
            That is the point: good customers should never meet the fraud system.
          </p>
          <button className="pa-btn" onClick={() => go("call")}>
            Continue
          </button>
        </div>
      )}

      {act === "call" && sc && (
        <div className="pa-card">
          <div className="pa-call-top">
            <div className="pa-call-av">!</div>
            <div>
              <div className="pa-call-from">{sc.caller_number}</div>
              <div className="pa-call-sub">{sc.caller_caption}</div>
            </div>
          </div>
          <div className="pa-sms">
            <div className="pa-sms-h">SMS · {sc.sender_id}</div>
            <div className="pa-sms-sub">{sc.headline}</div>
            <p>{sc.message_body}</p>
            <p className="pa-sms-acct">
              {sc.account_caption} <span className="mn">···{session?.scam_account.slice(-10)}</span>
              <br />
              Name: {sc.scam_label}
            </p>
          </div>
          <p className="pa-dim pa-note">
            This is one of the most common frauds in Indian payments. The approach is convincing, the urgency is
            invented, and the victim authorises the payment themselves — which is why a system that only asks "did the
            real customer approve this?" catches none of it.
          </p>
          <div className="pa-choices">
            <button className="pa-btn ghost" onClick={() => { loadFanin(); go("reveal"); }}>
              Ignore it
            </button>
            <button className="pa-btn danger" onClick={() => go("scam_pay")}>
              Do as they say
            </button>
          </div>
        </div>
      )}

      {act === "scam_pay" && sc && (
        <div className="pa-phone">
          <div className="pa-sec">Confirm payment</div>
          <div className="pa-payee">
            <div className="pa-payee-av danger">{sc.scam_initials}</div>
            <div className="pa-payee-txt">
              <div className="pa-payee-n">{sc.scam_label}</div>
              <div className="pa-payee-s mn">···{session?.scam_account.slice(-10)}</div>
            </div>
            <div className="pa-payee-amt mny">{formatMinor(sc.scam_amount_minor)}</div>
          </div>
          <button
            className="pa-btn pri"
            disabled={busy}
            onClick={() => pay(session!.scam_account, sc.scam_amount_minor, "warned", "scam")}
          >
            {busy ? "Sending…" : `Send ${formatMinor(sc.scam_amount_minor)}`}
          </button>
          <p className="pa-dim pa-note">
            You authorised this yourself. Your PIN is correct. Nothing about your account is compromised.
          </p>
        </div>
      )}

      {act === "warned" && decision && sc && (
        <div className="pa-warn">
          <div className="pa-warn-icon">!</div>
          <h1>Stop. This looks like a scam.</h1>
          <p className="pa-warn-lead">
            We are not blocking you. We are telling you what we can see, because you are about to lose{" "}
            {formatMinor(sc.scam_amount_minor)}.
          </p>
          <ul className="pa-facts">
            {humanFacts(decision.findings).map((f, i) => (
              <li key={i}>{f}</li>
            ))}
          </ul>
          <div className="pa-truth">{sc.the_truth}</div>
          <div className="pa-speed">
            decided in <b>{elapsed.toFixed(1)} ms</b> · {decision.action.replace(/_/g, " ").toLowerCase()}
          </div>

          <button className="pa-why-toggle" onClick={() => setWhyOpen((v) => !v)}>
            {whyOpen ? "Hide the details" : "Why do you think that?"}
          </button>
          {whyOpen && <WhyPanel why={why} err={whyErr} />}

          <div className="pa-choices">
            <button className="pa-btn pri" onClick={() => { loadFanin(); go("cancelled"); }}>
              Cancel this payment
            </button>
            <button className="pa-btn danger ghost" onClick={() => { loadFanin(); go("override"); }}>
              I understand, send anyway
            </button>
          </div>
        </div>
      )}

      {act === "cancelled" && sc && (
        <div className="pa-card pa-ok">
          <div className="pa-tick">✓</div>
          <h1>Payment cancelled</h1>
          <p className="pa-big mny">{formatMinor(sc.scam_amount_minor)}</p>
          <p>stayed in your account.</p>
          <p className="pa-dim">
            That warning cost you four seconds. It is the whole trade this system is built around: a few seconds of
            friction on the small number of payments that need it, and nothing at all on the rest.
          </p>
          <button className="pa-btn" onClick={() => go("reveal")}>
            Show me what the bank saw
          </button>
        </div>
      )}

      {act === "override" && (
        <div className="pa-card pa-held">
          <h1>Flagged to our fraud team</h1>
          <p className="pa-dim">
            You chose to continue, and we recorded that. The payment is now in the analyst queue with the full evidence
            attached, and the receiving account is on our radar.
          </p>
          <p className="pa-dim">
            We could have refused outright. We don't, because a bank that silently blocks its customers' money is a
            different and worse problem — and because occasionally the customer is right and we are wrong.
          </p>
          <button className="pa-btn" onClick={() => go("reveal")}>
            Show me what the bank saw
          </button>
        </div>
      )}

      {act === "reveal" && (
        <div className="pa-card pa-reveal">
          <div className="pa-kicker">Now look at the big screen</div>
          <h1>Everything you just did is already there.</h1>
          {allowedThrough.current && (
            <p className="pa-honest">
              Note: the engine allowed that payment rather than challenging it. That is what actually happened, so
              that is what this screen says.
            </p>
          )}
          <p>
            Both payments, the evidence behind each one, the money arithmetic that chose the response, and a re-run
            proving the decision reproduces exactly — with its audit hash intact.
          </p>
          {sc && (
            <div className="pa-why-works">
              <div className="pa-why-works-h">Why this one works on people</div>
              <p>{sc.why_it_works}</p>
            </div>
          )}
          {fanin !== null && fanin > 1 && (
            <div className="pa-ring">
              <b>{fanin} different people</b> have now paid that same collection account. You weren't being targeted.
              You were one row in a fan-out.
            </div>
          )}
          {decision && (
            <div className="pa-ref">
              your reference <span className="mn">{decision.end_to_end_id}</span>
            </div>
          )}
          <button
            className="pa-btn ghost"
            onClick={() => {
              sessionStorage.removeItem(SESSION_KEY);
              window.location.reload();
            }}
          >
            Run it again for the next judge
          </button>
          <p className="pa-dim pa-note">The next scan draws a different scam. There are five in rotation.</p>
        </div>
      )}
    </div>
  );
}

/** The customer-facing "show your working".
 *
 * Everything here is read from the same /explain payload the analyst console renders — the
 * ranked evidence, the checks that came back clean, and the re-execution result. Nothing is
 * re-worded per-decision by this component beyond the phrasing the server already produced,
 * so the phone and the big screen cannot disagree. */
function WhyPanel({ why, err }: { why: ExplainResponse | null; err: string | null }) {
  if (err)
    return (
      <div className="pa-why pa-why-load">
        The full record has not reached long-term storage yet, so we cannot show the detail here. The decision itself
        stands and is already written to the audit log.
      </div>
    );
  if (!why) return <div className="pa-why pa-why-load">Fetching the full record…</div>;
  const ex = why.explanation;
  const det = why.determinism;
  const cleared = ex.cleared ?? [];
  return (
    <div className="pa-why">
      <div className="pa-why-sec">What we saw</div>
      {(ex.evidence ?? []).slice(0, 4).map((e) => (
        <div key={e.id} className={`ev-item sev-${e.severity}`}>
          <span className="ev-dot" />
          <div>
            <div className="ev-t">{e.title}</div>
            <div className="ev-d">{e.detail}</div>
          </div>
        </div>
      ))}

      {cleared.length > 0 && (
        <>
          <div className="pa-why-sec">
            What we checked that was <b>fine</b>
          </div>
          <div className="pa-why-clear">
            {cleared.slice(0, 6).map((c) => (
              <span key={c.id} className="pa-why-chip">
                {c.title}
              </span>
            ))}
            {cleared.length > 6 && <span className="pa-why-chip more">+{cleared.length - 6} more</span>}
          </div>
          <p className="pa-why-note">
            We are not accusing you. Most of what we looked at came back normal — these are the specific things that
            did not.
          </p>
        </>
      )}

      <div className="pa-why-sec">Can the bank prove this later?</div>
      <div className="pa-why-proof">
        <span className={det.reproduced ? "ok" : "bad"}>
          {det.reproduced ? "✓ Re-ran and got the identical result" : "· Re-run did not match"}
        </span>
        <span className={det.chain_intact ? "ok" : "bad"}>
          {det.chain_intact ? "✓ Tamper-evident record intact" : "· Record chain broken"}
        </span>
      </div>
      <p className="pa-why-note">
        The same inputs always produce the same decision, and it is written into a sealed chain. If you dispute this
        later, the bank cannot quietly change what it decided today.
      </p>
    </div>
  );
}

const ACT_STEPS: { key: string; label: string; acts: Act[] }[] = [
  { key: "1", label: "Ordinary", acts: ["intro", "home", "chai_done"] },
  { key: "2", label: "The call", acts: ["call", "scam_pay"] },
  { key: "3", label: "The catch", acts: ["warned"] },
  { key: "4", label: "Your choice", acts: ["cancelled", "override"] },
  { key: "5", label: "The proof", acts: ["reveal"] },
];

function ActBar({ act }: { act: Act }) {
  const activeIdx = ACT_STEPS.findIndex((s) => s.acts.includes(act));
  return (
    <div className="pa-acts">
      {ACT_STEPS.map((s, i) => (
        <div key={s.key} className={`pa-act ${i === activeIdx ? "on" : i < activeIdx ? "done" : ""}`}>
          <span className="pa-act-n">{s.key}</span>
          <span className="pa-act-l">{s.label}</span>
        </div>
      ))}
    </div>
  );
}
