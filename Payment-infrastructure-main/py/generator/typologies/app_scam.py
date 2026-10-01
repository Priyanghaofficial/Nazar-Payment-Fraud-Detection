"""APP (authorised push payment) scam.

A single victim account makes one first-ever payment to a brand-new beneficiary, amount in the
Rs 500-5,000 band, often with urgency-flavored remittance_info. The victim authenticates
normally -- that is the whole point of an APP scam (docs/03 §3.3: "authentication cannot fix a
problem where authentication succeeded"). Label arrives slowly: the victim has to notice and
report.
"""
from __future__ import annotations

from typing import List, Tuple

import numpy as np

import behavior
from population import Population


def inject(population: Population, start_ms: int, end_ms: int, rng: np.random.Generator,
           idc: behavior.IdCounter) -> Tuple[List[dict], List[dict]]:
    victim = population.get(str(rng.choice(population.retail_ids)))

    # brand-new beneficiary: usually a mule/scam-pool account, occasionally a never-paid retail
    # account (romance-scam / fake-marketplace-seller shaped).
    if population.mule_ids and rng.random() < 0.8:
        beneficiary = population.get(str(rng.choice(population.mule_ids)))
    else:
        candidates = [a for a in population.retail_ids
                      if a != victim.account_id and a not in victim.regular_payees]
        beneficiary = population.get(str(rng.choice(candidates)))

    ts = int(rng.integers(start_ms, max(start_ms + 1, end_ms)))
    rail = "UPI" if rng.random() < 0.85 else "IMPS"
    amount = behavior.rupees_to_minor(float(rng.uniform(500, 5000)))
    device = behavior.pick_device(rng, victim)
    geo = behavior.jitter_geo_cell(rng, victim.home_lat, victim.home_lon)
    initiation = behavior.pick_initiation(rng, rail)
    remit = behavior.remittance_urgency(rng)

    # a manipulated app / scam script may falsely reassure that the beneficiary is established
    claimed = behavior.claimed_facts(population, beneficiary.account_id, device,
                                      honest=False, rng=rng)
    if claimed.get("creditor_account_opened_ms", 0) == 0 and rng.random() < 0.5:
        claimed["creditor_account_opened_ms"] = ts - int(rng.integers(200, 1500)) * 86_400_000

    e2e = idc.next()
    ev = behavior.build_event(
        e2e_id=e2e, accepted_at_ms=ts, rail=rail, channel="MOBILE",
        debtor=victim, creditor_account_id=beneficiary.account_id,
        creditor_vpa=beneficiary.vpa, amount_minor=amount, device_id=device,
        geo_cell=geo, asn=victim.primary_asn, initiation=initiation, remittance_info=remit,
        claimed=claimed, ip=behavior.fake_ip(rng),
    )
    label = {
        "end_to_end_id": e2e,
        "label": True,
        "typology": "app_scam",
        "available_at_offset_hours": float(rng.uniform(24, 720)),
    }
    return [ev], [label]
