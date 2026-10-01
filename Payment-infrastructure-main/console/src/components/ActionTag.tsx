import type { Action } from "../api/types";

/** action -> {status pill class, display label}. CAP/BLOCK are off-ladder (never a risk hue
 * on the ladder itself, but the status pill still needs a visually distinct treatment here —
 * s-nt/s-sp, matching console-target-state.html's own usage for Capped/Blocked rows). */
function tagFor(action: Action): { cls: string; label: string } {
  switch (action) {
    case "ALLOW":
      return { cls: "s-ok", label: "Allowed" };
    case "ALLOW_MONITOR":
      return { cls: "s-ok", label: "Allow & monitor" };
    case "STEP_UP":
      return { cls: "s-wn", label: "Step-up" };
    case "STEP_UP_INTERSTITIAL":
      return { cls: "s-wn", label: "Step-up" };
    case "HOLD":
      return { cls: "s-hd", label: "Hold" };
    case "CAP":
      return { cls: "s-nt", label: "Capped" };
    case "BLOCK":
      return { cls: "s-sp", label: "Blocked" };
    default:
      return { cls: "s-nt", label: String(action) };
  }
}

export function ActionTag({ action }: { action: Action }) {
  const { cls, label } = tagFor(action);
  return (
    <span className={`st ${cls}`}>
      <i />
      {label}
    </span>
  );
}
