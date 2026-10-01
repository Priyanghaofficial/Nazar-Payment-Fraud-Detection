import { useEffect, useMemo, useState } from "react";
import { api } from "../api/client";
import type { GraphTopAccount, StreamDecisionEvent } from "../api/types";
import { formatMinor, formatTimeMs, truncateMid } from "../lib/format";

/** A browsable list of what the system has actually decided.
 *
 * The lookup screens each began with an empty box asking for an end-to-end id or an account
 * number — values nobody can know without first reading them off another screen. Opened
 * cold that reads as an unfinished stub, which is the opposite of what these pages are:
 * they are the proof that any decision can be pulled back up, whole, long after the fact.
 * So they now open on real, clickable history and keep the box for when someone genuinely
 * has an id in hand.
 *
 * Everything here is read from GET /v1/decisions/recent — persisted Postgres rows, not a
 * client-side cache — so an empty list means the system genuinely has no history, and says
 * so rather than showing placeholders. */

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

type Filter = "challenged" | "all";

export function RecentPicker({
  onPick,
  selected,
  limit = 60,
  title = "Or pick one that already happened",
}: {
  onPick: (id: string) => void;
  selected?: string | null;
  limit?: number;
  title?: string;
}) {
  const [rows, setRows] = useState<StreamDecisionEvent[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<Filter>("challenged");

  useEffect(() => {
    let cancelled = false;
    api
      .recentDecisions(200)
      .then((r) => !cancelled && setRows(r.rows ?? []))
      .catch((e) => !cancelled && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      cancelled = true;
    };
  }, []);

  const shown = useMemo(() => {
    if (!rows) return [];
    const challenged = rows.filter((r) => r.action !== "ALLOW" && r.action !== "ALLOW_MONITOR");
    // Default to the interesting ones. An analyst opening this screen is looking for a
    // decision worth reading, and an unfiltered list is mostly allowed payments.
    const base = filter === "challenged" && challenged.length > 0 ? challenged : rows;
    return base.slice(0, limit);
  }, [rows, filter, limit]);

  const challengedCount = rows
    ? rows.filter((r) => r.action !== "ALLOW" && r.action !== "ALLOW_MONITOR").length
    : 0;

  if (error) return <div className="deg">Could not load history: {error}</div>;

  return (
    <div className="card rp">
      <div className="ch">
        <h2>{title}</h2>
        {rows && <span className="badge">{shown.length}</span>}
        <div className="sp" />
        <div className="rp-tabs">
          <button
            className={`rp-tab ${filter === "challenged" ? "on" : ""}`}
            onClick={() => setFilter("challenged")}
          >
            Challenged{challengedCount > 0 ? ` (${challengedCount})` : ""}
          </button>
          <button className={`rp-tab ${filter === "all" ? "on" : ""}`} onClick={() => setFilter("all")}>
            Everything
          </button>
        </div>
      </div>

      {!rows && <div className="rp-empty">Loading history…</div>}
      {rows && shown.length === 0 && (
        <div className="rp-empty">
          No decisions yet. Start background traffic or launch an attack on Command Centre, or have
          someone scan the QR on the Payer App screen — then they appear here.
        </div>
      )}

      <div className="rp-rows">
        {shown.map((r) => {
          const tone = ACTION_TONE[r.action] ?? "ok";
          return (
            <button
              key={r.end_to_end_id}
              className={`rp-row t-${tone} ${selected === r.end_to_end_id ? "sel" : ""}`}
              onClick={() => onPick(r.end_to_end_id)}
            >
              <span className="rp-time ts">{formatTimeMs(r.decided_at_ms)}</span>
              <span className="rp-parties">
                <span className="mn">{truncateMid(r.debtor_account, 12, 4)}</span>
                <span className="rp-arrow">→</span>
                <span className="mn">{truncateMid(r.creditor_account, 12, 4)}</span>
              </span>
              <span className="rp-why">{r.top_reason}</span>
              <span className="rp-amt mny">{formatMinor(r.amount_minor)}</span>
              <span className={`rp-act t-${tone}`}>{ACTION_SHORT[r.action] ?? r.action}</span>
            </button>
          );
        })}
      </div>
    </div>
  );
}

/** Accounts worth looking at in the graph, derived from decisions that actually happened.
 *
 * Ranked by how many distinct payers have paid each beneficiary, because that is exactly the
 * shape the ring detector cares about — the account at the top of this list is the one most
 * likely to show a fan-in when you open it. */
export function AccountPicker({
  onPick,
  selected,
}: {
  onPick: (account: string) => void;
  selected?: string | null;
}) {
  const [accounts, setAccounts] = useState<GraphTopAccount[] | null>(null);
  const [note, setNote] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    api
      .graphTop()
      .then((r) => {
        if (cancelled) return;
        setAccounts(r.accounts ?? []);
        setNote(r.note ?? "");
      })
      .catch((e) => !cancelled && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      cancelled = true;
    };
  }, []);

  if (error) return <div className="deg">Could not load accounts: {error}</div>;

  return (
    <div className="card rp">
      <div className="ch">
        <h2>Pick a beneficiary the graph is holding</h2>
        <div className="sp" />
        <span className="sub">ranked by distinct payers</span>
      </div>
      {!accounts && <div className="rp-empty">Loading accounts…</div>}
      {accounts && accounts.length === 0 && (
        <div className="rp-empty">
          The graph is empty. It is held in process, so it only knows what this instance has seen since it
          started — launch the mule fan-out attack on Command Centre, which is built to produce a ring, and
          these fill in.
        </div>
      )}
      <div className="rp-accts">
        {(accounts ?? []).map((a) => (
          <button
            key={a.account}
            className={`rp-acct ${selected === a.account ? "sel" : ""} ${
              a.distinct_payers >= 3 ? "ring" : ""
            }`}
            onClick={() => onPick(a.account)}
          >
            <span className="rp-acct-id mn">{truncateMid(a.account, 16, 6)}</span>
            <span className="rp-acct-n">
              {a.distinct_payers} payer{a.distinct_payers === 1 ? "" : "s"}
              {a.shared_devices > 0 ? ` · ${a.shared_devices} device${a.shared_devices === 1 ? "" : "s"}` : ""}
            </span>
          </button>
        ))}
      </div>
      {note && <p className="rp-note">{note}</p>}
    </div>
  );
}
