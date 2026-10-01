"""Train the Nazar P0 model.

Pipeline: replay warmup_events.jsonl then training_events.jsonl (chronological, no gap)
through FeatureStream so every training-period feature is computed from real accumulated
state — then keep only the rows with a label. Time-forward split (train earlier, calibrate/
test later — never the reverse, per docs/03 and CLAUDE.md non-negotiable #8/#10). Trains
LightGBM (CPU: this pip build has no CUDA tree learner, verified at
`python -c "import lightgbm; lightgbm.LGBMClassifier(device='cuda').fit(...)"` — it raises
"CUDA Tree Learner was not enabled in this build"; irrelevant anyway at this row count,
which fits in about a second on CPU), fits a 3-parameter beta calibrator, and exports:

  output/model.txt              LightGBM native text format — loaded by go/internal/scoring
                                 via github.com/dmitryikh/leaves (LGEnsembleFromFile).
  output/model_manifest.json    bundle version + exact feature column order.
  output/calibrator.json        beta calibration (A, B, C) + version.
  output/prevalence.json        train/natural prevalence (docs non-negotiable #9).

Every number this script prints is [RECOVERED] — the labels are this repo's own generator's
ground truth (py/generator), not real-world fraud. That's the honest tier for this data; see
CLAUDE.md and docs/06 §5's claims register.
"""
from __future__ import annotations

import json
import math
import sys
import time
from pathlib import Path

import lightgbm as lgb
import numpy as np
import pandas as pd
import yaml
from sklearn.linear_model import LogisticRegression
from sklearn.metrics import average_precision_score, roc_auc_score

sys.path.insert(0, str(Path(__file__).parent))
from features import FEATURE_IDS, FeatureStream  # noqa: E402

REPO_ROOT = Path(__file__).resolve().parents[2]
# --data-dir lets a bigger regenerated corpus be trained and evaluated side by side with the
# one currently in production, instead of overwriting it before the new model has been judged.
_DATA_ARG = None
for _i, _a in enumerate(sys.argv):
    if _a == "--data-dir" and _i + 1 < len(sys.argv):
        _DATA_ARG = sys.argv[_i + 1]
DATA_DIR = Path(_DATA_ARG) if _DATA_ARG else REPO_ROOT / "data" / "generated"
OUT_DIR = Path(__file__).parent / "output"
REGISTRY_PATH = REPO_ROOT / "features" / "registry.yaml"


def load_jsonl(path: Path):
    with open(path) as f:
        for line in f:
            line = line.strip()
            if line:
                yield json.loads(line)


def load_monotone_constraints() -> list[int]:
    with open(REGISTRY_PATH) as f:
        entries = yaml.safe_load(f)
    mono_by_id = {e["id"]: e.get("monotone", "") for e in entries}
    mapping = {"increasing": 1, "decreasing": -1, "": 0}
    return [mapping.get(mono_by_id.get(fid, ""), 0) for fid in FEATURE_IDS]


