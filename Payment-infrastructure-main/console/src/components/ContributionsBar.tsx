import { Fragment } from "react";

interface ContributionsBarProps {
  contributions: Record<string, number>;
}

/** Signed contribution bars — positive (risk-increasing) in --stepup, negative in --allow,
 * scaled against the largest absolute contribution in the set. */
export function ContributionsBar({ contributions }: ContributionsBarProps) {
  const entries = Object.entries(contributions).sort((a, b) => Math.abs(b[1]) - Math.abs(a[1]));
  const max = Math.max(1e-9, ...entries.map(([, v]) => Math.abs(v)));

  return (
    <div className="shap">
      {entries.map(([id, v]) => {
        const pct = (Math.abs(v) / max) * 50; // half-width max, bar grows from center
        const neg = v < 0;
        return (
          <Fragment key={id}>
            <span className="k">{id}</span>
            <div className="trk">
              <i
                className={neg ? "n" : ""}
                style={neg ? { right: "50%", left: "auto", width: `${pct}%` } : { left: "50%", width: `${pct}%` }}
              />
            </div>
            <span className="v">
              {v >= 0 ? "+" : ""}
              {v.toFixed(2)}
            </span>
          </Fragment>
        );
      })}
    </div>
  );
}
