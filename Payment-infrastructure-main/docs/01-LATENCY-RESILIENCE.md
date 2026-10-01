# 01 — Latency, Performance, Resilience

**Fixes:** F-01 F-02 F-03 F-04 F-05 F-29 F-35 F-49 F-59 F-61 F-65 F-66 F-67 F-68

---

## Contents

- [1 — How latency is measured](#1--how-latency-is-measured)
- [2 — The deadline and the timeout action](#2--the-deadline-and-the-timeout-action)
- [3 — Tail budget](#3--tail-budget)
- [4 — Overload](#4--overload)
- [5 — High availability](#5--high-availability)
- [6 — Durability and the audit chain](#6--durability-and-the-audit-chain)
- [7 — Idempotency](#7--idempotency)
- [8 — Capacity model](#8--capacity-model)
- [9 — Observability](#9--observability)
- [10 — Chaos and load test plan](#10--chaos-and-load-test-plan)

---

## 1 — How latency is measured

The previous design's headline number measured handler service time and called it ingest→decision
(F-01). Under load it would have *improved* as the backlog grew. Fixed by defining three numbers
and always publishing all three.

```
  t0  accepted_at      wall clock, stamped ONCE at the trust boundary (B1),
                       by the first Nazar process to touch the event
  t1  scoring_start    monotonic, same process
  t2  decided_at       monotonic, same process
  t3  emitted_at       wall clock, immediately before the response write

  queue_delay_ms  = t1 − t0     (cross-process → wall clock, needs synced clocks)
  service_time_ms = t2 − t1     (same process → monotonic, exact)
  total_ms        = t3 − t0     ◀── THIS is the number you are allowed to say out loud
```

Implementation rules:

- `accepted_at` uses `time.Now()` (wall). `service_time` uses a monotonic reading. **Go's `time.Time`
  carries both**, and `Sub` uses the monotonic component automatically — which is exactly the trap
  the previous Python design fell into and Go avoids for free.
- Hosts run NTP/chrony with a **measured and alarmed offset**. `clock_offset_ms` is a first-class
  metric; if it exceeds 5 ms, `queue_delay` is marked unreliable rather than reported as a small
  number.
- **The SLO is on `total_ms`, always.** `service_time` is a debugging aid. Reporting service time as
  latency is the single most common way a queued system reports a great number while being half a
  second behind.
- Percentiles from **HDR histograms**, not from averaging pre-aggregated percentiles. Publish
  p50 / p90 / p99 / **p99.9** / max, per rail, per profile tag.

**The number you say on stage carries its profile tag and its percentile.** Not "38ms" — that is a
single sample and it is meaningless. `[P0] total p99 = 9.4 ms at 180 RPS sustained, n = 2.1M`.

---

## 2 — The deadline and the timeout action

**This section is the most important one in the document and the previous design did not have it
(F-49).**

Nazar is an inline advice call. The caller owns a hard deadline and a default action. Both must be
written down, agreed, and tested — because the default action, not the model, is the system's real
security posture.

```
Contract with the payment switch
────────────────────────────────
  Deadline                 25 ms   (client-enforced; Nazar also self-enforces at 22 ms)
  Nazar's timeout action   return RAILS_ONLY decision at 22 ms — always a real answer
  Caller's timeout action  ALLOW with `nazar_unavailable` flag, transaction tagged for
                           post-hoc review within the recall window
  Caller's error action    same as timeout
  Health signal            /healthz distinguishes "up" from "up and non-degraded"
```

**Why the caller fails open, stated plainly:** blocking every payment in the country because a
fraud service is unhealthy is a larger, more certain harm than the fraud that leaks through a
short outage. This is the bank's decision, not Nazar's, and Nazar's job is to make the cost of that
window small and visible: every transaction decided without Nazar is tagged, queued, and
**re-scored post-hoc** — for push rails within the recall window, for card rails before capture.

**Why Nazar self-enforces at 22 ms and returns something rather than nothing.** A rails-only
decision is computable with **zero I/O** — local blocklist filters, regulatory rails on event
fields, and a static conservative policy. It costs ~5 µs. So there is no reason for Nazar to ever
time out; it degrades to a weaker but real answer instead. This is why the local in-process filters
in [02-DATA §3.5](02-DATA-AND-FEATURES.md#35--local-filters) exist — they are not a cache
optimisation, they are what makes a guaranteed answer possible.

**Deadline propagation.** The remaining budget travels with the request context. Every downstream
call (`profile load`, `graph read`) gets `min(remaining − reserve, per_call_cap)`. A call that
cannot complete within the remaining budget is **not attempted**; its feature is marked
`NOT_EVALUATED` (D5) and the decision proceeds. Never start work you cannot finish in time.

---

## 3 — Tail budget

Allocated as **p99 per stage**, with common-mode stalls budgeted separately. The previous budget
summed medians and called it a tail (F-03), and two of its rows were wrong by 15–100× (F-04).

`[P0]` — measured on the reference machine, single node, 180 RPS sustained:

| Stage | p50 | p99 | Basis | Notes |
|---|---|---|---|---|
| Accept, decode (protobuf/JSON), validate | 8 µs | 40 µs | measured | Go codegen decoder |
| Local rails (in-proc cuckoo + field checks) | 2 µs | 6 µs | measured | Zero I/O — the timeout fallback |
| **Profile load** (K concurrent single-slot pipelines) | **310 µs** | **2.1 ms** | measured | `max(RTT)`, not `Σ(RTT)` — see §3.1 |
| Feature assembly (pure arithmetic) | 18 µs | 45 µs | measured | ~60 features, no I/O, no allocation in the hot loop |
| Rule evaluation (CEL, precompiled, ~40 rules) | 12 µs | 30 µs | measured | |
| GBM inference (Treelite `.so`, 400 trees) | 9 µs | 25 µs | measured | vs the previous doc's 3 ms budget |
| Calibration + decision + rails | 3 µs | 8 µs | measured | |
| TreeSHAP attribution (400 trees, depth 8) | 210 µs | 600 µs | measured | Optional; see note |
| WAL append (batched fsync, group commit) | 25 µs | 180 µs | measured | |
| Encode + respond | 6 µs | 20 µs | measured | |
| **Sum of stages** | **~0.6 ms** | **~3.1 ms** | | |
| **Common-mode reserve** (GC, scheduler, page fault, TCP) | — | **+3 ms** | budgeted | |
| **Observed `total_ms`** | **0.7 ms** | **6–9 ms** | measured | |

**SLO:** `[P0] total p50 ≤ 2 ms · p99 ≤ 12 ms · p99.9 ≤ 25 ms · deadline 25 ms · zero timeouts.`

`[P1]` — modelled, not measured. Same shape, plus one network hop for cluster routing and higher
Redis contention: `p50 ≤ 3 ms · p99 ≤ 15 ms · p99.9 ≤ 35 ms`. **Labelled as modelled every time it
is quoted.**

> **On TreeSHAP.** 600 µs p99 is affordable inside 25 ms, and having exact signed contributions on
> the decision record — rather than reconstructing them later from a different code path — is worth
> it. If the budget tightens, compute SHAP only for decisions above `ALLOW` (~2% of traffic) and
> lazily on demand for the rest. That is a config flag, not a redesign (§3.3 of 00-ARCHITECTURE).

### 3.1 — Why the profile load is `max(RTT)` and not one round trip

The previous claim — *"one pipelined round trip, ~28 commands"* — is impossible in Redis Cluster
(F-35), which is mandatory at the scale the same document cites to justify the design. One decision
touches keys for payer, payee, device, ASN, pair, and blocklists: up to seven hash slots on
different nodes.

The correct design:

```
  Group keys by hash tag → K groups, K ≤ 5 after co-location
  Issue K pipelines CONCURRENTLY (K goroutines, one per slot owner)
  Wall clock = max(RTT_1..RTT_K) ≈ one RTT + scheduling jitter
```

Co-location that is available for free via hash tags:

| Hash tag | Keys co-located | Commands |
|---|---|---|
| `{p:<payer>}` | payer windows, payer baseline, payer payee-set, payer device-set, payer ASN-set, payer last-txn | 9 |
| `{b:<payee>}` | payee windows, payee fan-in, payee forwarding stats, payee first-seen | 6 |
| `{d:<device>}` | device account-degree, device first-seen | 2 |
| `{r:<payer>:<payee>}` | pair counters, pair p95, pair last-disposition | 3 |
| — | ASN degree | 1 |

Blocklists are **not** in this read at all — they are local filters (F-36), which removes the
hot-key bottleneck *and* is what makes the rails-only fallback possible.

**Hedged reads:** if a group has not returned by its p95 (≈ 700 µs), fire a duplicate to a replica
and take the first response. This converts a p99.9 caused by one unlucky node into a p99.
Cost: ~5% extra read load. Guard it with a circuit breaker so a slow *cluster* does not double its
own load.

---

## 4 — Overload

The previous design had nothing here (F-66) — the degradation table covered dependency failure and
not the condition that actually kills p99.

**Admission control, in order:**

1. **Adaptive concurrency limiter** (gradient / Little's-law based) on the decision service. The
   limit is derived from observed latency, not configured — when latency rises, the limit falls.
2. **Deadline-aware queue.** Requests carry `accepted_at`. Any request whose remaining budget is
   below the rails-only cost is **immediately answered rails-only**, not queued. Never do work that
   will be discarded — that is how queueing collapse starts.
3. **LIFO under stress.** Above 80% of the concurrency limit, serve newest-first. Under overload,
   FIFO guarantees every response is late; LIFO guarantees most are on time and a few are shed.
   Counter-intuitive and correct.
4. **Shed to rails-only, never shed to nothing.** Shedding returns a real, conservative, tagged
   decision. There is no path where Nazar returns an error to the switch.
5. **Async lane back-pressure.** The async pool has a bounded queue. On overflow: stop enqueueing
   graph and case work, keep WAL and persist, raise `degraded=["async_shed"]`, alarm. Freshness
   degrades; correctness and durability do not.

**The model timeout, fixed (F-65).** The previous design specified "abandon inference at 20 ms",
which is not implementable for a blocking foreign call and does not help under overload anyway. At
9 µs p50, inference is not a timeout risk. The real control is the **concurrency limiter**: if
inference is slow, latency rises, the limiter tightens, and load sheds to rails-only. One mechanism,
not a per-stage timeout that cannot be enforced.

**Circuit breakers**, with actual numbers:

| Dependency | Open when | Half-open probe | While open |
|---|---|---|---|
| Redis slot owner | 20 consecutive errors or p99 > 8 ms over 10 s | 1 req / 500 ms | last-known-good cache; affected features `NOT_EVALUATED` |
| Graph service | 10 errors or p99 > 5 ms over 10 s | 1 req / 1 s | graph features `NOT_EVALUATED`; ring signals suppressed |
| Consortium registry | 5 errors over 30 s | 1 req / 5 s | no advisory (fail-open by construction) |
| Postgres | 5 errors over 10 s | 1 req / 2 s | WAL-only; shipper drains on recovery |

---

## 5 — High availability

The previous design had Redis as an unaddressed single point of failure (F-67).

| Component | HA design | Failover | RPO / RTO |
|---|---|---|---|
| decision | N ≥ 3 stateless replicas, ≥ 2 AZ, LB health-checked | Instant | 0 / 0 |
| Redis profile store | Cluster, 3 primaries min, 1 replica each, cross-AZ, `cluster-require-full-coverage no` | 5–15 s (Cluster) | Bounded staleness / seconds |
| Redpanda / Streams | RF=3, `acks=all` | Seconds | 0 / seconds |
| Postgres | Primary + sync standby + async replica | 10–30 s | 0 / < 1 min |
| graph | N replicas, each with a full in-memory graph rebuilt from the log | Instant (read path) | Bounded staleness |
| consortium registry | Out of our failure domain | — | Advisory only, so N/A |

Two things worth stating because they are usually missed:

- **`cluster-require-full-coverage no`** means a partial cluster still serves the slots it owns.
  Combined with per-group `NOT_EVALUATED` degradation, **losing one Redis shard degrades a slice of
  features rather than taking the system down.** This is the single highest-leverage HA setting in
  the stack.
- **Redis persistence must not fork on the primary.** `BGSAVE`'s `fork()` is a classic p99.9 killer
  on a large keyspace. Persist from a replica; run primaries with AOF `everysec` or no persistence
  at all, since the profile store is **derived state** — it can be rebuilt from the event log. Say
  that out loud: the profile store is a cache with a long memory, not a system of record.

---

## 6 — Durability and the audit chain

Three fixes: the response no longer depends on a remote write (F-05), the chain no longer requires
a global order (F-59), and decisions can no longer be silently lost.

### 6.1 — Local WAL before the response

```
  decide → append to local WAL → respond → async ship to Postgres
                  │
                  └─ segmented file, group-committed fsync (~25 µs amortised at 180 RPS),
                     CRC per record, monotonic sequence per (shard, epoch)
```

**A decision is durable before the customer sees it.** The previous design's "never block the
response on the write" is right about Postgres and wrong about durability — it produced a
tamper-evident log with routine holes in it, which is worse than no log because the holes are
indistinguishable from tampering.

The shipper drains WAL → Postgres with at-least-once delivery and idempotent upsert (§7). WAL
segments are retained until acknowledged, then for 24 h.

### 6.2 — Per-shard chains, cross-shard Merkle anchor

A single chain needs a total order, which is incompatible with N stateless writers (F-59). So:

```
  Each decision service instance owns a shard id and maintains its own chain:

     h_i = SHA256( h_{i-1} ‖ canonical_cbor(record) )

  Every 10 s (or 10k records), each shard publishes a signed checkpoint:

     { shard, epoch, seq_start, seq_end, head_hash, ts, sig }

  An anchor service builds a Merkle tree over all shard heads in the interval
  and publishes a signed root. Roots are chained.
```

Properties this actually gives you, stated precisely:

- **Within a shard:** any modification or deletion of a decision breaks the chain from that point.
- **Across shards:** any modification of a *checkpointed* shard head breaks the Merkle root.
- **Ordering across shards:** partial, by anchor interval — not total. **Say this.** A total order
  across concurrent writers requires consensus, and you do not need one; you need tamper-evidence,
  and interval-ordered anchoring gives it.
- **Not protected against:** an operator who controls both the DB and the anchor signing key from
  the moment of writing. That requires an external witness (publish roots to a third party, or to
  the consortium registry). Name it as the residual risk rather than implying it away.

`decision_shard`, `chain_seq`, `prev_hash`, `hash`, `checkpoint_id` go on every decision row.

---

## 7 — Idempotency

At-least-once delivery meets a primary key and an infinite retry loop in the previous design
(F-61).

| Path | Mechanism |
|---|---|
| Inbound `POST /v1/decide` | Idempotency key = `end_to_end_id`. A repeat within the TTL returns the **stored decision**, unchanged, with `replayed: true`. Never re-scores |
| WAL → Postgres shipper | `INSERT ... ON CONFLICT (end_to_end_id, decision_seq) DO NOTHING` |
| Profile store apply | Window `ZADD` is already idempotent (`member = end_to_end_id` overwrites). Bucketed counters use a per-bucket dedupe set with the bucket's TTL |
| Graph edge apply | Edge key `(src, dst, kind, end_to_end_id)`; upsert |
| Consortium publish | Entry id = `HMAC(reporter_key, token ‖ case_id)`; registry rejects duplicates |

The idempotency-key store is Redis with a 24 h TTL, keyed `{i:<e2e_id>}`, holding the serialised
decision. It sits in the same concurrent read (§3.1) as one extra command, so it costs nothing.

---

## 8 — Capacity model

The previous documents contained no sizing at any scale (F-38). Here is the arithmetic. **All `[P1]`
rows are modelled** — the point is that the model exists and can be argued with.

### 8.1 — Throughput

| | `[P0]` | `[P1]` | `[P2]` (not built) |
|---|---|---|---|
| Sustained TPS | 180 | 5,000 | 9,000 |
| Peak TPS (3.5× diurnal) | 600 | 17,500 | 32,000 |
| Decision replicas @ 1,200 RPS/core, 8 cores, 60% headroom | 1 | 4 | 12 |
| Redis primaries @ ~120k ops/s each, ~21 cmds/txn | 1 | 4 | 12 |
| Postgres decision writes/s | 180 | 5,000 | 32,000 |

> The `[P2]` peak of 32k TPS is derived from the previous document's own UPI figures, corrected:
> 23.2 B/month ÷ 31 = 748 M/day = **8,660 TPS average**, not "above 5,000" (F-75). With a 3.5×
> peak-to-average ratio that is ~30k TPS peak. Understating your own requirement is a strange way
> to justify a design.

### 8.2 — Profile store memory `[P1]`, 20 M accounts, 8 M daily-active

| Structure | Per entity | Entities | Total |
|---|---|---|---|
| Payer txn window zset (7 d, ~140 txns, 60 B/entry) | 8.4 KB | 8 M active | 67 GB |
| Payer amount buckets (minute buckets, 24 h + hourly 30 d) | 1.9 KB | 8 M | 15 GB |
| Payer baseline hash (12 fields) | 0.4 KB | 20 M | 8 GB |
| Payer payee-set + device-set + asn-set | 1.1 KB | 20 M | 22 GB |
| Payee fan-in zset (24 h) | 1.2 KB | 6 M | 7 GB |
| Payee forwarding stats hash | 0.2 KB | 6 M | 1 GB |
| Device / ASN degree | — | 12 M | 6 GB |
| **Pair keyspace (90 d)** — ~11 payees/payer | 0.3 KB | **220 M pairs** | **66 GB** |
| Idempotency (24 h) | 0.9 KB | 430 M/day | 12 GB (rolling) |
| **Subtotal** | | | **~204 GB** |
| **+ 40% fragmentation/overhead** | | | **~285 GB** |

**Findings from doing the arithmetic the previous documents never did:**

1. **The pair keyspace is a third of the memory**, and it is the one that grows as a cross product.
   It is also the thing the trusted-pair fast path depends on. Mitigation: pairs with
   `txn_count_90d < 2` are not stored at all — they are indistinguishable from "no relationship",
   which is the default. That removes an estimated 60% of pair keys.
2. **The 7-day payer window is the largest single structure**, and 7 days is only needed for one
   feature (`txn_velocity_7d`, weak). Dropping to 24 h + minute buckets for the longer windows
   saves ~50 GB. **This is exactly the kind of trade the previous docs could not evaluate because
   they never sized anything.**
3. At `[P1]`, ~285 GB across 4–6 primaries is 48–72 GB each — comfortable, and the reason `[P1]` is
   a real target. At `[P2]` the same model gives ~4 TB, which is where the economics change and the
   honest answer is a different store.

### 8.3 — Postgres `[P1]`

| Table | Rows/day | Row size | Daily | 90-day retention |
|---|---|---|---|---|
| `transactions` | 432 M | 260 B | 112 GB | 10 TB |
| `decisions` (incl. features JSONB) | 432 M | 1.9 KB | 820 GB | **74 TB** |

**`decisions` at 1.9 KB/row is not viable at 90-day retention**, and neither previous document
noticed that storing the full feature vector inline (their P2) has a cost. Resolution:

- Hot partition (7 days) in Postgres with the full vector — that is 5.7 TB, which is fine.
- Beyond 7 days, features move to **columnar object storage (Parquet, partitioned by day)** and the
  Postgres row keeps the decision, the hashes, and a pointer. Training reads Parquet; the Time
  Machine reads Parquet for old decisions and Postgres for recent ones, behind one interface.
- Daily partitions, `DETACH` + archive, never `DELETE`.

This preserves the replay property (D1/D2) at 1/20th the storage cost, and it is a Type 2 decision
behind the `DecisionSink` seam.

---

## 9 — Observability

Metrics that exist because a specific finding says they must:

| Metric | Exists because |
|---|---|
| `nazar_total_ms{rail,profile}` HDR, p50/p99/p99.9 | F-01 — the only latency number you may quote |
| `nazar_queue_delay_ms`, `nazar_service_ms` | F-01 — separated so neither can hide the other |
| `nazar_clock_offset_ms` | F-40 — a skewed clock corrupts every window silently |
| `nazar_deadline_exceeded_total`, `nazar_rails_only_total{reason}` | §2 — the fallback must be visible, not silent |
| `nazar_feature_not_evaluated_total{feature,reason}` | D5 — a feature that stopped computing must not look clean |
| `nazar_feature_integrity_divergence{feature}` | The dual-derivation check, the best idea in the original docs |
| `nazar_profile_slot_rtt_ms{slot}` | §3.1 — per-slot, because one slow shard is the p99.9 |
| `nazar_async_queue_depth`, `nazar_async_shed_total` | F-05 — backpressure must be observable |
| `nazar_wal_unshipped_records`, `nazar_chain_gap_total` | §6 — a gap in the audit chain is a page, immediately |
| `nazar_signal_state{signal}` (0=off,1=shadow,2=live) | §3.3 — you must be able to see which lanes are live |
| `nazar_score_distribution` (histogram, hourly) | F-15 — concept drift moves this before PSI moves |
| `nazar_rule_fire_rate{rule}` | F-15 — a rule whose fire rate jumps 10× is the earliest drift signal you have |
| `nazar_calibration_error` (ECE on matured labels, daily) | F-07/F-09 — when this drifts, every rupee threshold is wrong |

Tracing: one span per stage, with the deadline remaining recorded on each. Sample at 1% plus 100%
of anything that exceeded p99 or hit a fallback — the tail is the only interesting part.

---

## 10 — Chaos and load test plan

Every row is a CI job, not an exercise. The previous test suite was entirely demo beats (F-70).

| Test | Asserts | Gate |
|---|---|---|
| `load_sustained` | 2 h at target TPS; p99 within SLO; zero timeouts; memory flat | Release |
| `load_spike` | 5× step for 60 s; sheds to rails-only; **no errors returned**; recovers < 30 s | Release |
| `chaos_redis_slot_kill` | One primary killed: affected features `NOT_EVALUATED`, others unaffected, **no BLOCK that would not occur healthy** (D7) | Release |
| `chaos_redis_full_outage` | Rails-only; value caps applied; no denials; window replayed on recovery | Release |
| `chaos_pg_outage` | Decisions continue; WAL grows; zero loss; chain intact after drain | Release |
| `chaos_graph_lag` | Graph 60 s stale: features `NOT_EVALUATED`, staleness recorded, ring signals suppressed | Release |
| `chaos_clock_skew` | Producer clock +30 s: windows not corrupted, skew alarmed, features degraded not wrong | Release |
| `chaos_async_saturation` | Async queue full: sheds, alarms, WAL/persist unaffected | Release |
| `prop_no_block_under_degradation` | **Property test** over the cross product of every injected failure × 10k generated events: no `BLOCK` that would not occur healthy | Merge |
| `prop_advisory_monotone_and_capped` | Advisory never lowers the rung and never exceeds `advisory_max_rung` (F-20) | Merge |
| `prop_deadline_always_answered` | Under any injected latency, a decision is always returned within the deadline | Merge |
| `golden_features` | Checked-in `(ProfileBundle, Event) → FeatureVector` fixtures byte-match | Merge |
| `feature_catalogue_key_coverage` | **Every feature in the registry has a backing key and a producer.** This one test would have caught F-34 on day one | Merge |
| `window_arithmetic_property` | Randomised event streams: Redis window reads match a brute-force reference implementation over the same stream. Catches F-32/F-33 | Merge |

> The last two are the cheapest high-value tests in the suite and neither previous document had
> anything like them. `feature_catalogue_key_coverage` is about 30 lines.

---

**Next:** [02-DATA-AND-FEATURES.md](02-DATA-AND-FEATURES.md)