def main():
    t_start = time.time()
    OUT_DIR.mkdir(parents=True, exist_ok=True)

    labels: dict[str, dict] = {}
    for row in load_jsonl(DATA_DIR / "training_labels.jsonl"):
        labels[row["end_to_end_id"]] = row
    print(f"loaded {len(labels)} labels")

    stream = FeatureStream()
    rows = []
    n_events = 0
    for ev in load_jsonl(DATA_DIR / "warmup_events.jsonl"):
        stream.compute(ev)  # builds state; warmup has no labels, nothing kept
        n_events += 1
        if n_events % 25000 == 0:
            print(f"  warmup: {n_events} events replayed ({time.time()-t_start:.1f}s)")

    n_labeled = 0
    for ev in load_jsonl(DATA_DIR / "training_events.jsonl"):
        feats = stream.compute(ev)
        n_events += 1
        lab = labels.get(ev["end_to_end_id"])
        if lab is not None:
            row = dict(feats)
            row["end_to_end_id"] = ev["end_to_end_id"]
            row["accepted_at_ms"] = ev["accepted_at_ms"]
            row["label"] = bool(lab["label"])
            row["typology"] = lab["typology"]
            rows.append(row)
            n_labeled += 1

    print(f"replayed {n_events} total events, materialised {n_labeled} labeled training rows "
          f"({time.time()-t_start:.1f}s)")

    df = pd.DataFrame(rows).sort_values("accepted_at_ms").reset_index(drop=True)
    n = len(df)
    n_train = int(n * 0.70)
    n_cal = int(n * 0.85)
    train_df = df.iloc[:n_train]
    cal_df = df.iloc[n_train:n_cal]
    test_df = df.iloc[n_cal:]

    print(f"split: train={len(train_df)} (pos={train_df['label'].sum()}), "
          f"cal={len(cal_df)} (pos={cal_df['label'].sum()}), "
          f"test={len(test_df)} (pos={test_df['label'].sum()})")

    X_train, y_train = train_df[FEATURE_IDS], train_df["label"].astype(int)
    X_cal, y_cal = cal_df[FEATURE_IDS], cal_df["label"].astype(int)
    X_test, y_test = test_df[FEATURE_IDS], test_df["label"].astype(int)

    monotone = load_monotone_constraints()

    model = lgb.LGBMClassifier(
        n_estimators=200, num_leaves=15, learning_rate=0.05,
        min_child_samples=3, min_split_gain=0.0, subsample=0.9, colsample_bytree=0.8,
        objective="binary", monotone_constraints=monotone, monotone_constraints_method="advanced",
        random_state=42, verbosity=-1,
    )
    model.fit(X_train, y_train)

    raw_cal = model.predict_proba(X_cal)[:, 1]
    raw_test = model.predict_proba(X_test)[:, 1]

    if y_test.sum() > 0 and y_test.sum() < len(y_test):
        auc = roc_auc_score(y_test, raw_test)
        pr_auc = average_precision_score(y_test, raw_test)
        print(f"[RECOVERED] held-out (time-forward) test: ROC-AUC={auc:.4f} PR-AUC={pr_auc:.4f} "
              f"n={len(y_test)} positives={int(y_test.sum())} — pipeline validation on generator "
              f"ground truth, NOT a real-world detection rate")
    else:
        print("[RECOVERED] test slice has no positive examples (or all positive) — "
              "AUC undefined; this is a known limitation of a tiny synthetic positive count")

    # ---- beta calibration: fit on the CALIBRATION slice, never train or test ----
    eps = 1e-6
    s = np.clip(raw_cal, eps, 1 - eps)
    X_beta = np.column_stack([np.log(s), -np.log(1 - s)])
    calibrator_version = f"beta-v1-{int(time.time())}"
    try:
        lr = LogisticRegression(C=1.0, max_iter=1000)
        lr.fit(X_beta, y_cal)
        A, B = float(lr.coef_[0][0]), float(lr.coef_[0][1])
        C = float(lr.intercept_[0])
        print(f"fitted beta calibrator: A={A:.4f} B={B:.4f} C={C:.4f}")
    except Exception as e:
        print(f"beta calibration fit failed ({e}) — falling back to identity (A=1,B=1,C=0)")
        A, B, C = 1.0, 1.0, 0.0

    def calibrate(raw: np.ndarray) -> np.ndarray:
        s = np.clip(raw, eps, 1 - eps)
        logit = A * np.log(s) - B * np.log(1 - s) + C
        return 1.0 / (1.0 + np.exp(-logit))

    p_test_cal = calibrate(raw_test)
    ece = expected_calibration_error(y_test.to_numpy(), p_test_cal, n_bins=10)
    print(f"[MEASURED against held-out generator data] ECE after calibration = {ece:.4f}")

    natural_prevalence = float(df["label"].mean())
    print(f"[RECOVERED] natural prevalence in this generator's training slice = {natural_prevalence:.6f} "
          f"({int(df['label'].sum())}/{n})")

    # ---- export ----
    model.booster_.save_model(str(OUT_DIR / "model.txt"))

    bundle_version = f"nazar-v1-{time.strftime('%Y%m%d-%H%M%S')}"
    with open(OUT_DIR / "model_manifest.json", "w") as f:
        json.dump({
            "bundle_version": bundle_version,
            "trained_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "feature_order": FEATURE_IDS,
        }, f, indent=2)

    with open(OUT_DIR / "calibrator.json", "w") as f:
        json.dump({"a": A, "b": B, "c": C, "version": calibrator_version}, f, indent=2)

    with open(OUT_DIR / "prevalence.json", "w") as f:
        json.dump({
            "version": f"prevalence-v1-{time.strftime('%Y%m%d')}",
            "train_prevalence": natural_prevalence,
            "natural_prevalence": natural_prevalence,
        }, f, indent=2)

    # ---- full evaluation report ----
    # Written as a file rather than printed, so the console serves measured numbers and a
    # deleted file makes the number disappear instead of persisting as a stale slide.
    metrics = build_metrics(
        model=model, calibrate=calibrate, df=df, test_df=test_df,
        X_test=X_test, y_test=y_test, raw_test=raw_test, p_test_cal=p_test_cal,
        ece=ece, monotone=monotone, train_df=train_df, cal_df=cal_df,
    )
    with open(OUT_DIR / "metrics.json", "w") as f:
        json.dump(metrics, f, indent=2)
    print(f"wrote metrics.json: ROC-AUC={metrics['roc_auc']:.4f} PR-AUC={metrics['pr_auc']:.4f} "
          f"across {metrics['n_test']} held-out rows ({metrics['n_test_pos']} fraud)")

    print(f"wrote model.txt, model_manifest.json, calibrator.json, prevalence.json to {OUT_DIR}")
    print(f"done in {time.time()-t_start:.1f}s")


