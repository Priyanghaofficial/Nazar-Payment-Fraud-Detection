import { Fragment } from "react";
import { LADDER, type Action } from "../api/types";

interface DecisionBlockProps {
  action: Action;
  /** policy.Ladder.AdvisoryMaxRung — reads from the decision's context, not hardcoded.
   *  Falls back to "STEP_UP_INTERSTITIAL" (the only value the policy has ever shipped)
   *  only when the caller has no policy loaded yet. */
  advisoryMaxRung?: string;
  caption?: string;
}

const LADDER_LABEL: Record<string, string> = {
  ALLOW: "Allow",
  ALLOW_MONITOR: "Allow & monitor",
  STEP_UP: "Step-up",
  STEP_UP_INTERSTITIAL: "Step-up interstitial",
  HOLD: "Hold",
};

/** The signature component: all five rungs always render; CAP/BLOCK render detached below a
 * rule since they are off-ladder and never take a risk hue. */
export function DecisionBlock({ action, advisoryMaxRung = "STEP_UP_INTERSTITIAL", caption }: DecisionBlockProps) {
  const isOffLadder = action === "CAP" || action === "BLOCK";
  const ceilIndex = LADDER.indexOf(advisoryMaxRung as Action);

  return (
    <div className="card">
      <div className="ch">
        <h2>Decision</h2>
        {caption && (
          <>
            <div className="sp" />
            <span className="sub">{caption}</span>
          </>
        )}
      </div>
      <div className="lad">
        {LADDER.map((rung, i) => (
          <Fragment key={rung}>
            <div className={`lr3 ${!isOffLadder && rung === action ? "on" : ""}`}>
              <span className="p" />
              {LADDER_LABEL[rung]}
              {!isOffLadder && rung === action && <span className="tg">landed</span>}
            </div>
            {ceilIndex >= 0 && i === ceilIndex && <div className="ceil">advisory ceiling</div>}
          </Fragment>
        ))}
        <div className="offl">
          <span className="st s-nt" style={{ opacity: action === "CAP" ? 1 : 0.4 }}>
            <i />
            Cap
          </span>
          <span className="st s-sp" style={{ opacity: action === "BLOCK" ? 1 : 0.4 }}>
            <i />
            Block
          </span>
        </div>
      </div>
    </div>
  );
}
