import { useEffect, useState } from "react";
import { api } from "../api/client";

/** Fetches the policy bundle once and extracts advisory_max_rung for the decision block's
 * dashed ceiling line (docs/08 §3: "reads policy.advisory_max_rung from the decision
 * record", i.e. the live policy — not a hardcoded constant). */
export function useAdvisoryMaxRung(): string | undefined {
  const [rung, setRung] = useState<string | undefined>(undefined);

  useEffect(() => {
    let cancelled = false;
    api
      .policy()
      .then((p) => {
        if (cancelled) return;
        const ladder = p["Ladder"] as Record<string, unknown> | undefined;
        const v = ladder?.["AdvisoryMaxRung"];
        if (typeof v === "string") setRung(v);
      })
      .catch(() => {
        // leave undefined — DecisionBlock falls back to the documented default
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return rung;
}
