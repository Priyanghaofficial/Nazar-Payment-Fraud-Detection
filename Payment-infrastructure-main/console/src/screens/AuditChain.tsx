import { useState } from "react";
import { api } from "../api/client";
import type { AuditVerifyResponse } from "../api/types";
import { formatTimeMs } from "../lib/format";

export function AuditChain() {
  const [result, setResult] = useState<AuditVerifyResponse | null>(null);
  const [verifiedAt, setVerifiedAt] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function verify() {
    setLoading(true);
    setError(null);
    try {
      const res = await api.auditVerify();
      setResult(res);
      setVerifiedAt(Date.now());
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setResult(null);
    } finally {
      setLoading(false);
    }
  }

  return (
    <div>
      <div className="ph">
        <div>
          <h1>Audit Chain</h1>
          <p>
            One chain, one writer (P0 — CLAUDE.md). <span className="mn">GET /v1/audit/verify</span> recomputes the
            hash chain live and reports the first break, if any.
          </p>
        </div>
        <div className="sp" />
        <span className={`st ${!result ? "s-nt" : result.ok ? "s-ok" : "s-sp"}`}>
          <i />
          {!result ? "Not verified yet" : result.ok ? "Chain intact" : "Chain broken"}
        </span>
        <button className="pill pri" onClick={verify} disabled={loading}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
            <circle cx="12" cy="12" r="9" />
            <path d="M8 12.5l3 3 5-6" />
          </svg>
          {loading ? "Verifying…" : "Verify chain"}
        </button>
      </div>

      {error && (
        <div className="deg" style={{ marginBottom: 14 }}>
          GET /v1/audit/verify failed: {error}
        </div>
      )}

      <div className="row r4">
        <div className="card met">
          <div className="lb">Entries checked</div>
          <div className="vl mny">{result ? result.n.toLocaleString("en-IN") : "—"}</div>
          <div className="ft">
            <div className="dd">walked from genesis on each verify</div>
          </div>
        </div>
        <div className="card met">
          <div className="lb">Last verified</div>
          <div className="vl mny">{verifiedAt ? formatTimeMs(verifiedAt) : "—"}</div>
          <div className="ft">
            <div className="dd">this browser session</div>
          </div>
        </div>
        <div className="card met">
          <div className="lb">Break point</div>
          <div className="vl mny">{!result ? "—" : result.break_at === -1 ? "none" : result.break_at}</div>
          <div className="ft">
            <div className="dd">
              {!result
                ? "run a verify to check"
                : result.break_at === -1
                  ? `across ${result.n} entries`
                  : "first divergence found here"}
            </div>
          </div>
        </div>
        <div className="card met">
          <div className="lb">Writers</div>
          <div className="vl mny">1</div>
          <div className="ft">
            <div className="dd">single-writer at P0</div>
          </div>
        </div>
      </div>
    </div>
  );
}
