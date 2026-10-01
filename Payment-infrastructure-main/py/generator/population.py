"""Synthetic account population for the Nazar generator.

Builds ~2,000 accounts with personas: ~95% ordinary retail customers, a small pool of
"merchant" accounts with legitimate high fan-in (no fraud), and a separate, small pool of
mule/scam accounts that are NEVER used as normal-persona payers or payees in warmup traffic --
only the typology injectors (py/generator/typologies/) touch them.

Account-ID convention (matches go/internal/profile/helpers.go:bankOfAccount and the doc-string
there): "<BANK_ID>-<zero-padded serial>", e.g. "BANK_A-000001". The bank prefix is everything
before the first "-". The large majority of accounts are BANK_A (the host bank); a minority are
BANK_B so cross-bank features (payee_distinct_payer_banks_1h, etc.) are meaningful.

geo_cell convention (matches go/internal/features/compute.go:parseGeoCell): "lat:lon".
"""
from __future__ import annotations

import dataclasses
from typing import Dict, List, Tuple

import numpy as np

# A handful of realistic Indian city coordinates. Home base per account is one of these,
# jittered per-transaction by behavior.jitter_geo.
CITIES: Dict[str, Tuple[float, float]] = {
    "bangalore": (12.9716, 77.5946),
    "mumbai": (19.0760, 72.8777),
    "delhi": (28.7041, 77.1025),
    "chennai": (13.0827, 80.2707),
    "hyderabad": (17.3850, 78.4867),
    "pune": (18.5204, 73.8567),
    "kolkata": (22.5726, 88.3639),
    "ahmedabad": (23.0225, 72.5714),
}
CITY_NAMES = list(CITIES.keys())

# Small pool of fictitious ASNs standing in for retail ISPs / mobile carriers.
ASN_POOL: List[int] = list(range(45100, 45116))

DAY_MS = 86_400_000

BANK_HOST = "BANK_A"
BANK_OTHER = "BANK_B"


@dataclasses.dataclass
class Account:
    account_id: str
    bank: str
    kind: str  # "retail" | "merchant" | "mule"
    opened_at_ms: int
    home_city: str
    home_lat: float
    home_lon: float
    amt_center_rupees: float  # typical (median-ish) transaction amount, in rupees
    amt_sigma: float  # per-persona log-normal spread
    peak_hours: List[int]  # 2-3 hours (UTC) where this persona is usually active
    activity_lambda: float  # expected legit txns/day (retail); popularity (merchant fan-in/day)
    devices: List[str]
    primary_asn: int
    vpa: str
    regular_payees: List[str] = dataclasses.field(default_factory=list)  # weighted, retail only


@dataclasses.dataclass
class Population:
    accounts: Dict[str, Account]
    retail_ids: List[str]
    merchant_ids: List[str]
    mule_ids: List[str]
    device_first_seen_ms: Dict[str, int]
    device_owner: Dict[str, str]

    def get(self, account_id: str) -> Account:
        return self.accounts[account_id]


def _make_account_id(bank: str, serial: int) -> str:
    return f"{bank}-{serial:06d}"


def _make_device_id(serial: int) -> str:
    return f"DEV-{serial:06d}"


