#!/usr/bin/env python3
"""Main CLI for the Nazar synthetic data generator.

    python generate.py --accounts 2000 --warmup-days 90 --out-dir ../../data/generated --seed 42

Produces, in --out-dir:
    warmup_events.jsonl              90 days of legit traffic (profile-store replay / feature seed)
    training_events.jsonl            a further period mixing legit traffic with low-prevalence
    training_labels.jsonl            fraud injections -- ground truth, NEVER embedded in events
    demo_scenarios.jsonl             a small, hand-picked set covering every typology + normal
    demo_scenarios_labels.jsonl      ground truth for the demo set (debugging/manual use only)

Every number this generator's own labels can produce (e.g. "recovers N% of injected mule-fanout
instances") is [RECOVERED] -- pipeline validation only, never a real-world detection rate
(docs/03 §2.2, CLAUDE.md non-negotiable #13). This script does not compute such a number; it only
manufactures data.

Determinism: identical --seed (and --now-ms, if pinned) always produces byte-identical output.
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import time
from typing import List, Tuple

import numpy as np

import behavior
import warmup as warmup_mod
from population import DAY_MS, Population, generate_population
from typologies import ato_takeover, app_scam, card_testing, merchant_traffic, mule_fanout

REQUIRED_STR_FIELDS = ["end_to_end_id", "rail", "channel", "bank_instance",
                       "debtor_account", "creditor_account", "currency"]
VALID_RAILS = {"UPI", "IMPS", "NEFT", "CARD_CNP", "CARD_CP"}


# ───────────────────────────── validation ─────────────────────────────

def validate_events(events: List[dict], label: str) -> List[str]:
    """In-memory structural checks. Returns a list of problem strings (empty = clean)."""
    problems: List[str] = []
    seen_ids = set()
    for i, ev in enumerate(events):
        for f in REQUIRED_STR_FIELDS:
            if not ev.get(f):
                problems.append(f"{label}[{i}]: missing/empty required field {f!r}")
        eid = ev.get("end_to_end_id")
        if eid in seen_ids:
            problems.append(f"{label}[{i}]: duplicate end_to_end_id {eid!r}")
        seen_ids.add(eid)
        amt = ev.get("instructed_amount_minor")
        if not isinstance(amt, int) or isinstance(amt, bool) or amt <= 0:
            problems.append(f"{label}[{i}]: instructed_amount_minor must be a positive int, got {amt!r}")
        if ev.get("rail") not in VALID_RAILS:
            problems.append(f"{label}[{i}]: invalid rail {ev.get('rail')!r}")
        for acct_field in ("debtor_account", "creditor_account"):
            acct = ev.get(acct_field, "")
            if "-" not in acct:
                problems.append(f"{label}[{i}]: {acct_field} {acct!r} missing '<BANK>-<serial>' convention")
        geo = ev.get("geo_cell", "")
        if geo:
            parts = geo.split(":")
            if len(parts) != 2:
                problems.append(f"{label}[{i}]: geo_cell {geo!r} not 'lat:lon'")
            else:
                try:
                    float(parts[0]); float(parts[1])
                except ValueError:
                    problems.append(f"{label}[{i}]: geo_cell {geo!r} not numeric lat:lon")
        if ev.get("accepted_at_ms", 0) <= 0:
            problems.append(f"{label}[{i}]: accepted_at_ms must be > 0")
        if len(problems) > 200:
            problems.append(f"{label}: too many problems, truncating report")
            break
    return problems


def validate_labels(labels: List[dict], event_ids: set, label: str) -> List[str]:
    problems: List[str] = []
    for i, lb in enumerate(labels):
        eid = lb.get("end_to_end_id")
        if eid not in event_ids:
            problems.append(f"{label}[{i}]: end_to_end_id {eid!r} not found in matching events file")
        if not isinstance(lb.get("label"), bool):
            problems.append(f"{label}[{i}]: 'label' must be a bool")
        if lb.get("typology") not in {"mule_fanout", "app_scam", "ato", "card_testing", "legitimate"}:
            problems.append(f"{label}[{i}]: unexpected typology {lb.get('typology')!r}")
        if len(problems) > 200:
            break
    return problems


# ───────────────────────────── IO ─────────────────────────────

def write_jsonl(path: str, rows: List[dict]) -> None:
    with open(path, "w") as f:
        for row in rows:
            f.write(json.dumps(row, separators=(",", ":")))
            f.write("\n")


def self_check_on_disk(out_dir: str) -> bool:
    """Re-parses every output file from disk and re-runs the structural checks. This is the
    "actually test your own output" pass; run always at the end of generation, and also
    available standalone via --validate-only."""
    ok = True

    def load(name: str) -> List[dict]:
        path = os.path.join(out_dir, name)
        rows = []
        with open(path) as f:
            for lineno, line in enumerate(f, 1):
                line = line.strip()
                if not line:
                    continue
                try:
                    rows.append(json.loads(line))
                except json.JSONDecodeError as e:
                    print(f"  FAIL {name}:{lineno}: invalid JSON: {e}")
                    nonlocal_ok[0] = False
        return rows

    nonlocal_ok = [True]

    warmup_events = load("warmup_events.jsonl")
    training_events = load("training_events.jsonl")
    training_labels = load("training_labels.jsonl")
    demo_events = load("demo_scenarios.jsonl")
    demo_labels = load("demo_scenarios_labels.jsonl")

    problems = []
    problems += validate_events(warmup_events, "warmup_events.jsonl")
    problems += validate_events(training_events, "training_events.jsonl")
    problems += validate_events(demo_events, "demo_scenarios.jsonl")

    train_ids = {e["end_to_end_id"] for e in training_events}
    demo_ids = {e["end_to_end_id"] for e in demo_events}
    problems += validate_labels(training_labels, train_ids, "training_labels.jsonl")
    problems += validate_labels(demo_labels, demo_ids, "demo_scenarios_labels.jsonl")

    if problems or not nonlocal_ok[0]:
        print(f"SELF-CHECK: {len(problems)} problem(s) found:")
        for p in problems[:50]:
            print("  -", p)
        if len(problems) > 50:
            print(f"  ... and {len(problems) - 50} more")
        ok = False
    else:
        print("SELF-CHECK: all files valid JSON, all required fields present, "
              "all amounts positive ints, every label's end_to_end_id found in its events file.")

    typologies_seen = {lb["typology"] for lb in training_labels if lb["label"]} | \
                       {lb["typology"] for lb in demo_labels if lb["label"]}
    expected = {"mule_fanout", "app_scam", "ato", "card_testing"}
    missing = expected - typologies_seen
    if missing:
        print(f"SELF-CHECK WARNING: no positive-label examples found for: {sorted(missing)}")
        ok = False
    else:
        print(f"SELF-CHECK: at least one positive example present for every typology: {sorted(expected)}")

    return ok


# ───────────────────────────── generation phases ─────────────────────────────

def gen_training_period(population: Population, start_ms: int, end_ms: int,
                         master_rng: np.random.Generator, idc: behavior.IdCounter,
                         train_days: int) -> Tuple[List[dict], List[dict]]:
    bg_rng = np.random.default_rng(int(master_rng.integers(0, 2**31 - 1)))
    background = warmup_mod.generate_warmup(population, start_ms, end_ms, bg_rng, idc)

    labels: List[dict] = []
    for ev in background:
        labels.append({
            "end_to_end_id": ev["end_to_end_id"],
            "label": False,
            "typology": "legitimate",
            "available_at_offset_hours": 0.0,
        })

    scale = max(train_days / 14.0, 1.0 / 14.0)
    n_mule = max(1, round(2 * scale))
    n_app_scam = max(1, round(6 * scale))
    n_ato = max(1, round(4 * scale))
    n_card = max(1, round(2 * scale))
    n_merchant_big = max(1, round(1 * scale))

    injected_events: List[dict] = []

    def run(injector_module, n, **kwargs):
        for _ in range(n):
            sub_rng = np.random.default_rng(int(master_rng.integers(0, 2**31 - 1)))
            evs, lbs = injector_module.inject(population, start_ms, end_ms, sub_rng, idc, **kwargs)
            injected_events.extend(evs)
            labels.extend(lbs)

    run(mule_fanout, n_mule)
    run(app_scam, n_app_scam)
    run(ato_takeover, n_ato)
    run(card_testing, n_card)
    run(merchant_traffic, n_merchant_big, min_payers=220)

    all_events = background + injected_events
    all_events.sort(key=lambda e: e["accepted_at_ms"])
    labels_by_id = {lb["end_to_end_id"]: lb for lb in labels}
    ordered_labels = [labels_by_id[e["end_to_end_id"]] for e in all_events]
    return all_events, ordered_labels


def gen_demo_scenarios(population: Population, start_ms: int, end_ms: int,
                        master_rng: np.random.Generator, idc: behavior.IdCounter
                        ) -> Tuple[List[dict], List[dict]]:
    events: List[dict] = []
    labels: List[dict] = []

    # a handful of ordinary, illustrative legitimate transactions
    bg_rng = np.random.default_rng(int(master_rng.integers(0, 2**31 - 1)))
    background = warmup_mod.generate_warmup(population, start_ms, end_ms, bg_rng, idc)
    if background:
        idxs = bg_rng.choice(len(background), size=min(5, len(background)), replace=False)
        normal_sample = [background[int(i)] for i in idxs]
    else:
        normal_sample = []
    for ev in normal_sample:
        events.append(ev)
        labels.append({"end_to_end_id": ev["end_to_end_id"], "label": False,
                        "typology": "legitimate", "available_at_offset_hours": 0.0})

    def run(injector_module, **kwargs):
        sub_rng = np.random.default_rng(int(master_rng.integers(0, 2**31 - 1)))
        evs, lbs = injector_module.inject(population, start_ms, end_ms, sub_rng, idc, **kwargs)
        events.extend(evs)
        labels.extend(lbs)

    run(mule_fanout)
    run(app_scam)
    run(ato_takeover)
    run(card_testing)
    run(merchant_traffic, min_payers=25)

    events.sort(key=lambda e: e["accepted_at_ms"])
    labels_by_id = {lb["end_to_end_id"]: lb for lb in labels}
    ordered_labels = [labels_by_id[e["end_to_end_id"]] for e in events]
    return events, ordered_labels


# ───────────────────────────── main ─────────────────────────────

def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--accounts", type=int, default=2000)
    ap.add_argument("--merchants", type=int, default=50)
    ap.add_argument("--mules", type=int, default=20)
    ap.add_argument("--warmup-days", type=int, default=90)
    ap.add_argument("--train-days", type=int, default=14)
    ap.add_argument("--out-dir", type=str, default="../../data/generated")
    ap.add_argument("--seed", type=int, default=42)
    ap.add_argument("--now-ms", type=int, default=0, help="Pin the 'now' anchor for full determinism; default = current wall clock at generation time.")
    ap.add_argument("--validate", action="store_true", help="Run the on-disk self-check after generating.")
    ap.add_argument("--validate-only", action="store_true", help="Skip generation; just self-check an existing --out-dir.")
    args = ap.parse_args()

    os.makedirs(args.out_dir, exist_ok=True)

    if args.validate_only:
        ok = self_check_on_disk(args.out_dir)
        return 0 if ok else 1

    now_ms = args.now_ms or int(time.time() * 1000)
    train_end_ms = now_ms
    train_start_ms = train_end_ms - args.train_days * DAY_MS
    warmup_end_ms = train_start_ms
    warmup_start_ms = warmup_end_ms - args.warmup_days * DAY_MS
    demo_start_ms = train_end_ms - 2 * DAY_MS
    demo_end_ms = train_end_ms

    master_rng = np.random.default_rng(args.seed)
    idc = behavior.IdCounter()

    print(f"Generating population: {args.accounts} accounts "
          f"({args.merchants} merchants, {args.mules} mules)...")
    population = generate_population(
        n_accounts=args.accounts, n_merchants=args.merchants, n_mules=args.mules,
        seed=int(master_rng.integers(0, 2**31 - 1)), now_ms=now_ms, warmup_days=args.warmup_days,
    )

    print(f"Generating warmup traffic: {args.warmup_days} days...")
    warmup_rng = np.random.default_rng(int(master_rng.integers(0, 2**31 - 1)))
    warmup_events = warmup_mod.generate_warmup(population, warmup_start_ms, warmup_end_ms,
                                                warmup_rng, idc)
    print(f"  -> {len(warmup_events)} warmup events")
    problems = validate_events(warmup_events, "warmup_events (in-memory)")
    if problems:
        print(f"  WARNING: {len(problems)} structural problems in warmup events (showing 5):")
        for p in problems[:5]:
            print("   -", p)

    print(f"Generating training period: {args.train_days} days, low-prevalence typology injection...")
    training_events, training_labels = gen_training_period(
        population, train_start_ms, train_end_ms, master_rng, idc, args.train_days)
    n_fraud = sum(1 for lb in training_labels if lb["label"])
    prevalence = n_fraud / len(training_events) if training_events else 0.0
    print(f"  -> {len(training_events)} training events, {n_fraud} labelled fraud "
          f"({prevalence:.3%} prevalence) [RECOVERED generator ground truth, not a real rate]")
    problems = validate_events(training_events, "training_events (in-memory)")
    problems += validate_labels(training_labels, {e["end_to_end_id"] for e in training_events},
                                 "training_labels (in-memory)")
    if problems:
        print(f"  WARNING: {len(problems)} structural problems in training data (showing 5):")
        for p in problems[:5]:
            print("   -", p)

    print("Generating demo scenarios...")
    demo_events, demo_labels = gen_demo_scenarios(population, demo_start_ms, demo_end_ms,
                                                   master_rng, idc)
    print(f"  -> {len(demo_events)} demo events")

    write_jsonl(os.path.join(args.out_dir, "warmup_events.jsonl"), warmup_events)
    write_jsonl(os.path.join(args.out_dir, "training_events.jsonl"), training_events)
    write_jsonl(os.path.join(args.out_dir, "training_labels.jsonl"), training_labels)
    write_jsonl(os.path.join(args.out_dir, "demo_scenarios.jsonl"), demo_events)
    write_jsonl(os.path.join(args.out_dir, "demo_scenarios_labels.jsonl"), demo_labels)

    print(f"\nWrote files to {os.path.abspath(args.out_dir)}:")
    for name in ["warmup_events.jsonl", "training_events.jsonl", "training_labels.jsonl",
                 "demo_scenarios.jsonl", "demo_scenarios_labels.jsonl"]:
        p = os.path.join(args.out_dir, name)
        print(f"  {name}: {os.path.getsize(p):,} bytes")

    print()
    ok = self_check_on_disk(args.out_dir)
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
