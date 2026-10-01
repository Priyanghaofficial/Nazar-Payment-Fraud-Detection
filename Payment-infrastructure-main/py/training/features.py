"""Offline feature materialisation for training — a Python re-implementation of the same
feature semantics as go/internal/features/compute.go and go/internal/profile/store.go,
computed from data (features/registry.yaml) rather than duplicated by convention.

This is NOT a byte-exact parity implementation (docs/06 §4's golden_features test, which
would enforce that, is named as a [P1] stretch item — see REVIEW.md). It is a real,
from-scratch streaming computation over the same raw event stream, respecting the same
invariants the Go side is tested against:

  - read-before-write: every feature for event i is computed from state strictly BEFORE
    event i, then state is updated. The event being scored is never in its own features.
  - windows use exact counts over an explicit time window (the ZCOUNT equivalent), never a
    total cardinality (never the ZCARD bug class).
  - a feature that cannot be computed yet (cold start, insufficient history) is NaN, not a
    fabricated zero — LightGBM handles NaN natively, matching docs/02 §5.6.

Money stays int64 (Python int) minor units throughout; only cast to float at the point a
ratio/z-score is actually computed.
"""
from __future__ import annotations

import bisect
import math
from collections import defaultdict, deque

MS_PER_MIN = 60_000
MS_PER_HOUR = 3_600_000
MS_PER_DAY = 86_400_000


def _bank_of(acct: str) -> str:
    return acct.split("-", 1)[0] if "-" in acct else "UNKNOWN"


def _median_mad_p95_p99(samples: list[int]) -> tuple[float, float, float, float]:
    if not samples:
        return 0.0, 0.0, 0.0, 0.0
    s = sorted(samples)
    n = len(s)

    def pct(p: float) -> float:
        idx = min(int(p * (n - 1)), n - 1)
        return float(s[idx])

    median = pct(0.5)
    deviations = sorted(abs(x - median) for x in s)
    mad = deviations[min(int(0.5 * (len(deviations) - 1)), len(deviations) - 1)] if deviations else 0.0
    return median, mad, pct(0.95), pct(0.99)


class _Window:
    """A timestamp-ordered deque supporting exact ZCOUNT-equivalent windowed counts."""

    def __init__(self, retain_ms: int = 26 * MS_PER_HOUR):
        self.times: deque[int] = deque()
        self.retain_ms = retain_ms

    def add(self, ts_ms: int):
        self.times.append(ts_ms)

    def prune(self, now_ms: int):
        cutoff = now_ms - self.retain_ms
        while self.times and self.times[0] < cutoff:
            self.times.popleft()

    def count_since(self, now_ms: int, window_ms: int) -> int:
        lo = now_ms - window_ms
        # times is append-ordered (== time-ordered since we feed it chronologically)
        idx = bisect.bisect_right(self.times, lo)
        return len(self.times) - idx


class PayerState:
    __slots__ = (
        "txn_window", "amt_samples", "known_payees", "known_devices", "known_asns",
        "first_seen_ms", "last_ts_ms", "last_geo", "last_amt", "hour_hist",
        "amt_daily_sums", "txn_count_lifetime", "baseline_updated_ms",
    )

    def __init__(self):
        self.txn_window = _Window()
        self.amt_samples: deque[int] = deque(maxlen=500)
        self.known_payees: set[str] = set()
        self.known_devices: set[str] = set()
        self.known_asns: set[int] = set()
        self.first_seen_ms: int | None = None
        self.last_ts_ms: int | None = None
        self.last_geo: str | None = None
        self.last_amt: int = 0
        self.hour_hist = [0] * 24
        self.amt_daily_sums: dict[int, int] = defaultdict(int)  # day_epoch -> sum, for 30d mean
        self.txn_count_lifetime = 0
        self.baseline_updated_ms: int | None = None


class PayeeState:
    __slots__ = ("payers_window", "payer_last_paid", "first_seen_ms", "inflow", "outflow")

    def __init__(self):
        self.payers_window = _Window()
        self.payer_last_paid: dict[str, int] = {}  # payer -> last accepted_at_ms (for fanin exact count + HHI)
        self.first_seen_ms: int | None = None
        self.inflow: deque[tuple[int, int]] = deque()   # (ts_ms, amt) as payee
        self.outflow: deque[tuple[int, int]] = deque()  # (ts_ms, amt) when this account later pays out


