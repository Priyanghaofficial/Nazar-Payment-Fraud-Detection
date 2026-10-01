"""Pipeline validation on a REAL labelled fraud dataset (docs/06 §5 claims register:
"Model architecture quality [MEASURED] — IEEE-CIS/ULB time-split... validates the pipeline,
doesn't score your payment").

This is deliberately NOT wired into the live Go scorer: the ULB dataset's 28 PCA-anonymised
components (V1..V28) share no feature identity with Nazar's own registry (features/registry.yaml)
— they're a different domain (generic card-present/CNP transactions, not UPI/IMPS payments).
Its purpose here is narrow and honest: prove the training methodology (time-forward split,
GPU-accelerated gradient boosting, PR-AUC on genuinely imbalanced real fraud) produces a
sane, non-trivial model on data everyone can check — a sanity check on the PIPELINE, stated
exactly that way every time the number is quoted.

Dataset: ULB Credit Card Fraud Detection (Dal Pozzolo et al., via OpenML id=1597), 284,807
transactions over 2 days, 492 fraud (0.173%). Downloaded from openml.org (no auth required).

GPU: trains XGBoost with device="cuda" — this dataset (285k rows) is large enough that the
GPU path is actually exercised, unlike the tiny synthetic Nazar dataset (py/training), where
LightGBM CPU training completes in ~1s and a GPU would sit idle.
"""
from __future__ import annotations

import time
from pathlib import Path

import numpy as np
import pandas as pd
import xgboost as xgb
from scipy.io import arff
from sklearn.metrics import average_precision_score, roc_auc_score

DATA_PATH = Path(__file__).resolve().parents[2] / "data" / "ulb" / "creditcard.arff"
OUT_DIR = Path(__file__).parent / "output"


def main():
    t0 = time.time()
    print(f"loading {DATA_PATH} ...")
    data, meta = arff.loadarff(DATA_PATH)
    df = pd.DataFrame(data)
    df["Class"] = df["Class"].str.decode("utf-8").astype(int)
    print(f"loaded {len(df)} rows, {df['Class'].sum()} fraud ({df['Class'].mean()*100:.4f}%) "
          f"in {time.time()-t0:.1f}s")

    # Time-forward split — docs non-negotiable #10 applied here too, even though this is a
    # sanity-check dataset, not Nazar's own decision data: "Time" is seconds since the first
    # transaction in the dataset, strictly ordered.
    df = df.sort_values("Time").reset_index(drop=True)
    n = len(df)
    train_df = df.iloc[: int(n * 0.7)]
    test_df = df.iloc[int(n * 0.7):]

    features = [c for c in df.columns if c not in ("Class",)]
    X_train, y_train = train_df[features], train_df["Class"]
    X_test, y_test = test_df[features], test_df["Class"]
    print(f"time-forward split: train={len(train_df)} (fraud={y_train.sum()}), "
          f"test={len(test_df)} (fraud={y_test.sum()})")

    gpu_available = False
    try:
        model = xgb.XGBClassifier(
            n_estimators=300, max_depth=6, learning_rate=0.1,
            tree_method="hist", device="cuda",
            eval_metric="aucpr", scale_pos_weight=(y_train == 0).sum() / max((y_train == 1).sum(), 1),
        )
        t1 = time.time()
        model.fit(X_train, y_train)
        gpu_available = True
        print(f"trained on GPU (device=cuda) in {time.time()-t1:.1f}s")
    except Exception as e:
        print(f"GPU training failed ({e}), falling back to CPU")
        model = xgb.XGBClassifier(
            n_estimators=300, max_depth=6, learning_rate=0.1, tree_method="hist",
            eval_metric="aucpr", scale_pos_weight=(y_train == 0).sum() / max((y_train == 1).sum(), 1),
        )
        t1 = time.time()
        model.fit(X_train, y_train)
        print(f"trained on CPU in {time.time()-t1:.1f}s")

    p_test = model.predict_proba(X_test)[:, 1]
    roc_auc = roc_auc_score(y_test, p_test)
    pr_auc = average_precision_score(y_test, p_test)

    print()
    print("=" * 78)
    print("[MEASURED] pipeline validation on ULB real labelled fraud (time-forward split)")
    print(f"  GPU used: {gpu_available}")
    print(f"  ROC-AUC: {roc_auc:.4f}")
    print(f"  PR-AUC:  {pr_auc:.4f}  (baseline PR-AUC at this prevalence = {y_test.mean():.4f})")
    print(f"  n_test={len(y_test)} fraud={int(y_test.sum())}")
    print("  This validates the TRAINING METHODOLOGY (time-split, GBM, imbalanced PR-AUC).")
    print("  It does NOT score a Nazar payment — the feature spaces are unrelated domains.")
    print("=" * 78)

    OUT_DIR.mkdir(exist_ok=True)
    with open(OUT_DIR / "ulb_validation_result.json", "w") as f:
        import json
        json.dump({
            "dataset": "ULB creditcard (OpenML id=1597)", "gpu_used": gpu_available,
            "roc_auc": roc_auc, "pr_auc": pr_auc, "n_test": len(y_test),
            "n_fraud_test": int(y_test.sum()), "tier": "MEASURED",
            "note": "Validates training methodology on real labelled fraud; feature space is "
                    "unrelated to Nazar's own registry and this model is NOT used for live scoring.",
        }, f, indent=2)
    print(f"wrote {OUT_DIR / 'ulb_validation_result.json'}")


if __name__ == "__main__":
    main()
