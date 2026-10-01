"""Merchant traffic -- NOT fraud.

A "merchant" persona account legitimately receiving many small payments from many distinct
genuine payers over a day, each payer using their own distinct device, no forwarding behaviour.
Exists specifically to validate the system does NOT flag merchants as fraud rings: high fan-in
alone is not the signal (docs/02 §4.2 -- payee_inflow_concentration / forwarding features are
what actually separate a merchant from a mule). Generates at least one clear case with 200+
distinct payers.
"""
from __future__ import annotations

from typing import List, Tuple

import numpy as np

import behavior
from population import Population


def inject(population: Population, start_ms: int, end_ms: int, rng: np.random.Generator,
           idc: behavior.IdCounter, min_payers: int = 220) -> Tuple[List[dict], List[dict]]:
    if not population.merchant_ids:
        return [], []
    merchant = population.get(str(rng.choice(population.merchant_ids)))

    day_start = int(rng.integers(start_ms, max(start_ms + 1, end_ms - 86_400_000)))
    day_start -= day_start % 86_400_000

    n_payers = min(max(min_payers, int(rng.integers(min_payers, min_payers + 80))),
                    len(population.retail_ids))
    payers = rng.choice(population.retail_ids, size=n_payers, replace=False)

    events: List[dict] = []
    labels: List[dict] = []
    for payer_id in payers:
        payer = population.get(str(payer_id))
        hour = behavior.sample_hour(rng, merchant.peak_hours)
        ts = behavior.sample_ts_on_day(rng, day_start, hour)
        rail = "UPI" if rng.random() < 0.85 else str(rng.choice(["CARD_CNP", "CARD_CP"]))
        amount = behavior.sample_amount_minor(rng, merchant.amt_center_rupees, merchant.amt_sigma)
        device = behavior.pick_device(rng, payer)  # each payer uses their OWN distinct device
        geo = behavior.jitter_geo_cell(rng, payer.home_lat, payer.home_lon)
        initiation = behavior.pick_initiation(rng, rail)
        remit = behavior.remittance_for_regular(rng, is_family=False)

        e2e = idc.next()
        ev = behavior.build_event(
            e2e_id=e2e, accepted_at_ms=ts, rail=rail, channel="MOBILE",
            debtor=payer, creditor_account_id=merchant.account_id, creditor_vpa=merchant.vpa,
            amount_minor=amount, device_id=device, geo_cell=geo, asn=payer.primary_asn,
            initiation=initiation, remittance_info=remit,
            ip=behavior.fake_ip(rng),
        )
        events.append(ev)
        labels.append({
            "end_to_end_id": e2e,
            "label": False,
            "typology": "legitimate",
            "available_at_offset_hours": 0.0,
        })

    return events, labels
