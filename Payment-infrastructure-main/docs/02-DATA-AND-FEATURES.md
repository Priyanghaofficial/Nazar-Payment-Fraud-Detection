# 02 — Data Model, Profile Store, Feature Layer

**Fixes:** F-32 F-33 F-34 F-35 F-36 F-37 F-38 F-39 F-40 F-41 F-43 F-56 F-57 F-58 F-60 F-61 F-62 F-63 F-64 F-73

---

## Contents

- [1 — Canonical event](#1--canonical-event)
- [2 — Provenance: who controls each field](#2--provenance)
- [3 — Profile store](#3--profile-store)
- [4 — Feature registry](#4--feature-registry)
- [5 — Degenerate-value guards](#5--degenerate-value-guards)
- [6 — Feature integrity](#6--feature-integrity)
- [7 — Postgres schema](#7--postgres-schema)
- [8 — Retention, PII, and DPDP](#8--retention-pii-and-dpdp)

---

## 1 — Canonical event

ISO 20022 (`pain.001`) field naming, kept from the previous design — that part was right. Three
changes, all forced by findings.

```protobuf
// proto/event.v1.proto — TYPE 1 CONTRACT. Additive changes only. Readers ignore unknown fields.
message Event {
  // ── identity ─────────────────────────────────────────────────
  string  end_to_end_id   = 1;   // idempotency key
  int64   event_ts_ms     = 2;   // claimed by the producer — NEVER used for windowing (F-40)
  int64   accepted_at_ms  = 3;   // stamped by Nazar at the trust boundary. THIS drives windows
  Rail    rail            = 4;   // UPI | IMPS | NEFT | CARD_CNP | CARD_CP
  string  channel         = 5;   // MOBILE | WEB | BRANCH | ATM | API
  string  bank_instance   = 6;   // participant id, from a real registry (F-62)

  // ── parties ──────────────────────────────────────────────────
  string  debtor_account   = 10;
  string  debtor_vpa       = 11;
  string  creditor_account = 12;
  string  creditor_vpa     = 13;
  string  creditor_ifsc    = 14;

  // ── money — integer minor units, always ──────────────────────
  int64   instructed_amount_minor = 20;
  string  currency                = 21;  // ISO 4217

  // ── channel context ──────────────────────────────────────────
  string  device_id  = 30;
  string  ip         = 31;
  int32   asn        = 32;
  string  geo_cell   = 33;
  string  session_id = 34;
  string  app_version= 35;

  // ── payment context ──────────────────────────────────────────
  string  initiation      = 40;  // QR | INTENT | COLLECT | P2P | MANDATE
  string  remittance_info = 41;  // ⚠ ATTACKER-CONTROLLED FREE TEXT. Never reaches an LLM.

  // ── claims: asserted by the caller, NOT trusted, kept for the signal in the lie ──
  ClaimedFacts claimed = 50;

  uint32  schema_version = 99;
}

message ClaimedFacts {                 // fixes F-43 / D8
  int64 creditor_account_opened_ms = 1;  // rarely available and not verifiable inter-bank
  int64 device_first_seen_ms       = 2;  // client-asserted; server value always wins
}
```

Three changes from the previous schema, and why:

1. **`accepted_at_ms` is separate from `event_ts_ms`.** Every window, every velocity, every
   baseline uses `accepted_at_ms`, stamped by Nazar. The producer's clock never enters the feature
   layer (F-40). Divergence between the two is itself logged and is a mild signal.
2. **`ClaimedFacts` is quarantined.** The previous schema had `creditor_account_age_days` and
   `device_first_seen` as ordinary fields feeding ATO and APP features — attacker-influenced inputs
   to security decisions (F-43). Server-derived values always win; the claim is retained only
   because a *false* claim is evidence.
3. **`rail` gains `NEFT` and splits `CARD`** into CNP/CP, because `LOSS_GIVEN_FRAUD` already
   distinguishes them and the previous enum did not.

### 1.1 — Rail adapters

`ingest/adapters/{upi,imps,neft,card}.go`, one per rail, each mapping the rail's native message into
`Event`. Every feature, rule, and model consumes the canonical form only. This is the previous
design's §20.6 and it was correct — kept.

**Adapters are the seam that makes new rails a Type 2 decision.** Adding a rail is: one adapter, one
entry in the rail registry, one set of rail-scoped signals. No change to features, decisions, or
storage.

---

## 2 — Provenance

**Missing entirely from the previous documents (F-16), and it is the frame that makes the whole
adversarial story coherent.** Every feature carries a provenance class in the registry, and the
class constrains what the feature is allowed to do.

| Class | Meaning | Constraint |
|---|---|---|
| **`A` — attacker-controlled** | Set directly by the party being scored, per transaction | May never be the *sole* basis for a rung above `STEP_UP`. Monotone constraint required where direction is known |
| **`B` — attacker-shapeable** | Requires days/weeks of investment to move (payee history, pair depth, account age) | May contribute freely; **cost-to-forge is recorded in the registry** |
| **`C` — bank-observed** | Derived from our own history and outcomes; not forgeable by the payer | Highest weight; the only class allowed to solely support a `HOLD` |

Classification of the actual catalogue:

| Class | Features |
|---|---|
| **A** | `amount_*`, all `hour_*`, all `*_velocity_*` (timing is chosen), `device_id`-derived novelty, `asn_*`, `geo_*`, `initiation`, `remittance_*` |
| **B** | `payee_first_seen_by_us_days`, `payee_fanin_*`, `payee_fwd_*`, `pair_*`, `account_age_days`, `device_acct_degree_*`, all ring features |
| **C** | payer's own 90-day baseline (`amt_median`, `amt_mad`, `hour_hist`), `pair_txn_count_90d` on our own settled history, matured labels, settlement outcomes, local blocklist |

**The uncomfortable finding this frame produces, and which you should say out loud:** the majority
of the catalogue is class A or B. The system's real robustness comes from class C, which is thin,
and from the *cost* of shaping class B — which is exactly why beneficiary-side features and
consortium data matter more than payer-side cleverness. This is a stronger version of the previous
documents' APP-scam argument, and it is derived rather than asserted.

---

## 3 — Profile store

### 3.1 — Key layout with hash tags

Hash tags (`{...}`) force co-location so that all keys for one entity land on one slot, which is
what makes the concurrent-pipeline design in [01-LATENCY §3.1](01-LATENCY-RESILIENCE.md#31--why-the-profile-load-is-maxrtt-and-not-one-round-trip)
work.

```
── PAYER group — hash tag {p:<acct>} ──────────────────────────────────
zset  w:{p:<acct>}:txn            member=e2e_id      score=accepted_at_ms
hash  c:{p:<acct>}:amt:m          field=minute_epoch value=paise_sum      ← fixes F-33
hash  c:{p:<acct>}:amt:h          field=hour_epoch   value=paise_sum
hash  b:{p:<acct>}                amt_median amt_mad amt_p95 amt_p99
                                  hour_hist_b64 payee_set_size device_set_size
                                  account_age_days txn_count_lifetime
                                  txn_1h_p999 baseline_version updated_at
set   s:{p:<acct>}:payees         known creditor accounts
set   s:{p:<acct>}:devices
set   s:{p:<acct>}:asns                                                   ← fixes F-34 (asn_is_new)
hash  l:{p:<acct>}                last_ts_ms last_geo_cell last_amt_minor ← fixes F-34
                                                              (dormancy_days, geo_jump_kmh)

── PAYEE group — hash tag {b:<acct>} ──────────────────────────────────
zset  w:{b:<acct>}:payers         member=payer_acct  score=last_paid_ms
zset  w:{b:<acct>}:txn            member=e2e_id      score=accepted_at_ms
hash  c:{b:<acct>}:in:m           field=minute_epoch value=paise_sum
hash  c:{b:<acct>}:out:m          field=minute_epoch value=paise_sum
hash  fwd:{b:<acct>}              fwd_latency_p50_s fwd_ratio_1h          ← fixes F-34
                                  fwd_sample_n fwd_updated_at
str   f:{b:<acct>}:first_seen     accepted_at_ms of first sighting BY US  ← named honestly, D8

── DEVICE group — hash tag {d:<id>} ───────────────────────────────────
zset  w:{d:<id>}:accts            member=acct        score=last_seen_ms
str   f:{d:<id>}:first_seen

── PAIR group — hash tag {r:<payer>:<payee>} ──────────────────────────
hash  pr:{r:<payer>:<payee>}      txn_count_90d amt_p95_minor last_ts_ms
                                  last_disposition first_added_ms          ← fixes F-34, F-43
                                  (only materialised at txn_count ≥ 2 — see 01-LATENCY §8.2)

── ASN ────────────────────────────────────────────────────────────────
zset  w:asn:<asn>:accts           member=acct        score=last_seen_ms

── IDEMPOTENCY ────────────────────────────────────────────────────────
str   i:{e:<e2e_id>}              serialised Decision, TTL 24h
```

**Every feature in the registry (§4) has a backing key above.** This is enforced by the
`feature_catalogue_key_coverage` test — the 30-line test that would have caught the previous
design's six orphaned features (F-34) before anyone wrote a line of scoring code.

### 3.2 — Windowed sums: bucketed counters, not sorted sets

The previous design's `w:payer:{acct}:amt` was structurally incapable of producing a windowed sum
(F-33): a sorted set has one score per member, and Redis has no sum-over-range.

```
  Minute buckets:  HINCRBY c:{p:<acct>}:amt:m <floor(ts/60000)> <paise>
                   HEXPIRE  ... 90000                      (Redis 7.4 field TTL)

  Read 1 h:        HMGET  c:{p:<acct>}:amt:m <60 consecutive minute keys>  → sum in-process
  Read 24 h:       HMGET  c:{p:<acct>}:amt:h <24 hour keys>
  Read 30 d:       HMGET  c:{p:<acct>}:amt:d <30 day keys>
```

Properties: exact integer arithmetic (no float, honouring the doc's own money rule — F-64), O(k)
where k is the bucket count and not the transaction count, trivially expirable per field, and
network cost independent of how busy the customer is. A 1-hour read is 60 small integers — roughly
600 bytes — regardless of whether the account did 2 transactions or 2,000.

Rollup (minute → hour → day) runs in `profile-apply`, off the request path.

### 3.3 — Distinct counts: `ZCOUNT`, never `ZCARD`

**The previous design's single worst silent bug (F-32):** `ZCARD` ignores scores, and the trim uses
one MAXWINDOW per key, so `payee_fanin_1h` — the feature the entire APP-scam pitch rests on — was
computed over 24 hours.

```
  Write:   ZADD  w:{b:<acct>}:payers  <accepted_at_ms>  <payer_acct>      (idempotent: overwrites)
  Read 1h: ZCOUNT w:{b:<acct>}:payers ( <now-3600000>  <now>              ← EXCLUSIVE lower bound
  Read 24h:ZCOUNT w:{b:<acct>}:payers ( <now-86400000> <now>
  Trim:    ZREMRANGEBYSCORE w:{b:<acct>}:payers 0 <now - MAX_WINDOW>      (on write, amortised)
  TTL:     EXPIRE MAX_WINDOW + 10%
```

`ZCARD` is **banned from the codebase** by a lint rule, with a comment pointing at this section.
The semantics: member = payer id, score = that payer's most recent payment to this payee, so
`ZCOUNT` over a window is exactly "distinct payers whose most recent payment falls in the window."

**On HyperLogLog (F-39):** do not sketch a quantity a threshold rule reads. Fan-in values of
interest are 8–20, and the question is whether the sketch returns 7 or 8, not what its relative
error is. Exact zsets at `[P0]` and `[P1]`; at `[P2]`, exact below a cardinality cap with an
explicit `HIGH_CARDINALITY` sentinel above it — which is the honest structure, because a payee with
50,000 distinct payers is a merchant and the exact number is irrelevant.

### 3.4 — Point-in-time: read strictly before write

The previous design never specified the ordering, which determines whether every velocity feature
is off by one (F-41).

```
  1. READ   all windows/baselines/counters  →  state STRICTLY BEFORE this event
  2. SCORE  using that state
  3. DECIDE, respond
  4. APPLY  this event to the store (async, via profile-apply)
```

**Rule: the transaction being scored is never in its own features.** Written into the feature
registry as an invariant and property-tested with a synthetic stream where the expected value is
known by construction.

`profile-apply` is a separate consumer of the event log. It applies **every** event including those
decided rails-only during degradation, so the store stays complete even when scoring was degraded.

### 3.5 — Local filters

Blocklists move out of Redis and into every decision process (fixes F-36, and enables the
zero-I/O rails path that makes the deadline guarantee possible —
[01-LATENCY §2](01-LATENCY-RESILIENCE.md#2--the-deadline-and-the-timeout-action)):

```
  In-process cuckoo filter, ~12 bits/entry, FPR ≈ 0.1%
    ├─ bl_local        locally confirmed beneficiaries
    ├─ bl_consortium   ≥2 independent reporters
    └─ watchlist       trending-toward-mule, advisory only

  Refresh: full snapshot on start + delta stream via Redis pub/sub (< 1 s propagation)
  Miss  → definitively not listed.        Skip the round trip.
  Hit   → MAYBE listed. Confirm exactly against `blk:<token>` before ANY rail fires.
```

**A filter hit is never authoritative** (fixes F-54). At 0.1% FPR and `[P1]` volume that is ~430k
false hits per day; each costs one extra Redis GET and zero customer impact. A design that lets a
probabilistic structure block a payment is producing wrongful blocks with a confident
cross-institutional explanation attached.

### 3.6 — The read, assembled

```go
// Declarative spec — zipped by NAME, never by position. Fixes F-37.
type Req struct{ Name string; Cmd string; Args []any }

func (s *RedisProfileStore) Load(ctx context.Context, ev *Event) (*ProfileBundle, error) {
    groups := s.plan(ev)              // 4–5 groups keyed by hash tag; conditional keys omitted
    replies := s.runConcurrent(ctx, groups)   // wall clock = max(RTT), not sum
    return bindByName(replies)        // panics in test builds on any name mismatch or arity error
}
```

Conditional commands (skip the device group when `device_id` is absent — required by D5) shift
positional indices and silently misassign every subsequent feature in a positional design. Name
binding makes that impossible. The bug class it prevents — **silent feature corruption feeding a
calibrated probability and a rupee threshold** — is the worst one available in this system.

---

## 4 — Feature registry

Features are declared as data, not as code scattered through the scorer. This is the seam that
makes feature changes a Type 2 decision.

```yaml
# features/registry.yaml — one entry per feature. Machine-read by trainer, scorer, and tests.
- id: payee_fanin_1h
  version: 2                       # bump on ANY semantic change; old id stays valid for replay
  expr: zcount(w:{b:creditor}:payers, now-1h, now)
  requires_keys: [w:{b:creditor}:payers]
  provenance: B                    # attacker-shapeable
  cost_to_forge: "8+ distinct funded accounts, coordinated within 1h"
  monotone: increasing             # → LightGBM monotone constraint
  rails: [UPI, IMPS, NEFT]
  na_when: []
  guard: none
  catches: [mule_fanout, app_scam]
```

### 4.1 — Payer behaviour

| Feature | Definition | Prov. | Mono. | Guard |
|---|---|---|---|---|
| `amt_robust_z` | `0.6745·(amt − median)/mad_eff` | A | ↑ | §5.1 |
| `amt_over_p95` | `amt / p95`, clipped 20 | A | ↑ | §5.2 |
| `hour_surprisal` | `−log₂(P(hour) )`, Laplace-smoothed | A | ↑ | replaces `hour_rarity` (F-25) |
| `txn_velocity_1m/5m/1h/24h` | `ZCOUNT` window | A | ↑ | — |
| `amt_velocity_1h/24h` | bucketed sum ÷ 30 d daily mean | A | ↑ | §5.2 |
| `account_age_days` | from baseline | C | ↓ | — |
| `dormancy_days` | `(now − l:{p}.last_ts)/86400` | C | ↑ | `NA` if no prior |
| `baseline_staleness_h` | `(now − b.updated_at)/3600` | C | — | **new** — a stale baseline is a fact the model should see |

### 4.2 — Counterparty (the APP-scam block)

| Feature | Definition | Prov. | Mono. | Guard |
|---|---|---|---|---|
| `payee_is_new_to_payer` | `NOT SISMEMBER s:{p}:payees` | B | ↑ | — |
| `payee_first_seen_by_us_days` | `(now − f:{b}.first_seen)/86400` | B | ↓ | **renamed** from `payee_age_days` (D8, F-43) |
| `payee_fanin_1h` | `ZCOUNT ... 1h` | B | ↑ | §3.3 |
| `payee_fanin_24h` | `ZCOUNT ... 24h` | B | ↑ | §3.3 |
| `payee_fanin_burstiness` | `fanin_1h / (fanin_24h/24)` **gated on `fanin_24h ≥ 6`** | B | ↑ | §5.3 — fixes F-24 |
| `pair_txn_count_90d` | `pr:{r}.txn_count_90d` | C | ↓ | — |
| `pair_amt_ratio_p95` | `amt / pr.amt_p95_minor` | B | ↑ | §5.2 |
| `payee_fwd_latency_p50_s` | `fwd:{b}.fwd_latency_p50_s` | B | ↓ | §5.4 |
| `payee_fwd_ratio_1h` | `Σout/Σin` from minute buckets | B | ↑ | §5.4 |
| `payee_inflow_concentration` | HHI over payer share of 24 h inflow | B | ↓ | **new** — separates a merchant (many small payers, low HHI) from a mule collecting from a ring |
| `payee_distinct_payer_banks_1h` | distinct participant ids | B | ↑ | **new** — cross-bank fan-in is much harder to fake than same-bank |

> `payee_inflow_concentration` and `payee_distinct_payer_banks_1h` are the two features that do the
> work the previous design assigned to a weight-1.0 shared-beneficiary ring edge (F-26). A merchant
> and a mule both have high fan-in; they differ in *concentration*, *forwarding*, and *tenure*.
> Encoding that as continuous features is correct; encoding it as a hard ring rail is not.

### 4.3 — Channel

| Feature | Definition | Prov. | Mono. | Guard |
|---|---|---|---|---|
| `device_is_new_to_payer` | `NOT SISMEMBER s:{p}:devices` | A | ↑ | `NA` if no `device_id` |
| `device_first_seen_hours` | server-derived, claim ignored | A | ↓ | `NA` if absent |
| `device_acct_degree_24h` | `ZCOUNT w:{d}:accts 24h` | B | ↑ | — |
| `asn_is_new_to_payer` | `NOT SISMEMBER s:{p}:asns` | A | ↑ | `NA` if absent |
| `asn_acct_degree_1h` | `ZCOUNT w:asn:<asn>:accts 1h` | B | ↑ | — |
| `geo_jump_kmh` | haversine(`l:{p}.last_geo`, geo) ÷ max(Δt, 60 s) | A | ↑ | §5.5 |

### 4.4 — Graph

`ring_score`, `ring_size`, `component_bank_count`, `hops_to_cashout`, `device_shared_degree` — all
class B, all carrying an explicit `graph_staleness_s` companion feature so the model can learn to
discount stale graph signal (fixes F-30). See [05-GRAPH-CONSORTIUM](05-GRAPH-CONSORTIUM.md).

### 4.5 — Rule-features

Every rule predicate is **also** exposed as a boolean feature into the model
(`rf_<rule_id>`). This is what makes double-counting structurally impossible and lets the whole
composition layer be deleted — see [03-ML-PIPELINE §5](03-ML-PIPELINE.md#5--rules).

---

## 5 — Degenerate-value guards

The previous design's §06 got this genuinely right in principle (`MAD = 0` is common; an off-scale
value is a data problem, not a signal). Kept, extended to every ratio, and each guard emits
`NOT_EVALUATED` — never a silent zero and never `CLEAR` (D5).

### 5.1 — `amt_robust_z`

```
mad_eff = max(mad, 0.02 × median, 100)          # ≥ ₹1 in paise
z       = 0.6745 × (amt − median) / mad_eff
if !isfinite(z) or |z| > 25 → NOT_EVALUATED, reason=OFF_SCALE
if baseline.txn_count_lifetime < 10 → NOT_EVALUATED, reason=COLD_START
```

The `|z| > 25` gate keeps the previous design's discrimination, which is correct and worth keeping
verbatim: **off by orders of magnitude is a bug; off by a factor of six is a fraud.** New: a
minimum-sample gate, because a median over 3 transactions is not a baseline.

### 5.2 — Ratio guards

| Feature | Degenerate case | Guard |
|---|---|---|
| `amt_over_p95` | `p95 = 0` (new account) | `NOT_EVALUATED` |
| `pair_amt_ratio_p95` | `pair.amt_p95 = 0` or `txn_count < 3` | `NOT_APPLICABLE` |
| `amt_velocity_1h` | 30 d mean = 0 | `NOT_EVALUATED` |

### 5.3 — `payee_fanin_burstiness`

Requires `fanin_24h ≥ 6`, else `NOT_APPLICABLE`. Without the gate, every payee first seen within
the hour maxes the feature and it degenerates into a proxy for payee tenure (F-24).

### 5.4 — Forwarding features

| Feature | Guard |
|---|---|
| `payee_fwd_ratio_1h` | inflow ≥ ₹100 **and** `fwd_sample_n ≥ 3`, else `NOT_APPLICABLE` |
| `payee_fwd_latency_p50_s` | `fwd_sample_n ≥ 3`, else `NOT_APPLICABLE` |
| both | `fwd_updated_at` older than 15 min → `NOT_EVALUATED`, reason `STALE` |

### 5.5 — `geo_jump_kmh`

Floor Δt at 60 s. `NOT_APPLICABLE` when either geo_cell is absent, when the cells are identical, or
when cell resolution exceeds 25 km — an "impossible travel" computed from two coarse cells is
noise, and the previous design would have shipped it as a feature.

### 5.6 — Cold start

Pass `NaN` to LightGBM (which handles it natively — the previous design was right), emit a
`COLD_START` finding, and set `cold_start_features_n` as an explicit feature so the model can learn
how much to discount a sparse vector. **Do not pass `NaN` to any component that cannot accept it** —
see [03-ML-PIPELINE §6](03-ML-PIPELINE.md#6--novelty), where this crashed the previous design's
novelty lane on the entire cold-start population (F-18).

---

## 6 — Feature integrity

**The best idea in the previous documents (§20.2), kept and completed.** Every windowed feature has
two derivations — the streaming counter and a recompute from Postgres — and they diverge silently
in production via TTL bugs, dropped events, clock skew, and lost checkpoints. A counter reading low
looks exactly like a quiet customer.

```go
// Sampled at 1% of decisions, entirely off the request path.
func integrityCheck(ctx context.Context, d *Decision) {
    for _, f := range integrityCheckedFeatures {          // the ~12 windowed ones
        streamed := d.Features[f.ID]
        batch    := recomputeFromPostgres(ctx, f, d.Entity, d.AcceptedAt)  // point-in-time
        if diverges(streamed, batch, f.Tolerance) {
            emit(FeatureDrift{Feature: f.ID, Streamed: streamed, Batch: batch, E2E: d.E2EID})
        }
    }
}
```

Escalation ladder:

| Divergence rate (1 h rolling) | Action |
|---|---|
| < 0.5% | Log only |
| 0.5–2% | Alarm; annotate the governance screen |
| > 2% sustained 15 min | **Feature → `NOT_EVALUATED` for new transactions** until recovery, model scores without it, decisions tagged |

The recompute SQL is the actual work here and it is what makes the check real rather than a
paragraph. One query per checked feature, point-in-time as of `accepted_at`:

```sql
-- payee_fanin_1h, recomputed
SELECT count(DISTINCT debtor_account)
FROM   transactions
WHERE  creditor_account = $1
  AND  ts >  $2::timestamptz - interval '1 hour'
  AND  ts <= $2::timestamptz;
```

> Had this existed in the previous design, it would have fired on **100% of samples on day one**,
> because the streaming derivation used `ZCARD` over a 24-hour trim window while claiming a 1-hour
> feature (F-32). That is the whole argument for building it in week one rather than week six.

---

## 7 — Postgres schema

```sql
-- ══════════════════════════════════════════════════════════════════════
--  TRANSACTIONS — daily partitions
-- ══════════════════════════════════════════════════════════════════════
CREATE TABLE transactions (
  end_to_end_id     TEXT        NOT NULL,
  accepted_at       TIMESTAMPTZ NOT NULL,
  event_ts          TIMESTAMPTZ,                    -- producer-claimed, not authoritative
  rail              TEXT        NOT NULL,
  channel           TEXT        NOT NULL,
  bank_instance     TEXT        NOT NULL REFERENCES participants(id),   -- F-62
  debtor_account    TEXT        NOT NULL,
  creditor_account  TEXT        NOT NULL,
  creditor_vpa      TEXT,
  creditor_ifsc     TEXT,
  amount_minor      BIGINT      NOT NULL CHECK (amount_minor > 0),
  currency          CHAR(3)     NOT NULL DEFAULT 'INR',
  device_id         TEXT,
  ip                INET,
  asn               INTEGER,
  geo_cell          TEXT,
  session_id        TEXT,
  initiation        TEXT,
  remittance_hash   BYTEA,        -- hash only. Raw attacker text is NOT stored here (B5)
  schema_version    INTEGER     NOT NULL,
  PRIMARY KEY (end_to_end_id, accepted_at)
) PARTITION BY RANGE (accepted_at);

CREATE INDEX ON transactions (debtor_account, accepted_at DESC);
CREATE INDEX ON transactions (creditor_account, accepted_at DESC);

-- ══════════════════════════════════════════════════════════════════════
--  DECISIONS — append-only, MULTIPLE ROWS PER TRANSACTION (fixes F-58)
-- ══════════════════════════════════════════════════════════════════════
CREATE TYPE decision_kind AS ENUM ('LIVE','SHADOW','REPLAY','RESOLUTION','CONTROL');

CREATE TABLE decisions (
  end_to_end_id        TEXT          NOT NULL,
  decision_seq         INTEGER       NOT NULL,       -- 0 = first live decision
  kind                 decision_kind NOT NULL,
  decided_at           TIMESTAMPTZ   NOT NULL,
  accepted_at          TIMESTAMPTZ   NOT NULL,

  action               TEXT NOT NULL,        -- ALLOW|ALLOW_MONITOR|STEP_UP|
                                             -- STEP_UP_INTERSTITIAL|HOLD|BLOCK|CAP
  pre_advisory_action  TEXT NOT NULL,        -- what OUR data alone concluded
  rail_fired           TEXT,
  reason_codes         TEXT[] NOT NULL,

  p_model              DOUBLE PRECISION,     -- calibrated. p_final ≡ p_model (F-10)
  p_prevalence_adj     DOUBLE PRECISION,     -- after prior correction (F-07)
  expected_loss_minor  BIGINT,
  expected_cost_minor  BIGINT,               -- incl. friction — the real objective (F-48)

  features             JSONB NOT NULL,       -- hot partitions only; archived to Parquet
  feature_status       JSONB NOT NULL,       -- per feature: OK|NA|NOT_EVALUATED + reason (D5)
  feature_staleness    JSONB NOT NULL,       -- per source: seconds stale at decision time (D2)
  contributions        JSONB,                -- exact TreeSHAP
  findings             JSONB NOT NULL,

  -- reproducibility: every dial that produced this row
  model_bundle_version    TEXT NOT NULL,
  policy_version          TEXT NOT NULL,
  rules_version           TEXT NOT NULL,
  signal_registry_version TEXT NOT NULL,

  -- feedback-loop controls (fixes F-12)
  is_control           BOOLEAN NOT NULL DEFAULT false,
  action_propensity    DOUBLE PRECISION,     -- P(this action | policy) for off-policy eval

  degraded             TEXT[] NOT NULL DEFAULT '{}',
  total_ms             DOUBLE PRECISION NOT NULL,
  queue_delay_ms       DOUBLE PRECISION NOT NULL,
  service_ms           DOUBLE PRECISION NOT NULL,

  -- audit chain, per shard (fixes F-59)
  decision_shard       SMALLINT NOT NULL,
  chain_seq            BIGINT   NOT NULL,
  prev_hash            BYTEA,
  hash                 BYTEA    NOT NULL,
  checkpoint_id        BIGINT,

  PRIMARY KEY (end_to_end_id, decision_seq)
) PARTITION BY RANGE (decided_at);

CREATE UNIQUE INDEX ON decisions (decision_shard, chain_seq);
CREATE INDEX ON decisions (decided_at DESC) WHERE kind = 'LIVE';
CREATE INDEX ON decisions (action, decided_at DESC) WHERE kind = 'LIVE';

-- ══════════════════════════════════════════════════════════════════════
--  OUTCOMES — what actually happened (missing entirely before: F-60)
-- ══════════════════════════════════════════════════════════════════════
CREATE TABLE transaction_outcomes (
  end_to_end_id     TEXT PRIMARY KEY,
  settled           BOOLEAN,
  settled_at        TIMESTAMPTZ,
  settled_amount_minor BIGINT,               -- differs from instructed when CAP applied
  step_up_issued    BOOLEAN NOT NULL DEFAULT false,
  step_up_result    TEXT,                    -- PASSED|ABANDONED|FAILED|EXPIRED
  step_up_latency_ms INTEGER,
  step_up_attempts  SMALLINT,
  interstitial_shown BOOLEAN NOT NULL DEFAULT false,
  interstitial_result TEXT,                  -- PROCEEDED|CANCELLED
  recall_attempted  BOOLEAN NOT NULL DEFAULT false,
  recall_result     TEXT,
  recovered_minor   BIGINT NOT NULL DEFAULT 0,
  updated_at        TIMESTAMPTZ NOT NULL
);
```

Without this table, `value_recall`, `step_up_pass_rate`, and the entire step-up-outcome control
loop are uncomputable — and all three appear as headline metrics in the previous documents.

```sql
-- ══════════════════════════════════════════════════════════════════════
--  LABELS — valid DDL, and a guard that guards the right thing
-- ══════════════════════════════════════════════════════════════════════
CREATE TABLE labels (
  end_to_end_id TEXT PRIMARY KEY,
  label         BOOLEAN     NOT NULL,
  source        TEXT        NOT NULL,   -- ANALYST|CHARGEBACK|CONFIRMED_MULE|VICTIM_REPORT|LEA
  confidence    REAL        NOT NULL DEFAULT 1.0,
  labelled_at   TIMESTAMPTZ NOT NULL,
  available_at  TIMESTAMPTZ NOT NULL,   -- when this label would REALISTICALLY be known
  labelled_by   TEXT,
  superseded_by BIGINT                  -- labels get revised; keep the history
);
CREATE INDEX ON labels (available_at);
-- NO generated column. The previous DDL was invalid Postgres (F-56): a STORED generated
-- column must be IMMUTABLE and now() is STABLE. Use a view for the dashboard counter:
CREATE VIEW labels_matured AS SELECT * FROM labels WHERE available_at <= now();
```

**And the guard that actually prevents the leak (fixes F-57).** The named risk is training on a
label that had not arrived *at the decision's own timestamp*. That is a point-in-time join per row,
not a global boolean:

```sql
-- The ONLY sanctioned training query shape.
SELECT d.features, l.label
FROM   decisions d
JOIN   labels    l USING (end_to_end_id)
WHERE  d.kind IN ('LIVE','CONTROL')
  AND  d.decided_at <  $train_as_of
  AND  l.available_at <= $train_as_of          -- known by training time
  AND  l.available_at >  d.decided_at;          -- and NOT known at decision time
```

The third clause is the one the previous design was missing, and it is the difference between a
guard and a comment. Enforced by `test_training_query_is_point_in_time`, which fails CI if any
training path issues a query without it.

```sql
-- ══════════════════════════════════════════════════════════════════════
--  CASES
-- ══════════════════════════════════════════════════════════════════════
CREATE TABLE cases (
  id                  BIGSERIAL PRIMARY KEY,
  opened_at           TIMESTAMPTZ NOT NULL,
  updated_at          TIMESTAMPTZ NOT NULL,
  status              TEXT NOT NULL,
  typology            TEXT NOT NULL,
  anchor_kind         TEXT NOT NULL,
  anchor_id           TEXT NOT NULL,
  exposure_minor      BIGINT NOT NULL,      -- rolling, recomputed as alerts join
  sla_due_at          TIMESTAMPTZ,
  assigned_to         TEXT,
  ring_id             BIGINT,
  narrative           TEXT,
  narrative_version   INTEGER NOT NULL DEFAULT 0,   -- regenerated on change (F-63)
  narrative_source    TEXT NOT NULL DEFAULT 'TEMPLATE'
);
CREATE INDEX ON cases (status, exposure_minor DESC);

CREATE TABLE alerts (
  id            BIGSERIAL PRIMARY KEY,
  case_id       BIGINT REFERENCES cases,
  end_to_end_id TEXT NOT NULL,
  decision_seq  INTEGER NOT NULL,
  raised_at     TIMESTAMPTZ NOT NULL,
  severity      TEXT NOT NULL,
  FOREIGN KEY (end_to_end_id, decision_seq) REFERENCES decisions
);

CREATE TABLE dispositions (
  id         BIGSERIAL PRIMARY KEY,
  case_id    BIGINT NOT NULL REFERENCES cases,
  analyst    TEXT NOT NULL,
  approver   TEXT,                         -- four-eyes on blocklist/consortium effects
  action     TEXT NOT NULL,
  reason     TEXT NOT NULL,
  at         TIMESTAMPTZ NOT NULL,
  propagated JSONB
);

-- ══════════════════════════════════════════════════════════════════════
--  CONFIG AUDIT — the half the previous design did not audit (F-71)
-- ══════════════════════════════════════════════════════════════════════
CREATE TABLE config_changes (
  id            BIGSERIAL PRIMARY KEY,
  at            TIMESTAMPTZ NOT NULL,
  bundle        TEXT NOT NULL,      -- policy | rules | model | signal_registry
  from_version  TEXT,
  to_version    TEXT NOT NULL,
  proposed_by   TEXT NOT NULL,
  approved_by   TEXT NOT NULL CHECK (approved_by <> proposed_by),   -- four-eyes, in the schema
  diff          JSONB NOT NULL,
  prev_hash     BYTEA,
  hash          BYTEA NOT NULL      -- same chain discipline as decisions
);
```

---

## 8 — Retention, PII, and DPDP

The previous design argued DPDP compliance for the consortium wire and stored everything in
cleartext forever inside the perimeter (F-73).

| Class | Fields | At rest | Retention | Analytics/training |
|---|---|---|---|---|
| **Direct identifiers** | `debtor_account`, `creditor_account`, `creditor_vpa`, `device_id`, `ip` | Column encryption (pgcrypto or app-layer AEAD, KMS-held keys) | 90 d hot, 7 y archive (regulatory) | **Tokenised** — training never sees raw identifiers |
| **Quasi-identifiers** | `geo_cell`, `asn`, `session_id` | Plain | 90 d | Coarsened |
| **Attacker-controlled text** | `remittance_info` | **Hash only in the system of record**; raw kept 7 d in a separate quarantined store for investigation | 7 d | **Never** |
| **Derived** | features, scores, decisions | Plain | Per §7 | Yes |

Operational commitments (write these down before a compliance question, not after):

- **Erasure:** a data-subject erasure request tombstones the identifier and rotates its token. The
  decision record and its hash survive with the identifier redacted — you cannot erase from a
  tamper-evident chain, and the correct answer is to erase the *identifier* and retain the
  *record*, which is also what financial-records retention law requires. Say this; it is a real
  tension and having an answer is worth more than pretending there is none.
- **Access:** every read of a direct identifier through the API is logged with actor and purpose.
- **Minimisation:** the console shows masked identifiers by default; unmasking is a logged action
  requiring `analyst` or above.
- **Purpose limitation:** the training pipeline runs against the tokenised view. It has no grant on
  the encrypted columns. Enforced by Postgres role, not by convention.

---

**Next:** [03-ML-PIPELINE.md](03-ML-PIPELINE.md)
