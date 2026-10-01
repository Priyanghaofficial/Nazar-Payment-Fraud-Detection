import type { Decision, TransactionRecord } from "../api/types";
import { ContributionsBar } from "./ContributionsBar";
import { DecisionBlock } from "./DecisionBlock";
import { LatencyBlock } from "./LatencyBlock";
import { StateChip } from "./StateChip";
import { formatMinor, formatMs, truncateHash, truncateMid } from "../lib/format";

interface DecisionDetailProps {
  decision: Decision;
  transaction?: TransactionRecord;
  advisoryMaxRung?: string;
}

/** The Investigation / Alert Detail view — also reused verbatim by Time Machine: this IS
 * the persisted record, never recomputed. No fabricated "case" panel (assignee, SLA,
 * escalate/freeze/confirm-fraud workflow) — there is no case-management backend behind
 * those actions at P0, so they are left out rather than rendered as buttons that do nothing. */
export function DecisionDetail({ decision, transaction, advisoryMaxRung }: DecisionDetailProps) {
  const fv = decision.features;
  const featureIds = fv ? Object.keys(fv.status).sort() : [];
  const findings = decision.findings ?? [];
  const degraded = decision.degraded ?? [];

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <div className="sp1">
        <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
          <div className="card">
            <div className="ch">
              <h2>{formatMinor(transaction?.instructed_amount_minor)}</h2>
              <span className="tag">{decision.rail_fired || transaction?.rail || "—"}</span>
              <div className="sp" />
              <span className="sub">{decision.kind}</span>
            </div>
            <div style={{ padding: "0 18px 12px", fontFamily: "var(--font-mono)", fontSize: 11.5, color: "var(--ink4)" }}>
              {decision.end_to_end_id}
              {decision.reason_codes && decision.reason_codes.length > 0 ? ` · ${decision.reason_codes.join(", ")}` : ""}
            </div>
            <div className="kvg" style={{ gridTemplateColumns: "repeat(4, 1fr)" }}>
              <div>
                <div className="k">p_model</div>
                <div className="v mn">{decision.p_model?.toFixed(4) ?? "—"}</div>
              </div>
              <div>
                <div className="k">p_prevalence_adj</div>
                <div className="v mn">{decision.p_prevalence_adj?.toFixed(4) ?? "—"}</div>
              </div>
              <div>
                <div className="k">Expected loss</div>
                <div className="v mny" style={{ fontSize: 15 }}>
                  {formatMinor(decision.expected_loss_minor)}
                </div>
              </div>
              <div>
                <div className="k">Expected cost</div>
                <div className="v mny" style={{ fontSize: 15 }}>
                  {formatMinor(decision.expected_cost_minor)}
                </div>
              </div>
            </div>
            <div style={{ padding: "14px 18px 4px" }}>
              <LatencyBlock queueMs={decision.queue_delay_ms} serviceMs={decision.service_ms} totalMs={decision.total_ms} />
            </div>
          </div>

          <div className="card">
            <div className="ch">
              <h2>Why this fired</h2>
              <div className="sp" />
              <span className="sub">model {decision.model_bundle_version || "—"}</span>
            </div>
            {decision.contributions && Object.keys(decision.contributions).length > 0 ? (
              <ContributionsBar contributions={decision.contributions} />
            ) : (
              <div style={{ padding: "0 18px 16px", color: "var(--ink4)", fontSize: 12.5 }}>
                contributions: null on this decision (rails-only / rule path, no model attribution)
              </div>
            )}
            <div className="chips">
              {findings.length === 0 && <span style={{ color: "var(--ink4)", fontSize: 12.5 }}>no findings recorded</span>}
              {findings.map((f) => (
                <StateChip key={f.signal_id} label={f.signal_id} status={f.status} reason={f.reason_code} />
              ))}
            </div>
          </div>

          <div className="card">
            <div className="ch">
              <h2>Feature vector</h2>
              <span className="badge i">{featureIds.length}</span>
            </div>
            <div style={{ maxHeight: 460, overflowY: "auto" }}>
              <table>
                <thead>
                  <tr>
                    <th>feature_id</th>
                    <th className="r">value</th>
                    <th>status</th>
                    <th>reason</th>
                    <th className="r">staleness</th>
                  </tr>
                </thead>
                <tbody>
                  {featureIds.map((id) => {
                    const status = fv!.status[id];
                    const value = fv!.values[id];
                    const reason = fv!.reason[id];
                    const stale = fv!.staleness[id];
                    return (
                      <tr key={id}>
                        <td className="mn">{id}</td>
                        <td className="r mn">{status === "CLEAR" || status === "FIRED" ? (value ?? "—") : "—"}</td>
                        <td>
                          <StateChip label={status} status={status} reason={reason} />
                        </td>
                        <td className="mn">{reason ?? "—"}</td>
                        <td className="r mn">{stale !== undefined ? formatMs(stale * 1000) : "—"}</td>
                      </tr>
                    );
                  })}
                  {featureIds.length === 0 && (
                    <tr>
                      <td colSpan={5} style={{ color: "var(--ink4)" }}>
                        no feature vector on this decision record
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>

          {transaction && (
            <div className="card">
              <div className="ch">
                <h2>Raw transaction — as persisted</h2>
              </div>
              <div
                className="mn"
                style={{ padding: "0 18px 16px", display: "grid", gridTemplateColumns: "1fr 1fr", gap: "4px 20px" }}
              >
                {Object.entries(transaction).map(([k, v]) => (
                  <div key={k} style={{ display: "flex", justifyContent: "space-between", gap: 12 }}>
                    <span style={{ color: "var(--ink4)" }}>{k}</span>
                    <span className="mn" style={{ textAlign: "right", wordBreak: "break-all", color: "var(--ink2)" }}>
                      {k.includes("account") ? truncateMid(String(v), 6, 6) : String(v)}
                    </span>
                  </div>
                ))}
              </div>
              {"remittance_info" in transaction && (
                <div style={{ padding: "0 18px 16px", color: "var(--ink4)", fontSize: 12 }}>
                  remittance_info is shown here as plain text for the investigator only — it is
                  attacker-controlled and is never forwarded to an LLM (CLAUDE.md non-negotiable #14).
                </div>
              )}
            </div>
          )}
        </div>

        <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
          <DecisionBlock action={decision.action} advisoryMaxRung={advisoryMaxRung} caption={`seq ${decision.decision_seq}`} />

          <div className="card">
            <div className="ch">
              <h2>Reproducibility</h2>
            </div>
            <div className="meta">
              <div className="kv">
                <span className="k">Model</span>
                <span className="v mn">{decision.model_bundle_version || "—"}</span>
              </div>
              <div className="kv">
                <span className="k">Policy</span>
                <span className="v mn">{decision.policy_version || "—"}</span>
              </div>
              <div className="kv">
                <span className="k">Rules</span>
                <span className="v mn">{decision.rules_version || "—"}</span>
              </div>
              <div className="kv">
                <span className="k">Registry</span>
                <span className="v mn">{decision.signal_registry_version || "—"}</span>
              </div>
              <div className="kv">
                <span className="k">Degraded</span>
                <span className="v mn" style={{ color: degraded.length > 0 ? "var(--warn)" : "var(--ink)" }}>
                  {degraded.length > 0 ? degraded.join(", ") : "none"}
                </span>
              </div>
              <div className="kv">
                <span className="k">is_control</span>
                <span className="v mn">{String(decision.is_control)}</span>
              </div>
              <div className="kv">
                <span className="k">Propensity</span>
                <span className="v mn">{decision.action_propensity?.toFixed(3) ?? "—"}</span>
              </div>
            </div>
          </div>

          <div className="card">
            <div className="ch">
              <h2>Chain</h2>
            </div>
            <div className="meta">
              <div className="kv">
                <span className="k">Seq</span>
                <span className="v mn">{decision.chain_seq}</span>
              </div>
              <div className="kv">
                <span className="k">Prev hash</span>
                <span className="v mn">{truncateHash(decision.prev_hash)}</span>
              </div>
              <div className="kv">
                <span className="k">Hash</span>
                <span className="v mn" style={{ color: "var(--indigo)" }}>
                  {truncateHash(decision.hash)}
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
