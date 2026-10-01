// Formatting helpers — brand kit §6. Money always from int64 paise, en-IN grouping, never
// 3-3-3. No formatting logic duplicated elsewhere.

const inr = new Intl.NumberFormat("en-IN", { style: "currency", currency: "INR" });

/** Full rupee amount from minor units (paise). ₹4,25,000.00 — en-IN grouping. */
export function formatMinor(minor: number | null | undefined): string {
  if (minor === null || minor === undefined || Number.isNaN(minor)) return "—";
  return inr.format(minor / 100);
}

/** Compact strip figure: ₹4.2L, ₹1.3Cr — lakh/crore, never K/M. */
export function formatMinorCompact(minor: number | null | undefined): string {
  if (minor === null || minor === undefined || Number.isNaN(minor)) return "—";
  const rupees = minor / 100;
  const abs = Math.abs(rupees);
  if (abs >= 1_00_00_000) return `₹${(rupees / 1_00_00_000).toFixed(1)}Cr`;
  if (abs >= 1_00_000) return `₹${(rupees / 1_00_000).toFixed(1)}L`;
  return inr.format(rupees);
}

/** Mid-truncate an account id / hash: a3f1…9e2b. */
export function truncateMid(s: string | null | undefined, head = 4, tail = 4): string {
  if (!s) return "—";
  if (s.length <= head + tail + 1) return s;
  return `${s.slice(0, head)}…${s.slice(-tail)}`;
}

/** base64 chain hash -> hex-ish short display, mid-truncated. */
export function truncateHash(b64: string | null | undefined): string {
  if (!b64) return "—";
  return truncateMid(b64, 6, 6);
}

/** ms -> fixed-precision string, tabular. */
export function formatMs(ms: number | null | undefined, digits = 2): string {
  if (ms === null || ms === undefined || Number.isNaN(ms)) return "—";
  return `${ms.toFixed(digits)} ms`;
}

const timeFmt = new Intl.DateTimeFormat("en-IN", {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});

/** epoch ms -> HH:MM:SS.mmm, local time, for the live table. */
export function formatTimeMs(ms: number | null | undefined): string {
  if (!ms) return "—";
  const d = new Date(ms);
  const millis = String(d.getMilliseconds()).padStart(3, "0");
  return `${timeFmt.format(d)}.${millis}`;
}

/** "we first saw this account N days ago" — never "created". */
export function daysAgo(days: number | null | undefined): string {
  if (days === null || days === undefined || Number.isNaN(days)) return "unknown";
  if (days < 1) return "today";
  if (days === 1) return "1 day ago";
  return `${Math.floor(days)} days ago`;
}
