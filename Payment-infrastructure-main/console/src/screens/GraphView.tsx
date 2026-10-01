import { useState } from "react";
import { api } from "../api/client";
import type { GraphResponse } from "../api/types";
import { truncateMid } from "../lib/format";
import { AccountPicker } from "../components/RecentPicker";

// Real bounds pulled from go/internal/graph/engine.go — used only to scale the .stage bar
// widths (the numbers displayed are exact, unscaled API values). RingSizeCap=25 is the line
// test_merchant_is_not_a_ring depends on: a payee with more distinct payers than this is
// merchant-shaped, not a ring. cashoutDepth is bounded BFS at depth 5. Bank count and shared
// device degree are bounded by ring size inside that same cap.
const RING_SIZE_CAP = 25;
const HOPS_CAP = 5;

function pct(value: number, cap: number): number {
  if (!Number.isFinite(value) || cap <= 0) return 0;
  return Math.max(0, Math.min(100, (value / cap) * 100));
}

export function GraphView() {
  const [input, setInput] = useState("");
  const [data, setData] = useState<GraphResponse | null>(null);
  const [account, setAccount] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function lookup(e: React.FormEvent) {
    e.preventDefault();
    if (!input) return;
    setLoading(true);
    setError(null);
    try {
      const res = await api.graph(input);
      setData(res);
      setAccount(input);
    } catch (e2) {
      setError(e2 instanceof Error ? e2.message : String(e2));
      setData(null);
    } finally {
      setLoading(false);
    }
  }

  return (
    <div>
      <div className="ph">
        <div>
          <h1>Graph / Ring</h1>
          <p>
            Account lookup against <span className="mn">GET /v1/graph/&#123;account&#125;</span> — in-process Go
            adjacency with decay and component cap (P0). Rings never block on their own.
          </p>
        </div>
      </div>

      <form onSubmit={lookup} className="card" style={{ padding: 14, marginBottom: 14, display: "flex", gap: 10 }}>
        <input
          className="field"
          style={{ flex: 1 }}
          placeholder="account id"
          value={input}
          onChange={(e) => setInput(e.target.value)}
        />
        <button type="submit" className="pill pri" disabled={loading}>
          {loading ? "Looking up…" : "Look up"}
        </button>
      </form>

      {error && (
        <div className="deg" style={{ marginBottom: 14 }}>
          GET /v1/graph/{input} failed: {error}
        </div>
      )}

      {data && account && (
        <>
          <div className="row r4" style={{ marginBottom: 14 }}>
            <div className="card met">
              <div className="lb">Ring score</div>
              <div className="vl mny">{data.RingScore.toFixed(4)}</div>
              <div className="ft">
                <div className="dd">structural feature, 0–1</div>
              </div>
            </div>
            <div className="card met">
              <div className="lb">Ring size</div>
              <div className="vl mny">{data.RingSize}</div>
              <div className="ft">
                <div className="dd">distinct payers · merchant-shaped above {RING_SIZE_CAP}</div>
              </div>
            </div>
            <div className="card met">
              <div className="lb">Component banks</div>
              <div className="vl mny">{data.ComponentBankCount}</div>
              <div className="ft">
                <div className="dd">distinct banks across payers</div>
              </div>
            </div>
            <div className="card met">
              <div className="lb">Hops to cash-out</div>
              <div className="vl mny">{data.HopsToCashout}</div>
              <div className="ft">
                <div className="dd">bounded BFS, depth {HOPS_CAP}</div>
              </div>
            </div>
          </div>

          <div className="card" style={{ marginBottom: 14 }}>
            <div className="ch">
              <h2>Structural signals</h2>
              <div className="sp" />
              <span className="sub mn">{truncateMid(account, 6, 4)}</span>
            </div>
            <div className="stage">
              <span className="k">Ring score</span>
              <div className="b">
                <i style={{ width: `${pct(data.RingScore * 100, 100)}%` }} />
              </div>
              <span className="v">{data.RingScore.toFixed(2)}</span>

              <span className="k">Ring size</span>
              <div className="b">
                <i style={{ width: `${pct(data.RingSize, RING_SIZE_CAP)}%` }} />
              </div>
              <span className="v">{data.RingSize}</span>

              <span className="k">Component banks</span>
              <div className="b">
                <i style={{ width: `${pct(data.ComponentBankCount, RING_SIZE_CAP)}%` }} />
              </div>
              <span className="v">{data.ComponentBankCount}</span>

              <span className="k">Hops to cash-out</span>
              <div className="b">
                <i style={{ width: `${pct(data.HopsToCashout, HOPS_CAP)}%` }} />
              </div>
              <span className="v">{data.HopsToCashout}</span>

              <span className="k">Device-shared degree</span>
              <div className="b">
                <i style={{ width: `${pct(data.DeviceSharedDegree, RING_SIZE_CAP)}%` }} />
              </div>
              <span className="v">{data.DeviceSharedDegree}</span>
            </div>
          </div>

          {data.RingScore === 0 && (
            <div className="cmt">
              ring_score 0 — the graph layer does not flag this account (this is the invariant
              <code className="mn"> test_merchant_is_not_a_ring</code> proves: a payee with more distinct payers
              than the {RING_SIZE_CAP}-payer cap is merchant-shaped, not a ring). Rings and novelty never block on
              their own (CLAUDE.md non-negotiable #7).
            </div>
          )}
        </>
      )}

      {!data && !error && !loading && (
        <AccountPicker
          onPick={(acct) => {
            setInput(acct);
            setLoading(true);
            setError(null);
            api
              .graph(acct)
              .then((res) => {
                setData(res);
                setAccount(acct);
              })
              .catch((e) => setError(e instanceof Error ? e.message : String(e)))
              .finally(() => setLoading(false));
          }}
          selected={account}
        />
      )}
    </div>
  );
}