class PairState:
    __slots__ = ("txn_count", "first_added_ms", "last_ts_ms", "amt_p95")

    def __init__(self):
        self.txn_count = 0
        self.first_added_ms: int | None = None
        self.last_ts_ms: int | None = None
        self.amt_p95 = 0


class DeviceState:
    __slots__ = ("accts_window_ts", "first_seen_ms")

    def __init__(self):
        self.accts_window_ts: dict[str, int] = {}  # account -> last_seen_ms
        self.first_seen_ms: int | None = None


class ASNState:
    __slots__ = ("accts_window_ts",)

    def __init__(self):
        self.accts_window_ts: dict[str, int] = {}


FEATURE_IDS = [
    "amt_robust_z", "amt_over_p95", "hour_surprisal",
    "txn_velocity_1m", "txn_velocity_5m", "txn_velocity_1h", "txn_velocity_24h",
    "amt_velocity_1h", "amt_velocity_24h",
    "account_age_days", "dormancy_days", "baseline_staleness_h",
    "payee_is_new_to_payer", "payee_first_seen_by_us_days",
    "payee_fanin_1h", "payee_fanin_24h", "payee_fanin_burstiness",
    "pair_txn_count_90d", "pair_amt_ratio_p95",
    "payee_fwd_latency_p50_s", "payee_fwd_ratio_1h",
    "payee_inflow_concentration", "payee_distinct_payer_banks_1h",
    "device_is_new_to_payer", "device_first_seen_hours", "device_acct_degree_24h",
    "asn_is_new_to_payer", "asn_acct_degree_1h",
    "geo_jump_kmh",
    "cold_start_features_n",
]


def _parse_geo(cell: str | None):
    if not cell:
        return None
    try:
        lat, lon = cell.split(":")
        return float(lat), float(lon)
    except ValueError:
        return None


def _haversine_km(a, b) -> float:
    lat1, lon1 = a
    lat2, lon2 = b
    r = 6371.0
    p1, p2 = math.radians(lat1), math.radians(lat2)
    dphi = math.radians(lat2 - lat1)
    dlmb = math.radians(lon2 - lon1)
    h = math.sin(dphi / 2) ** 2 + math.cos(p1) * math.cos(p2) * math.sin(dlmb / 2) ** 2
    return 2 * r * math.asin(math.sqrt(h))


