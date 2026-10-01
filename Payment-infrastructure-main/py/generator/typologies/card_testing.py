"""Card testing.

On CARD_CNP: a burst of small, rapidly increasing test amounts from one compromised card-like
account to several distinct merchants in a short window -- the classic "validate a stolen card
number" pattern before a real cash-out attempt.
"""
from __future__ import annotations

from typing import List, Tuple

import numpy as np

import behavior
from population import Population


def inject(population: Population, start_ms: int, end_ms: int, rng: np.random.Generator,
           idc: behavior.IdCounter) -> Tuple[List[dict], List[dict]]:
    cardholder = population.get(str(rng.choice(population.retail_ids)))

    n_probes = int(rng.integers(5, 11))
    merchants = rng.choice(population.merchant_ids,
                            size=min(n_probes, len(population.merchant_ids)), replace=False)

    window_start = int(rng.integers(start_ms, max(start_ms + 1, end_ms - 10 * 60_000)))
    device = behavior.pick_device(rng, cardholder)
    geo = behavior.jitter_geo_cell(rng, cardholder.home_lat, cardholder.home_lon)

    # rapidly increasing tiny amounts: 1, ~5, ~15, ~40 rupees ... geometric-ish growth
    base = float(rng.uniform(1, 3))
    growth = float(rng.uniform(2.2, 3.5))

    events: List[dict] = []
    labels: List[dict] = []
    t = window_start
    for i, mid in enumerate(merchants):
        merchant = population.get(str(mid))
        t += int(rng.integers(5_000, 45_000))  # seconds apart, rapid-fire
        rupees = base * (growth ** i)
        amount = behavior.rupees_to_minor(min(rupees, 2000))

        e2e = idc.next()
        ev = behavior.build_event(
            e2e_id=e2e, accepted_at_ms=t, rail="CARD_CNP", channel="WEB",
            debtor=cardholder, creditor_account_id=merchant.account_id,
            creditor_vpa="", amount_minor=amount, device_id=device, geo_cell=geo,
            asn=cardholder.primary_asn, initiation="", remittance_info="",
            ip=behavior.fake_ip(rng),
        )
        events.append(ev)
        labels.append({
            "end_to_end_id": e2e,
            "label": True,
            "typology": "card_testing",
            "available_at_offset_hours": float(rng.uniform(0.05, 2.0)),
        })

    return events, labels
