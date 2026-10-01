"""Account takeover (ATO).

A normally-behaved account suddenly transacts from a new device AND a new geo cell (large
implied travel speed) at an unusual hour, paying a new payee. Unlike an APP scam, the account
owner never intended this payment -- the attacker controls the session.
"""
from __future__ import annotations

from typing import List, Tuple

import numpy as np

import behavior
from population import Population


def inject(population: Population, start_ms: int, end_ms: int, rng: np.random.Generator,
           idc: behavior.IdCounter) -> Tuple[List[dict], List[dict]]:
    victim = population.get(str(rng.choice(population.retail_ids)))

    # brand-new payee -- the attacker's own drop account.
    if population.mule_ids and rng.random() < 0.85:
        payee = population.get(str(rng.choice(population.mule_ids)))
    else:
        candidates = [a for a in population.retail_ids if a != victim.account_id]
        payee = population.get(str(rng.choice(candidates)))

    day_start = int(rng.integers(start_ms, max(start_ms + 1, end_ms - 86_400_000)))
    day_start -= day_start % 86_400_000
    # unusual hour: pick something far from the victim's normal peak hours.
    off_peak = [h for h in range(24) if all(abs(h - p) > 3 for p in victim.peak_hours)] or [3]
    hour = int(rng.choice(off_peak))
    ts = behavior.sample_ts_on_day(rng, day_start, hour)

    rail = "UPI" if rng.random() < 0.8 else "IMPS"
    # attacker often drains a larger-than-typical amount
    amount = behavior.sample_amount_minor(
        rng, center_rupees=victim.amt_center_rupees * float(rng.uniform(3, 12)),
        sigma=0.4, min_rupees=500, max_rupees=200_000,
    )

    # brand-new device, never seen on this account before.
    fake_device = f"DEV-ATO-{int(rng.integers(0, 1_000_000)):06d}"
    # implausible travel: far city from the victim's home.
    geo = behavior.far_geo_cell(rng, victim.home_city)
    initiation = behavior.pick_initiation(rng, rail)

    # a compromised/spoofed client lies about the device's history to look established.
    claimed = behavior.claimed_facts(population, payee.account_id, fake_device,
                                      honest=False, rng=rng)
    if claimed.get("device_first_seen_ms", 0) == 0 and rng.random() < 0.6:
        claimed["device_first_seen_ms"] = ts - int(rng.integers(100, 900)) * 86_400_000

    e2e = idc.next()
    ev = behavior.build_event(
        e2e_id=e2e, accepted_at_ms=ts, rail=rail, channel="MOBILE",
        debtor=victim, creditor_account_id=payee.account_id, creditor_vpa=payee.vpa,
        amount_minor=amount, device_id=fake_device, geo_cell=geo,
        asn=int(rng.integers(45200, 45230)),  # ASN outside the persona's usual pool
        initiation=initiation, remittance_info="", claimed=claimed,
        ip=behavior.fake_ip(rng),
    )
    label = {
        "end_to_end_id": e2e,
        "label": True,
        "typology": "ato",
        "available_at_offset_hours": float(rng.uniform(2, 48)),
    }
    return [ev], [label]