def build_metrics(*, model, calibrate, df, test_df, X_test, y_test, raw_test, p_test_cal,
                  ece, monotone, train_df, cal_df) -> dict:
    """Everything the Model Evidence screen shows, computed once, here.

    Two evaluations are reported and never conflated. The headline is the time-forward
    held-out split -- train on earlier, test on strictly later -- which is the only split
    that cannot leak the future and is what CLAUDE.md non-negotiable #8 requires. Stratified
    cross-validation is reported alongside it purely as a stability estimate, because at this
    fraud count a single test slice holds few positives and one unlucky split says little.
    CV is labelled as such and is NOT the headline number.
    """
    out: dict = {}
    y = y_test.to_numpy()
    n_pos = int(y.sum())

    out["n_train"] = int(len(train_df))
    out["n_cal"] = int(len(cal_df))
    out["n_test"] = int(len(y))
    out["n_test_pos"] = n_pos
    out["split"] = "time-forward: train on earliest 70%, calibrate on next 15%, test on latest 15%"

    if 0 < n_pos < len(y):
        out["roc_auc"] = float(roc_auc_score(y, raw_test))
        out["pr_auc"] = float(average_precision_score(y, raw_test))
        out["brier"] = float(np.mean((p_test_cal - y) ** 2))
    else:
        out["roc_auc"] = None
        out["pr_auc"] = None
        out["brier"] = None
        out["note_test"] = "held-out slice contains no positive examples; AUC undefined"
    out["ece"] = float(ece)

    # Operating points: what an analyst team would actually feel, per 10k payments.
    ops = []
    if n_pos > 0:
        for thr in [0.05, 0.10, 0.25, 0.50, 0.75, 0.90]:
            pred = (p_test_cal >= thr)
            tp = int((pred & (y == 1)).sum())
            fp = int((pred & (y == 0)).sum())
            fn = int(((~pred) & (y == 1)).sum())
            prec = tp / (tp + fp) if (tp + fp) else 0.0
            rec = tp / (tp + fn) if (tp + fn) else 0.0
            f1 = 2 * prec * rec / (prec + rec) if (prec + rec) else 0.0
            ops.append({"threshold": thr, "precision": prec, "recall": rec, "f1": f1,
                        "true_positives": tp, "false_positives": fp,
                        "alerts_per_10k": (tp + fp) / max(1, len(y)) * 10000})
    out["operating_points"] = ops

    # Recall per attack type -- the direct answer to "how does it generalise across the
    # different ways payments actually go wrong?"
    per_typ = {}
    if "typology" in test_df.columns and n_pos > 0:
        thr = 0.5
        for typ, grp in test_df[test_df["label"]].groupby("typology"):
            idx = grp.index
            pos = p_test_cal[[test_df.index.get_loc(i) for i in idx]]
            per_typ[str(typ)] = {"n": int(len(idx)), "recall": float((pos >= thr).mean())}
    out["per_typology_recall"] = per_typ

    # Ablation: retrain with whole feature families removed, same split, same seed. If a
    # family can be deleted without moving the numbers, it is not earning its place.
    families = {
        "network / beneficiary graph": [f for f in FEATURE_IDS if f.startswith("payee_")],
        "velocity": [f for f in FEATURE_IDS if "velocity" in f],
        "device & network identity": [f for f in FEATURE_IDS if f.startswith("device_") or f.startswith("asn_")],
        "relationship history": [f for f in FEATURE_IDS if f.startswith("pair_") or f.startswith("payee_is_new")],
    }
    ablation = [{"name": "full feature set", "pr_auc": out["pr_auc"], "recall": _recall_at(p_test_cal, y, 0.5),
                 "precision": _precision_at(p_test_cal, y, 0.5)}]
    if n_pos > 0:
        for label, drop in families.items():
            keep = [f for f in FEATURE_IDS if f not in set(drop)]
            if len(keep) == len(FEATURE_IDS) or not keep:
                continue
            mono = [monotone[FEATURE_IDS.index(f)] for f in keep]
            m2 = lgb.LGBMClassifier(
                n_estimators=200, num_leaves=15, learning_rate=0.05,
                min_child_samples=3, min_split_gain=0.0, subsample=0.9, colsample_bytree=0.8,
                objective="binary", monotone_constraints=mono, monotone_constraints_method="advanced",
                random_state=42, verbosity=-1)
            m2.fit(train_df[keep], train_df["label"].astype(int))
            p2 = m2.predict_proba(X_test[keep])[:, 1]
            ablation.append({
                "name": f"without {label}",
                "pr_auc": float(average_precision_score(y, p2)),
                "recall": _recall_at(p2, y, 0.5),
                "precision": _precision_at(p2, y, 0.5),
                "features_removed": len(drop),
            })
    out["ablation"] = ablation

    # Which features the model actually leans on.
    imp = sorted(zip(FEATURE_IDS, model.booster_.feature_importance(importance_type="gain")),
                 key=lambda kv: -kv[1])
    total = sum(v for _, v in imp) or 1.0
    out["feature_importance"] = [{"feature": k, "gain_share": float(v / total)} for k, v in imp[:15]]

    out["tier"] = "RECOVERED"
    out["tier_note"] = ("Measured on a real time-forward held-out split, but the labels are this "
                        "repo's own generator ground truth. It validates the pipeline; it is not a "
                        "real-world detection rate.")
    out["prevalence"] = float(df["label"].mean())
    out["n_labelled_total"] = int(len(df))
    out["n_fraud_total"] = int(df["label"].sum())
    return out


def _recall_at(p, y, thr):
    pred = p >= thr
    tp = int((pred & (y == 1)).sum()); fn = int(((~pred) & (y == 1)).sum())
    return float(tp / (tp + fn)) if (tp + fn) else 0.0


def _precision_at(p, y, thr):
    pred = p >= thr
    tp = int((pred & (y == 1)).sum()); fp = int((pred & (y == 0)).sum())
    return float(tp / (tp + fp)) if (tp + fp) else 0.0


def expected_calibration_error(y_true: np.ndarray, p: np.ndarray, n_bins: int = 10) -> float:
    bins = np.linspace(0, 1, n_bins + 1)
    ece = 0.0
    n = len(p)
    for i in range(n_bins):
        mask = (p >= bins[i]) & (p < bins[i + 1] if i < n_bins - 1 else p <= bins[i + 1])
        if mask.sum() == 0:
            continue
        bin_acc = y_true[mask].mean()
        bin_conf = p[mask].mean()
        ece += (mask.sum() / n) * abs(bin_acc - bin_conf)
    return ece


if __name__ == "__main__":
    main()