def generate_population(
    n_accounts: int = 2000,
    n_merchants: int = 50,
    n_mules: int = 20,
    seed: int = 42,
    now_ms: int = 0,
    warmup_days: int = 90,
) -> Population:
    """Build the synthetic population, deterministic given `seed`.

    `now_ms` / `warmup_days` are used only to pick plausible account-opened tenures (accounts
    should mostly predate the warmup window so they have real history by the time warmup starts).
    """
    rng = np.random.default_rng(seed)

    n_retail = n_accounts - n_merchants - n_mules
    if n_retail <= 0:
        raise ValueError("n_accounts must exceed n_merchants + n_mules")

    accounts: Dict[str, Account] = {}
    retail_ids: List[str] = []
    merchant_ids: List[str] = []
    mule_ids: List[str] = []
    device_first_seen_ms: Dict[str, int] = {}
    device_owner: Dict[str, str] = {}

    serial = 1
    device_serial = 1

    def new_devices_for(account_id: str, n: int, opened_at_ms: int) -> List[str]:
        nonlocal device_serial
        devs = []
        for _ in range(n):
            d = _make_device_id(device_serial)
            device_serial += 1
            # a device is first seen at or shortly after the account opens
            first_seen = opened_at_ms + int(rng.integers(0, 5 * DAY_MS))
            device_first_seen_ms[d] = first_seen
            device_owner[d] = account_id
            devs.append(d)
        return devs

    # ---- retail personas -------------------------------------------------
    for _ in range(n_retail):
        bank = BANK_HOST if rng.random() < 0.9 else BANK_OTHER
        aid = _make_account_id(bank, serial)
        serial += 1

        # tenure: mostly well-established (predates warmup by 30d-6y), some newer accounts
        # that opened partway through the warmup window.
        if rng.random() < 0.85:
            tenure_days = int(rng.integers(warmup_days + 30, warmup_days + 2000))
        else:
            tenure_days = int(rng.integers(1, warmup_days + 30))
        opened_at_ms = now_ms - tenure_days * DAY_MS

        city = rng.choice(CITY_NAMES)
        lat, lon = CITIES[city]

        amt_center = float(np.clip(rng.lognormal(mean=np.log(600), sigma=0.9), 50, 25000))
        amt_sigma = float(rng.uniform(0.25, 0.55))

        # most people transact in 1-2 peak hours (commute/evening), some are flatter
        n_peaks = int(rng.choice([1, 2, 3], p=[0.35, 0.45, 0.20]))
        peak_hours = sorted(int(h) for h in rng.choice(range(24), size=n_peaks, replace=False))

        activity_lambda = float(np.clip(rng.gamma(shape=2.0, scale=0.15), 0.02, 2.5))

        n_devices = 1 if rng.random() < 0.88 else 2
        devices = new_devices_for(aid, n_devices, opened_at_ms)

        asn = int(rng.choice(ASN_POOL))
        vpa = f"user{serial}@{bank.lower()}"

        accounts[aid] = Account(
            account_id=aid, bank=bank, kind="retail", opened_at_ms=opened_at_ms,
            home_city=city, home_lat=lat, home_lon=lon,
            amt_center_rupees=amt_center, amt_sigma=amt_sigma,
            peak_hours=peak_hours, activity_lambda=activity_lambda,
            devices=devices, primary_asn=asn, vpa=vpa,
        )
        retail_ids.append(aid)

    # ---- merchant personas -------------------------------------------------
    for _ in range(n_merchants):
        bank = BANK_HOST if rng.random() < 0.9 else BANK_OTHER
        aid = _make_account_id(bank, serial)
        serial += 1

        tenure_days = int(rng.integers(warmup_days + 60, warmup_days + 3000))
        opened_at_ms = now_ms - tenure_days * DAY_MS

        city = rng.choice(CITY_NAMES)
        lat, lon = CITIES[city]

        amt_center = float(np.clip(rng.lognormal(mean=np.log(250), sigma=0.7), 30, 5000))
        amt_sigma = float(rng.uniform(0.3, 0.7))

        peak_hours = sorted(int(h) for h in rng.choice(range(10, 22), size=3, replace=False))
        # activity_lambda here doubles as "popularity": expected distinct payers/day
        activity_lambda = float(np.clip(rng.gamma(shape=3.0, scale=6.0), 5, 60))

        n_devices = int(rng.integers(1, 4))  # POS terminals
        devices = new_devices_for(aid, n_devices, opened_at_ms)

        asn = int(rng.choice(ASN_POOL))
        vpa = f"merchant{serial}@{bank.lower()}"

        accounts[aid] = Account(
            account_id=aid, bank=bank, kind="merchant", opened_at_ms=opened_at_ms,
            home_city=city, home_lat=lat, home_lon=lon,
            amt_center_rupees=amt_center, amt_sigma=amt_sigma,
            peak_hours=peak_hours, activity_lambda=activity_lambda,
            devices=devices, primary_asn=asn, vpa=vpa,
        )
        merchant_ids.append(aid)

    # ---- mule / scam pool (never used as a normal-persona payer/payee) -----
    for _ in range(n_mules):
        bank = BANK_HOST if rng.random() < 0.8 else BANK_OTHER
        aid = _make_account_id(bank, serial)
        serial += 1

        # mule accounts tend to be recently opened relative to when they're used
        tenure_days = int(rng.integers(1, 45))
        opened_at_ms = now_ms - tenure_days * DAY_MS

        city = rng.choice(CITY_NAMES)
        lat, lon = CITIES[city]

        amt_center = float(rng.uniform(200, 3000))
        amt_sigma = 0.6
        peak_hours = sorted(int(h) for h in rng.choice(range(24), size=2, replace=False))
        activity_lambda = 0.0  # mules do not appear in warmup at all

        devices = new_devices_for(aid, 1, opened_at_ms)
        asn = int(rng.choice(ASN_POOL))
        vpa = f"pay{serial}@{bank.lower()}"

        accounts[aid] = Account(
            account_id=aid, bank=bank, kind="mule", opened_at_ms=opened_at_ms,
            home_city=city, home_lat=lat, home_lon=lon,
            amt_center_rupees=amt_center, amt_sigma=amt_sigma,
            peak_hours=peak_hours, activity_lambda=activity_lambda,
            devices=devices, primary_asn=asn, vpa=vpa,
        )
        mule_ids.append(aid)

    # ---- regular payees for retail accounts (rent/family/a couple of merchants) ----
    for aid in retail_ids:
        acc = accounts[aid]
        regulars: List[str] = []
        # 0-1 "rent/family" retail payee
        if rng.random() < 0.7:
            other = rng.choice(retail_ids)
            tries = 0
            while other == aid and tries < 5:
                other = rng.choice(retail_ids)
                tries += 1
            if other != aid:
                regulars.append(other)
        # 1-2 regular merchants
        n_merch = int(rng.choice([1, 2], p=[0.6, 0.4]))
        if merchant_ids:
            chosen = rng.choice(merchant_ids, size=min(n_merch, len(merchant_ids)), replace=False)
            regulars.extend(chosen.tolist())
        acc.regular_payees = regulars

    return Population(
        accounts=accounts,
        retail_ids=retail_ids,
        merchant_ids=merchant_ids,
        mule_ids=mule_ids,
        device_first_seen_ms=device_first_seen_ms,
        device_owner=device_owner,
    )
