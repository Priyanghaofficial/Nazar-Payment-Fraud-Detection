import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api } from "../api/client";
import type { DecisionDetailResponse } from "../api/types";
import { DecisionDetail } from "../components/DecisionDetail";
import { RecentPicker } from "../components/RecentPicker";
import { useAdvisoryMaxRung } from "../lib/usePolicy";

interface InvestigationProps {
  /** When true, frames the copy as Time Machine ("look up any past transaction, see exactly
   * what was persisted, never recomputed") instead of live investigation. Same component,
   * same endpoint — per the spec these are explicitly the same data source. */
  timeMachine?: boolean;
}

export function Investigation({ timeMachine = false }: InvestigationProps) {
  const [params, setParams] = useSearchParams();
  const idParam = params.get("id") ?? "";
  const [input, setInput] = useState(idParam);
  const [data, setData] = useState<DecisionDetailResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const advisoryMaxRung = useAdvisoryMaxRung();

  useEffect(() => {
    setInput(idParam);
    if (!idParam) {
      setData(null);
      setError(null);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    api
      .getDecision(idParam)
      .then((res) => {
        if (!cancelled) setData(res);
      })
      .catch((e) => {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : String(e));
          setData(null);
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [idParam]);

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setParams(input ? { id: input } : {});
  }

  return (
    <div>
      <div className="ph">
        <div>
          <h1>{timeMachine ? "Time Machine" : "Investigation"}</h1>
          <p>
            {timeMachine
              ? "Look up any past end-to-end ID and see exactly what was persisted — never recomputed. Same GET /v1/decisions/{id} as live investigation."
              : "Paste an end-to-end ID, or click a row on Live Monitor, to open GET /v1/decisions/{id}."}
          </p>
        </div>
      </div>

      <form onSubmit={onSubmit} className="card" style={{ marginBottom: 14, display: "flex", gap: 10, padding: 14 }}>
        <input
          className="field"
          style={{ flex: 1 }}
          placeholder="end_to_end_id"
          value={input}
          onChange={(e) => setInput(e.target.value)}
        />
        <button type="submit" className="pill pri">
          Look up
        </button>
      </form>

      {!idParam && (
        <RecentPicker
          onPick={(id) => setParams({ id })}
          title={timeMachine ? "Pick any decision from history" : "Or pick one from recent activity"}
        />
      )}
      {loading && (
        <div className="card" style={{ padding: 18, color: "var(--ink3)", fontSize: 13 }}>
          Loading…
        </div>
      )}
      {error && (
        <div className="deg">
          GET /v1/decisions/{idParam} failed: {error}
        </div>
      )}
      {data && !loading && !error && (
        <DecisionDetail decision={data.decision} transaction={data.transaction} advisoryMaxRung={advisoryMaxRung} />
      )}
    </div>
  );
}
