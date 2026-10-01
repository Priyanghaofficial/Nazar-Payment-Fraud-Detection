"""Mule fan-out -- the flagship demo pattern.

8-20 distinct payer accounts, most of whom have never paid this payee before, send
individually-unremarkable payments to one mule/collector account within a short window
(minutes to a couple hours). The fan-in spike, not any single payment, is the signal
(payee_fanin_1h / payee_fanin_burstiness / payee_inflow_concentration -- docs/02 §4.2).
Optionally the mule forwards the money onward shortly after (cash-out), which is what
payee_fwd_ratio_1h / payee_fwd_latency_p50_s are for.
"""
from __future__ import annotations

from typing import List, Tuple

import numpy as np

import behavior
from population import Population


def inject(population: Population, start_ms: int, end_ms: int, rng: np.random.Generator,
           idc: behavior.IdCounter) -> Tuple[List[dict], List[dict]]:
    if not population.mule_ids:
        return [], []

    mule = population.get(str(rng.choice(population.mule_ids)))

    window_start = int(rng.integers(start_ms, max(start_ms + 1, end_ms - 3 * 3_600_000)))
    window_span_ms = int(rng.integers(5 * 60_000, 120 * 60_000))  # 5min - 2h

    n_payers = int(rng.integers(8, 21))
    payers = rng.choice(population.retail_ids, size=min(n_payers, len(population.retail_ids)),
                         replace=False)

    events: List[dict] = []
    labels: List[dict] = []
    total_in_minor = 0

    for payer_id in payers:
        payer = population.get(str(payer_id))
        ts = window_start + int(rng.integers(0, max(window_span_ms, 1)))
        rail = "UPI" if rng.random() < 0.9 else "IMPS"
        # amounts individually unremarkable relative to the payer's own typical spend
        amount = behavior.sample_amount_minor(
            rng, center_rupees=payer.amt_center_rupees * float(rng.uniform(0.6, 1.3)),
            sigma=0.35, min_rupees=100, max_rupees=9500,
        )
        total_in_minor += amount
        device = behavior.pick_device(rng, payer)
        geo = behavior.jitter_geo_cell(rng, payer.home_lat, payer.home_lon)
        initiation = behavior.pick_initiation(rng, rail)
        remit = "" if rng.random() < 0.6 else str(rng.choice(
            ["payment", "gift", "loan", "help", ""]))

        e2e = idc.next()
        ev = behavior.build_event(
            e2e_id=e2e, accepted_at_ms=ts, rail=rail, channel="MOBILE",
            debtor=payer, creditor_account_id=mule.account_id, creditor_vpa=mule.vpa,
            amount_minor=amount, device_id=device, geo_cell=geo, asn=payer.primary_asn,
            initiation=initiation, remittance_info=remit,
            ip=behavior.fake_ip(rng),
        )
        events.append(ev)
        labels.append({
            "end_to_end_id": e2e,
            "label": True,
            "typology": "mule_fanout",
            "available_at_offset_hours": float(rng.uniform(48, 720)),
        })

    # optional cash-out leg: mule forwards a large share of what it just collected onward,
    # shortly after the last inflow.
    if rng.random() < 0.7:
        last_in_ts = max(e["accepted_at_ms"] for e in events)
        fwd_delay_ms = int(rng.integers(3 * 60_000, 45 * 60_000))
        fwd_ts = last_in_ts + fwd_delay_ms
        cashout_target_id = str(rng.choice(
            [m for m in population.mule_ids if m != mule.account_id] or population.mule_ids))
        cashout_target = population.get(cashout_target_id)
        fwd_amount = int(total_in_minor * float(rng.uniform(0.6, 0.95)))
        fwd_amount = max(fwd_amount, 100)
        device = behavior.pick_device(rng, mule)
        geo = behavior.jitter_geo_cell(rng, mule.home_lat, mule.home_lon)

        e2e = idc.next()
        ev = behavior.build_event(
            e2e_id=e2e, accepted_at_ms=fwd_ts, rail="IMPS", channel="MOBILE",
            debtor=mule, creditor_account_id=cashout_target.account_id,
            creditor_vpa=cashout_target.vpa, amount_minor=fwd_amount, device_id=device,
            geo_cell=geo, asn=mule.primary_asn, initiation="P2P", remittance_info="",
            ip=behavior.fake_ip(rng),
        )
        events.append(ev)
        labels.append({
            "end_to_end_id": e2e,
            "label": True,
            "typology": "mule_fanout",
            "available_at_offset_hours": float(rng.uniform(48, 720)),
        })

    return events, labels