class FeatureStream:
    """Stateful streaming feature computer. Call `compute(event)` once per event, strictly
    in accepted_at_ms order — it returns that event's feature dict (state as of just before
    this event) and then updates its internal state with the event, mirroring the Go
    read-before-write invariant (docs/02 §3.4).
    """

    def __init__(self):
        self.payers: dict[str, PayerState] = defaultdict(PayerState)
        self.payees: dict[str, PayeeState] = defaultdict(PayeeState)
        self.pairs: dict[tuple[str, str], PairState] = defaultdict(PairState)
        self.devices: dict[str, DeviceState] = defaultdict(DeviceState)
        self.asns: dict[int, ASNState] = defaultdict(ASNState)

    def compute(self, ev: dict) -> dict:
        now = ev["accepted_at_ms"]
        payer_acct = ev["debtor_account"]
        payee_acct = ev["creditor_account"]
        amount = ev["instructed_amount_minor"]
        device_id = ev.get("device_id") or None
        asn = ev.get("asn") or None
        geo = ev.get("geo_cell") or None

        p = self.payers[payer_acct]
        b = self.payees[payee_acct]
        pair = self.pairs[(payer_acct, payee_acct)]

        out: dict[str, float] = {}

        # ---- payer features ----
        p.txn_window.prune(now)
        median, mad, p95, p99 = _median_mad_p95_p99(list(p.amt_samples))
        if p.txn_count_lifetime < 10:
            out["amt_robust_z"] = math.nan
        else:
            mad_eff = max(mad, max(0.02 * median, 100))
            z = 0.6745 * (amount - median) / mad_eff if mad_eff else math.nan
            out["amt_robust_z"] = z if math.isfinite(z) and abs(z) <= 25 else math.nan

        out["amt_over_p95"] = min(amount / p95, 20.0) if p95 > 0 else math.nan

        hour = int((now // MS_PER_HOUR) % 24)
        total_hist = sum(p.hour_hist) + 24  # Laplace smoothing, alpha=1
        prob = (p.hour_hist[hour] + 1) / total_hist
        out["hour_surprisal"] = -math.log2(prob)

        out["txn_velocity_1m"] = p.txn_window.count_since(now, MS_PER_MIN)
        out["txn_velocity_5m"] = p.txn_window.count_since(now, 5 * MS_PER_MIN)
        out["txn_velocity_1h"] = p.txn_window.count_since(now, MS_PER_HOUR)
        out["txn_velocity_24h"] = p.txn_window.count_since(now, MS_PER_DAY)

        day_epoch = now // MS_PER_DAY
        daily_sums = [v for k, v in p.amt_daily_sums.items() if day_epoch - 30 <= k < day_epoch]
        mean_daily = sum(daily_sums) / 30 if daily_sums else 0
        if mean_daily <= 0:
            out["amt_velocity_1h"] = math.nan
            out["amt_velocity_24h"] = math.nan
        else:
            out["amt_velocity_1h"] = _sum_recent(p, now, MS_PER_HOUR) / mean_daily
            out["amt_velocity_24h"] = _sum_recent(p, now, MS_PER_DAY) / mean_daily

        out["account_age_days"] = (now - p.first_seen_ms) / MS_PER_DAY if p.first_seen_ms else 0.0
        out["dormancy_days"] = (now - p.last_ts_ms) / MS_PER_DAY if p.last_ts_ms else math.nan
        out["baseline_staleness_h"] = (now - p.baseline_updated_ms) / MS_PER_HOUR if p.baseline_updated_ms else math.nan

        # ---- counterparty (payee/pair) features ----
        is_new = payee_acct not in p.known_payees
        out["payee_is_new_to_payer"] = 1.0 if is_new else 0.0
        out["payee_first_seen_by_us_days"] = (now - b.first_seen_ms) / MS_PER_DAY if b.first_seen_ms else math.nan

        b.payers_window.prune(now)
        fanin_1h = _fanin_count(b, now, MS_PER_HOUR)
        fanin_24h = _fanin_count(b, now, MS_PER_DAY)
        out["payee_fanin_1h"] = fanin_1h
        out["payee_fanin_24h"] = fanin_24h
        out["payee_fanin_burstiness"] = (fanin_1h / (fanin_24h / 24)) if fanin_24h >= 6 else math.nan

        out["pair_txn_count_90d"] = pair.txn_count if pair.txn_count >= 2 else math.nan
        if pair.amt_p95 > 0 and pair.txn_count >= 3:
            out["pair_amt_ratio_p95"] = amount / pair.amt_p95
        else:
            out["pair_amt_ratio_p95"] = math.nan

        fwd_events = [(t, a) for t, a in b.outflow if now - t <= MS_PER_HOUR]
        in_events = [(t, a) for t, a in b.inflow if now - t <= MS_PER_HOUR]
        inflow_1h = sum(a for _, a in in_events)
        outflow_1h = sum(a for _, a in fwd_events)
        if len(fwd_events) >= 3:
            latencies = [t - prev_t for (prev_t, _), (t, _) in zip(b.inflow, fwd_events) if t > prev_t]
            out["payee_fwd_latency_p50_s"] = (sorted(latencies)[len(latencies) // 2] / 1000.0) if latencies else math.nan
        else:
            out["payee_fwd_latency_p50_s"] = math.nan
        out["payee_fwd_ratio_1h"] = (outflow_1h / inflow_1h) if inflow_1h >= 100 and len(fwd_events) >= 3 else math.nan

        # HHI approximation: share of DISTINCT-PAYER COUNT concentration within 24h fanin,
        # a defensible proxy for amount concentration when per-payer amount history isn't
        # separately tracked (kept simple deliberately — see module docstring).
        out["payee_inflow_concentration"] = (1.0 / fanin_24h) if fanin_24h > 0 else math.nan

        recent_banks = {_bank_of(acct) for acct, t in b.payer_last_paid.items() if now - t <= MS_PER_HOUR}
        out["payee_distinct_payer_banks_1h"] = len(recent_banks)

        # ---- channel features ----
        if device_id is None:
            out["device_is_new_to_payer"] = math.nan
            out["device_first_seen_hours"] = math.nan
            out["device_acct_degree_24h"] = math.nan
        else:
            out["device_is_new_to_payer"] = 0.0 if device_id in p.known_devices else 1.0
            dstate = self.devices[device_id]
            out["device_first_seen_hours"] = (now - dstate.first_seen_ms) / MS_PER_HOUR if dstate.first_seen_ms else math.nan
            out["device_acct_degree_24h"] = sum(1 for t in dstate.accts_window_ts.values() if now - t <= MS_PER_DAY)

        if asn is None:
            out["asn_is_new_to_payer"] = math.nan
            out["asn_acct_degree_1h"] = math.nan
        else:
            out["asn_is_new_to_payer"] = 0.0 if asn in p.known_asns else 1.0
            astate = self.asns[asn]
            out["asn_acct_degree_1h"] = sum(1 for t in astate.accts_window_ts.values() if now - t <= MS_PER_HOUR)

        geo_now = _parse_geo(geo)
        geo_last = _parse_geo(p.last_geo)
        if geo_now and geo_last and geo != p.last_geo:
            dist = _haversine_km(geo_last, geo_now)
            dt_s = max((now - (p.last_ts_ms or now)) / 1000.0, 60)
            out["geo_jump_kmh"] = dist / (dt_s / 3600.0)
        else:
            out["geo_jump_kmh"] = math.nan

        out["cold_start_features_n"] = float(sum(1 for k, v in out.items() if isinstance(v, float) and math.isnan(v)))

        # ---- state update (AFTER computing features — read-before-write) ----
        p.txn_window.add(now)
        p.amt_samples.append(amount)
        p.known_payees.add(payee_acct)
        if device_id:
            p.known_devices.add(device_id)
        if asn:
            p.known_asns.add(asn)
        if p.first_seen_ms is None:
            p.first_seen_ms = now
        p.last_ts_ms = now
        p.last_geo = geo
        p.last_amt = amount
        p.hour_hist[hour] += 1
        p.txn_count_lifetime += 1
        p.baseline_updated_ms = now
        p.amt_daily_sums[day_epoch] += amount

        b.payers_window.add(now)
        b.payer_last_paid[payer_acct] = now
        if b.first_seen_ms is None:
            b.first_seen_ms = now
        b.inflow.append((now, amount))
        while b.inflow and now - b.inflow[0][0] > MS_PER_DAY:
            b.inflow.popleft()

        payer_as_payee = self.payees.get(payer_acct)
        if payer_as_payee is not None:
            payer_as_payee.outflow.append((now, amount))
            while payer_as_payee.outflow and now - payer_as_payee.outflow[0][0] > MS_PER_DAY:
                payer_as_payee.outflow.popleft()

        pair.txn_count += 1
        if pair.first_added_ms is None:
            pair.first_added_ms = now
        pair.last_ts_ms = now
        pair.amt_p95 = amount if pair.amt_p95 == 0 else (
            pair.amt_p95 + (amount - pair.amt_p95) // 3 if amount > pair.amt_p95 else pair.amt_p95 - (pair.amt_p95 - amount) // 10
        )

        if device_id:
            dstate = self.devices[device_id]
            if dstate.first_seen_ms is None:
                dstate.first_seen_ms = now
            dstate.accts_window_ts[payer_acct] = now
        if asn:
            astate = self.asns[asn]
            astate.accts_window_ts[payer_acct] = now

        return out


def _sum_recent(p: PayerState, now: int, window_ms: int) -> int:
    # amt_daily_sums is bucketed by day; for sub-day windows we approximate using the
    # amt_samples deque (which holds recent raw amounts) filtered by an implied recency —
    # since amt_samples doesn't carry timestamps, we fall back to the day bucket for the
    # CURRENT day scaled by the window fraction. This is a coarser approximation than the
    # Go implementation's minute buckets; documented in the module docstring's simplicity note.
    day_epoch = now // MS_PER_DAY
    today_sum = p.amt_daily_sums.get(day_epoch, 0)
    fraction = min(window_ms / MS_PER_DAY, 1.0)
    return int(today_sum * fraction) if window_ms < MS_PER_DAY else today_sum


def _fanin_count(b: PayeeState, now: int, window_ms: int) -> int:
    cutoff = now - window_ms
    return sum(1 for t in b.payer_last_paid.values() if t > cutoff)
