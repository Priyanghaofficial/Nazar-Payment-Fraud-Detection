"""Warmup engine: ~90 days of entirely legitimate transactions across the population.

This is what would seed the profile store's baselines (median/MAD/p95 amount, hour histogram,
known payee/device sets, pair history) if replayed through the real system (docs/02 §3). Mule
accounts never appear here -- see population.py.

Two traffic mechanisms, both legitimate:
  1. Retail self-initiated activity: each retail account fires ~Poisson(activity_lambda) txns/day,
     paying a regular payee most of the time and an occasional new payee otherwise.
  2. Merchant footfall: each merchant independently draws ~Poisson(popularity) distinct retail
     payers per day making one-off small payments -- this is what gives merchants legitimate high
     fan-in without any of them ever forwarding money onward.
"""
from __future__ import annotations

from typing import List

import numpy as np

import behavior
from population import DAY_MS, Population


def _retail_daily_events(rng: np.random.Generator, population: Population, aid: str,
                          day_start_ms: int, idc: behavior.IdCounter) -> List[dict]:
    acc = population.get(aid)
    n_txn = int(rng.poisson(acc.activity_lambda))
    events = []
    for _ in range(n_txn):
        # 80% of the time pay a regular payee (if any); else an occasional new payee.
        if acc.regular_payees and rng.random() < 0.80:
            payee_id = str(rng.choice(acc.regular_payees))
        else:
            pool = population.retail_ids if rng.random() < 0.5 else population.merchant_ids
            payee_id = str(rng.choice(pool))
            tries = 0
            while payee_id == aid and tries < 5:
                payee_id = str(rng.choice(pool))
                tries += 1
        payee = population.get(payee_id)
        is_family = payee.kind == "retail"

        hour = behavior.sample_hour(rng, acc.peak_hours)
        ts = behavior.sample_ts_on_day(rng, day_start_ms, hour)
        rail = behavior.pick_rail(rng)
        amount = behavior.sample_amount_minor(rng, acc.amt_center_rupees, acc.amt_sigma)
        device = behavior.pick_device(rng, acc)
        geo = behavior.jitter_geo_cell(rng, acc.home_lat, acc.home_lon)
        initiation = behavior.pick_initiation(rng, rail)
        remit = behavior.remittance_for_regular(rng, is_family)

        ev = behavior.build_event(
            e2e_id=idc.next(), accepted_at_ms=ts, rail=rail, channel="MOBILE",
            debtor=acc, creditor_account_id=payee.account_id, creditor_vpa=payee.vpa,
            amount_minor=amount, device_id=device, geo_cell=geo, asn=acc.primary_asn,
            initiation=initiation, remittance_info=remit,
            ip=behavior.fake_ip(rng),
        )
        events.append(ev)
    return events


def _merchant_footfall_events(rng: np.random.Generator, population: Population, mid: str,
                               day_start_ms: int, idc: behavior.IdCounter) -> List[dict]:
    merchant = population.get(mid)
    n_payers = int(rng.poisson(merchant.activity_lambda))
    n_payers = min(n_payers, len(population.retail_ids))
    if n_payers <= 0:
        return []
    payers = rng.choice(population.retail_ids, size=n_payers, replace=False)
    events = []
    for payer_id in payers:
        payer = population.get(str(payer_id))
        hour = behavior.sample_hour(rng, merchant.peak_hours)
        ts = behavior.sample_ts_on_day(rng, day_start_ms, hour)
        rail = "UPI" if rng.random() < 0.85 else str(rng.choice(["CARD_CNP", "CARD_CP"]))
        amount = behavior.sample_amount_minor(rng, merchant.amt_center_rupees, merchant.amt_sigma)
        device = behavior.pick_device(rng, payer)
        geo = behavior.jitter_geo_cell(rng, payer.home_lat, payer.home_lon)
        initiation = behavior.pick_initiation(rng, rail)
        remit = behavior.remittance_for_regular(rng, is_family=False)

        ev = behavior.build_event(
            e2e_id=idc.next(), accepted_at_ms=ts, rail=rail, channel="MOBILE",
            debtor=payer, creditor_account_id=merchant.account_id, creditor_vpa=merchant.vpa,
            amount_minor=amount, device_id=device, geo_cell=geo, asn=payer.primary_asn,
            initiation=initiation, remittance_info=remit,
            ip=behavior.fake_ip(rng),
        )
        events.append(ev)
    return events


def generate_warmup(population: Population, start_ms: int, end_ms: int, rng: np.random.Generator,
                     idc: behavior.IdCounter) -> List[dict]:
    """Generate legitimate traffic for [start_ms, end_ms), one day at a time, sorted output."""
    events: List[dict] = []
    day_start = start_ms - (start_ms % DAY_MS)
    day = day_start
    while day < end_ms:
        for aid in population.retail_ids:
            events.extend(_retail_daily_events(rng, population, aid, day, idc))
        for mid in population.merchant_ids:
            events.extend(_merchant_footfall_events(rng, population, mid, day, idc))
        day += DAY_MS
    events = [e for e in events if start_ms <= e["accepted_at_ms"] < end_ms]
    events.sort(key=lambda e: e["accepted_at_ms"])
    return events
