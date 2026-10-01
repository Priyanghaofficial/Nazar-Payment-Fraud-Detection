"""Shared sampling / event-assembly helpers used by warmup.py and the typology injectors.

Everything that touches the wire contract shape lives here so there is exactly one place that
has to match go/internal/contract/event.go.
"""
from __future__ import annotations

import itertools
from typing import Dict, Optional

import numpy as np

from population import Account, Population

RAILS_WEIGHTED = ["UPI", "IMPS", "NEFT", "CARD_CNP", "CARD_CP"]
RAILS_P = [0.70, 0.10, 0.10, 0.07, 0.03]

UPI_INITIATIONS = ["P2P", "INTENT", "COLLECT", "QR"]
UPI_INITIATIONS_P = [0.50, 0.25, 0.15, 0.10]

REMIT_BLANK_P = 0.55
REMIT_RENT_FAMILY = ["rent", "house rent", "to mom", "family support", "hostel fee", "school fee"]
REMIT_MERCHANT = ["grocery", "dinner", "payment", "order", "bill payment", "subscription"]
REMIT_URGENCY = [
    "urgent help needed", "gift for you", "emergency, please help",
    "loan repayment urgent", "help asap", "prize claim fee",
]


class IdCounter:
    """Monotonic, deterministic end_to_end_id generator shared across all producers."""

    def __init__(self, prefix: str = "TXN"):
        self._prefix = prefix
        self._counter = itertools.count(1)

    def next(self) -> str:
        return f"{self._prefix}-{next(self._counter):012d}"


def rupees_to_minor(rupees: float) -> int:
    """Rupees (float) -> paise (int64 minor units). Never emit a float amount."""
    minor = int(round(rupees * 100))
    return max(minor, 1)


def sample_amount_minor(rng: np.random.Generator, center_rupees: float, sigma: float,
                         min_rupees: float = 10, max_rupees: float = 500_000) -> int:
    val = rng.lognormal(mean=np.log(max(center_rupees, 1.0)), sigma=sigma)
    val = float(np.clip(val, min_rupees, max_rupees))
    return rupees_to_minor(val)


def sample_hour(rng: np.random.Generator, peak_hours) -> int:
    if peak_hours and rng.random() < 0.75:
        h = int(rng.choice(peak_hours))
        h = (h + int(rng.integers(-1, 2))) % 24
        return h
    return int(rng.integers(0, 24))


def sample_ts_on_day(rng: np.random.Generator, day_start_ms: int, hour: int) -> int:
    ms_in_hour = int(rng.integers(0, 3_600_000))
    return day_start_ms + hour * 3_600_000 + ms_in_hour


def jitter_geo_cell(rng: np.random.Generator, lat: float, lon: float, spread: float = 0.01) -> str:
    jlat = lat + rng.normal(0, spread)
    jlon = lon + rng.normal(0, spread)
    return f"{jlat:.4f}:{jlon:.4f}"


def far_geo_cell(rng: np.random.Generator, exclude_city: str) -> str:
    """A geo cell from a different city entirely -- used by the ATO injector to model an
    implausible travel jump."""
    from population import CITIES, CITY_NAMES
    choices = [c for c in CITY_NAMES if c != exclude_city]
    city = rng.choice(choices)
    lat, lon = CITIES[city]
    return jitter_geo_cell(rng, lat, lon, spread=0.02)


def fake_ip(rng: np.random.Generator) -> str:
    return f"{int(rng.integers(10, 223))}.{int(rng.integers(0, 255))}.{int(rng.integers(0, 255))}.{int(rng.integers(1, 254))}"


def pick_device(rng: np.random.Generator, account: Account) -> str:
    if len(account.devices) == 1 or rng.random() < 0.92:
        return account.devices[0]
    return str(rng.choice(account.devices))


def pick_rail(rng: np.random.Generator) -> str:
    return str(rng.choice(RAILS_WEIGHTED, p=RAILS_P))


def pick_initiation(rng: np.random.Generator, rail: str) -> str:
    if rail == "UPI":
        return str(rng.choice(UPI_INITIATIONS, p=UPI_INITIATIONS_P))
    if rail in ("IMPS", "NEFT"):
        return "P2P"
    return ""  # CARD_CNP / CARD_CP: no UPI-style initiation


def remittance_for_regular(rng: np.random.Generator, is_family: bool) -> str:
    if rng.random() < REMIT_BLANK_P:
        return ""
    return str(rng.choice(REMIT_RENT_FAMILY if is_family else REMIT_MERCHANT))


def remittance_urgency(rng: np.random.Generator) -> str:
    return str(rng.choice(REMIT_URGENCY))


def claimed_facts(population: Population, creditor_id: str, debtor_device_id: str,
                   honest: bool = True, rng: Optional[np.random.Generator] = None) -> Dict[str, int]:
    """ClaimedFacts are asserted by the caller and NEVER trusted as feature inputs
    (CLAUDE.md non-negotiable #11; go/internal/contract/event.go:ClaimedFacts). Most legitimate
    traffic simply omits these (the field is optional); a minority honestly reports what the
    client can plausibly know. Fraud typologies may report a deliberately false, reassuring
    claim -- that mismatch against the server-derived truth is itself a signal, never an input.
    """
    out: Dict[str, int] = {}
    if not honest and rng is not None:
        # A manipulated/compromised client lies to look established.
        if rng.random() < 0.6:
            out["creditor_account_opened_ms"] = 0  # left to caller to override if desired
        if rng.random() < 0.6:
            out["device_first_seen_ms"] = 0
        return {k: v for k, v in out.items() if v}
    # Honest but sparse: most clients don't report this at all.
    return {}


def build_event(
    e2e_id: str,
    accepted_at_ms: int,
    rail: str,
    channel: str,
    debtor: Account,
    creditor_account_id: str,
    creditor_vpa: str,
    amount_minor: int,
    device_id: str,
    geo_cell: str,
    asn: int,
    initiation: str,
    remittance_info: str = "",
    event_ts_offset_ms: int = 0,
    creditor_ifsc: str = "",
    session_id: str = "",
    app_version: str = "1.0.0",
    claimed: Optional[Dict[str, int]] = None,
    ip: str = "",
    bank_instance: str = "",
) -> dict:
    claimed_obj = {
        "creditor_account_opened_ms": (claimed or {}).get("creditor_account_opened_ms", 0),
        "device_first_seen_ms": (claimed or {}).get("device_first_seen_ms", 0),
    }
    return {
        "end_to_end_id": e2e_id,
        "event_ts_ms": accepted_at_ms + event_ts_offset_ms,
        "accepted_at_ms": accepted_at_ms,
        "rail": rail,
        "channel": channel,
        "bank_instance": bank_instance or debtor.bank,
        "debtor_account": debtor.account_id,
        "debtor_vpa": debtor.vpa,
        "creditor_account": creditor_account_id,
        "creditor_vpa": creditor_vpa,
        "creditor_ifsc": creditor_ifsc,
        "instructed_amount_minor": amount_minor,
        "currency": "INR",
        "device_id": device_id,
        "ip": ip,
        "asn": asn,
        "geo_cell": geo_cell,
        "session_id": session_id,
        "app_version": app_version,
        "initiation": initiation,
        "remittance_info": remittance_info,
        "claimed": claimed_obj,
        "schema_version": 1,
    }
